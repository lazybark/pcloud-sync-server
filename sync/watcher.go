package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

// FilesystemWatcherRoutine tracks changes in every folder in root dir
func (s *Server) FilesystemWatcherRoutine() {
	done := make(chan bool)
	go func() {
		defer close(done)

		for {
			select {
			case event, ok := <-s.Watcher.Events:
				if !ok {
					return
				}

				select {
				case s.ConnNotifier <- event:
					if s.Config.FilesystemVerboseLogging {
						s.Logger.Info(fmt.Sprintf("%s %s", event.Name, event.Op))
					}
				default:

				}

				// Pause before processing actions to make sure that target isn't locked
				// If file hashing still produces errors (target busy) - increase pause time
				time.Sleep(100 * time.Millisecond)

				if event.Op.String() == "CREATE" {
					dat, err := os.Stat(event.Name)
					if err != nil {
						s.Logger.Error("Object reading failed: ", err)
						break
					}
					if dat.IsDir() {
						// Watch new dir
						err := s.Watcher.Add(event.Name)
						if err != nil {
							s.Logger.Error("FS watcher add failed:", err)
						}
						s.Logger.Info(fmt.Sprintf("%s added to watcher", event.Name))

						// Scan dir
						err = s.ProcessDirectory(event.Name)
						if err != nil {
							s.Logger.Error(fmt.Sprintf("Error processing %s: ", event.Name), err)
						}
					}
					// Add object to DB
					err = s.MakeDBRecord(dat, event.Name)
					if err != nil && err.Error() != "UNIQUE constraint failed: sync_files.name, sync_files.path" {
						s.Logger.Error(fmt.Sprintf("Error making record for %s: ", event.Name), err)
					}
				} else if event.Op.String() == "WRITE" {
					dir, child := filepath.Split(event.Name)
					dir = strings.TrimSuffix(dir, string(filepath.Separator))
					dat, err := os.Stat(event.Name)
					if err != nil {
						s.Logger.Error("Object reading failed: ", event.Op.String(), err)
						break
					}
					if dat.IsDir() {
						var folder Folder

						if err := s.DB.Where("name = ? and path = ?", child, s.EscapeAddress(dir)).First(&folder).Error; err != nil {
							s.Logger.Error("File reading failed: ", err)
						} else {
							// Update data in DB
							folder.FSUpdatedAt = dat.ModTime()
							if err := s.DB.Table("folders").Save(&folder).Error; err != nil && err != gorm.ErrEmptySlice {
								s.Logger.Error("Dir saving failed: ", err)
							}
						}
					} else {
						var file File
						if err := s.DB.Where("name = ? and path = ?", child, s.EscapeAddress(dir)).First(&file).Error; err != nil {
							s.Logger.Error("File reading failed: ", err)
						} else {
							// Update data in DB
							hash := ""
							hash, err := s.FileHash(event.Name)
							if err != nil {
								s.Logger.Error(err)
							}
							file.FSUpdatedAt = dat.ModTime()
							file.Size = dat.Size()
							file.Hash = hash
							if err := s.DB.Table("files").Save(&file).Error; err != nil && err != gorm.ErrEmptySlice {
								s.Logger.Error("Dir saving failed: ", err)
							}
						}
					}
				} else if event.Op.String() == "REMOVE" || event.Op.String() == "RENAME" { //no difference for DB between deletion and renaming
					var file File
					var folder Folder

					dir, child := filepath.Split(event.Name)
					dir = strings.TrimSuffix(dir, string(filepath.Separator))

					err := s.DB.Where("name = ? and path = ?", child, s.EscapeAddress(dir)).First(&file).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						s.Logger.Error(err)
					}
					// ID becomes real if object found in DB
					if file.ID > 0 {
						err = s.DB.Delete(&file).Error
						if err != nil {
							s.Logger.Error(err)
						}
						break
					}

					err = s.DB.Where("name = ? and path = ?", child, s.EscapeAddress(dir)).First(&folder).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						s.Logger.Error(err)
					}
					// ID becomes real if object found in DB
					if folder.ID > 0 {
						err = s.DB.Delete(&folder).Error
						if err != nil {
							s.Logger.Error(err)
						}
						// Manually delete all files connected to this dir
						err = s.DB.Where("path = ?", s.EscapeAddress(event.Name)).Delete(&file).Error
						if err != nil {
							s.Logger.Error(err)
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

			case err, ok := <-s.Watcher.Errors:
				if !ok {
					return
				}
				s.Logger.Error("FS watcher error: ", err)
			}
		}
	}()

	err := s.Watcher.Add(s.Config.FileSystemRootPath)
	if err != nil {
		s.Logger.Fatal("FS watcher add failed: ", err)
	}
	s.Logger.Info(fmt.Sprintf("%s added to watcher", s.Config.FileSystemRootPath))
	<-done
}
