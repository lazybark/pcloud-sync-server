package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lazybark/go-pretty-code/logs"
	"github.com/lazybark/pcloud-sync-server/config"
	"github.com/lazybark/pcloud-sync-server/models"

	"github.com/fsnotify/fsnotify"
	"gorm.io/gorm"
)

// HashFilesRoutine checks hash of every unhashed file
/*func HashFilesRoutine(db *gorm.DB, logger *logs.Logger) {

}*/

// FilesystemWatcherRoutine tracks changes in every folder in root dir
func FilesystemWatcherRoutine(logger *logs.Logger, db *gorm.DB, watch *fsnotify.Watcher) {
	done := make(chan bool)
	go func() {
		defer close(done)

		for {
			select {
			case event, ok := <-watch.Events:
				if !ok {
					return
				}
				if config.Current.FilesystemVerboseLogging {
					logger.Info(fmt.Sprintf("%s %s", event.Name, event.Op))
				}

				// Pause before processing actions to make sure that target isn't locked
				// If file hashing still produces errors (target busy) - increase pause time
				time.Sleep(100 * time.Millisecond)

				if event.Op.String() == "CREATE" {
					dat, err := os.Stat(event.Name)
					if err != nil {
						logger.Error("Object reading failed: ", err)
						break
					}
					if dat.IsDir() {
						// Watch new dir
						err := watch.Add(event.Name)
						if err != nil {
							logger.Error("FS watcher add failed:", err)
						}
						logger.Info(fmt.Sprintf("%s added to watcher", event.Name))

						// Scan dir
						err = ProcessDirectory(event.Name, db, watch, logger)
						if err != nil {
							logger.Error(fmt.Sprintf("Error processing %s: ", event.Name), err)
						}
					}
					// Add object to DB
					err = MakeDBRecord(dat, event.Name, db, logger)
					if err != nil && err.Error() != "UNIQUE constraint failed: sync_files.name, sync_files.path" {
						logger.Error(fmt.Sprintf("Error making record for %s: ", event.Name), err)
					}
				} else if event.Op.String() == "WRITE" {
					dir, child := filepath.Split(event.Name)
					dir = strings.TrimSuffix(dir, string(filepath.Separator))
					dat, err := os.Stat(event.Name)
					if err != nil {
						logger.Error("Object reading failed: ", event.Op.String(), err)
						break
					}
					if dat.IsDir() {
						var folder models.SyncFolder

						if err := db.Where("name = ? and path = ?", child, EscapeAddress(dir)).First(&folder).Error; err != nil {
							logger.Error("File reading failed: ", err)
						} else {
							// Update data in DB
							folder.FSUpdatedAt = dat.ModTime()
							if err := db.Table("sync_folders").Save(&folder).Error; err != nil && err != gorm.ErrEmptySlice {
								logger.Error("Dir saving failed: ", err)
							}
						}
					} else {
						var file models.SyncFile
						if err := db.Where("name = ? and path = ?", child, EscapeAddress(dir)).First(&file).Error; err != nil {
							logger.Error("File reading failed: ", err)
						} else {
							// Update data in DB
							hash := ""
							hash, err := FileHash(event.Name)
							if err != nil {
								logger.Error(err)
							}
							file.FSUpdatedAt = dat.ModTime()
							file.Size = dat.Size()
							file.Hash = hash
							if err := db.Table("sync_files").Save(&file).Error; err != nil && err != gorm.ErrEmptySlice {
								logger.Error("Dir saving failed: ", err)
							}
						}
					}
				} else if event.Op.String() == "REMOVE" || event.Op.String() == "RENAME" { //no difference for DB between deletion and renaming
					var file models.SyncFile
					var folder models.SyncFolder

					dir, child := filepath.Split(event.Name)
					dir = strings.TrimSuffix(dir, string(filepath.Separator))

					err := db.Where("name = ? and path = ?", child, EscapeAddress(dir)).First(&file).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						logger.Error(err)
					}
					// ID becomes real if object found in DB
					if file.ID > 0 {
						err = db.Delete(&file).Error
						if err != nil {
							logger.Error(err)
						}
						break
					}

					err = db.Where("name = ? and path = ?", child, EscapeAddress(dir)).First(&folder).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						logger.Error(err)
					}
					// ID becomes real if object found in DB
					if folder.ID > 0 {
						err = db.Delete(&folder).Error
						if err != nil {
							logger.Error(err)
						}
						// Manually delete all files connected to this dir
						err = db.Where("path = ?", EscapeAddress(event.Name)).Delete(&file).Error
						if err != nil {
							logger.Error(err)
						}

						// Stop watching this dir
						/*errWatcher := watch.Remove(event.Name)
						if errWatcher != nil {
							logger.Zap.Error("FS watcher delete failed: ", errWatcher)
						} else {
							logger.Zap.Info(fmt.Sprintf("%s deleted from watcher", event.Name))
						}*/

						break
					}
				}

			case err, ok := <-watch.Errors:
				if !ok {
					return
				}
				logger.Error("FS watcher error: ", err)
			}
		}
	}()

	err := watch.Add(config.Current.FileSystemRootPath)
	if err != nil {
		logger.Fatal("FS watcher add failed: ", err)
	}
	logger.Info(fmt.Sprintf("%s added to watcher", config.Current.FileSystemRootPath))
	<-done
}
