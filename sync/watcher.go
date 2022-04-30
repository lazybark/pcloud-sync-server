package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lazybark/go-helpers/hasher"
	"github.com/lazybark/pcloud-sync-server/fsworker"
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

				notifyEvent := ConnNotifierEvent{
					Event: event,
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
						err = s.FW.ProcessDirectory(event.Name)
						if err != nil {
							s.Logger.Error(fmt.Sprintf("Error processing %s: ", event.Name), err)
						}

						dir, _ := filepath.Split(event.Name)
						dir = strings.TrimSuffix(dir, string(filepath.Separator))

						notifyEvent.Object = fsworker.Folder{
							Name:        dat.Name(),
							FSUpdatedAt: dat.ModTime(),
							Path:        s.FW.EscapeAddress(dir),
						}
					} else {
						dir, _ := filepath.Split(event.Name)
						dir = strings.TrimSuffix(dir, string(filepath.Separator))

						hash, err := hasher.HashFilePath(event.Name, hasher.SHA256, 8192)
						if err != nil {
							s.Logger.Error(err)
						}

						notifyEvent.Object = fsworker.File{
							Name:        dat.Name(),
							FSUpdatedAt: dat.ModTime(),
							Path:        s.FW.EscapeAddress(dir),
							Hash:        hash,
						}
					}
					// Add object to DB
					err = s.FW.MakeDBRecord(dat, event.Name)
					if err != nil && err.Error() != "UNIQUE constraint failed: sync_files.name, sync_files.path" {
						s.Logger.Error(fmt.Sprintf("Error making record for %s: ", event.Name), err)
					}

					select {
					case s.ConnNotifier <- notifyEvent:
						if s.Config.FilesystemVerboseLogging {
							s.Logger.Info(fmt.Sprintf("%s %s", event.Name, event.Op))
						}
					default:

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
						var folder fsworker.Folder

						if err := s.DB.Where("name = ? and path = ?", child, s.FW.EscapeAddress(dir)).First(&folder).Error; err != nil {
							s.Logger.Error("File reading failed: ", err)
						} else {
							// Update data in DB
							folder.FSUpdatedAt = dat.ModTime()
							if err := s.DB.Table("folders").Save(&folder).Error; err != nil && err != gorm.ErrEmptySlice {
								s.Logger.Error("Dir saving failed: ", err)
							}
						}

						notifyEvent.Object = folder
						select {
						case s.ConnNotifier <- notifyEvent:
							if s.Config.FilesystemVerboseLogging {
								s.Logger.Info(fmt.Sprintf("%s %s", event.Name, event.Op))
							}
						default:

						}
					} else {
						var file fsworker.File
						if err := s.DB.Where("name = ? and path = ?", child, s.FW.EscapeAddress(dir)).First(&file).Error; err != nil {
							s.Logger.Error("File reading failed: ", err)
						} else {
							// Update data in DB
							hash := ""
							hash, err := hasher.HashFilePath(event.Name, hasher.SHA256, 8192)
							if err != nil {
								s.Logger.Error(err)
							}
							file.FSUpdatedAt = dat.ModTime()
							file.Size = dat.Size()
							file.Hash = hash
							if err := s.DB.Table("files").Save(&file).Error; err != nil && err != gorm.ErrEmptySlice {
								s.Logger.Error("File saving failed: ", err)
							}

						}
						notifyEvent.Object = file
						select {
						case s.ConnNotifier <- notifyEvent:
							if s.Config.FilesystemVerboseLogging {
								s.Logger.Info(fmt.Sprintf("%s %s", event.Name, event.Op))
							}
						default:

						}
					}
				} else if event.Op.String() == "REMOVE" || event.Op.String() == "RENAME" { //no difference for DB between deletion and renaming
					var file fsworker.File
					var folder fsworker.Folder

					dir, child := filepath.Split(event.Name)
					dir = strings.TrimSuffix(dir, string(filepath.Separator))

					err := s.DB.Where("name = ? and path = ?", child, s.FW.EscapeAddress(dir)).First(&file).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						s.Logger.Error(err)
					}
					// ID becomes real if object found in DB
					if file.ID > 0 {
						err = s.DB.Delete(&file).Error
						if err != nil {
							s.Logger.Error(err)
						}
						notifyEvent.Object = file

						select {
						case s.ConnNotifier <- notifyEvent:
							if s.Config.FilesystemVerboseLogging {
								s.Logger.Info(fmt.Sprintf("%s %s", event.Name, event.Op))
							}
						default:

						}
						break
					}

					err = s.DB.Where("name = ? and path = ?", child, s.FW.EscapeAddress(dir)).First(&folder).Error
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
						err = s.DB.Where("path = ?", s.FW.EscapeAddress(event.Name)).Delete(&file).Error
						if err != nil {
							s.Logger.Error(err)
						}

						folder.Path = s.FW.EscapeAddress(folder.Path)
						folder.FSUpdatedAt = time.Now()
						notifyEvent.Object = folder
						select {
						case s.ConnNotifier <- notifyEvent:
							if s.Config.FilesystemVerboseLogging {
								s.Logger.Info(fmt.Sprintf("%s %s", event.Name, event.Op))
							}
						default:

						}

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
