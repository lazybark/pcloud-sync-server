package server

import (
	"fmt"

	"github.com/lazybark/pcloud-sync-server/cloud/events"
)

// FilesystemWatcherRoutine tracks changes in every folder in root dir
func (s *Server) FilesystemWatcherRoutine() {
	s.evProc.SendVerbose(EventType("green"), events.SourceFileSystemWatcher.String(), "Starting filesystem watcher")
	s.fswOnline = true
	done := make(chan bool)
	go func() {
		defer close(done)

		for {
			select {
			case event, ok := <-s.watcher.Events:
				if !ok {
					return
				}

				s.fsEventsChan <- event

			case err, ok := <-s.watcher.Errors:
				if !ok {
					return
				}
				s.evProc.Send(EventType("error"), events.SourceFileSystemWatcher.String(), fmt.Errorf("fs watcher error: %w", err))
			}
		}

	}()

	err := s.watcher.Add(s.config.FileSystemRootPath)
	if err != nil {
		s.evProc.Send(EventType("fatal"), events.SourceFileSystemWatcher.String(), fmt.Errorf("fs watcher add failed: %w", err))
	}
	s.evProc.SendVerbose(EventType("info"), events.SourceFileSystemWatcher.String(), fmt.Sprintf("%s added to watcher", s.config.FileSystemRootPath))
	<-done
	s.fswOnline = false
	s.evProc.SendVerbose(EventType("red"), events.SourceFileSystemWatcher.String(), "FS watcher stopped")
}

/*














































notifyEvent := ConnNotifierEvent{
	Event: event,
}










// Pause before processing actions to make sure that target isn't locked
// If file hashing still produces errors (target busy) - increase pause time
// or add some latency in methods that access files
time.Sleep(500 * time.Millisecond)

if event.Op.String() == "CREATE" {
	dat, err := os.Stat(event.Name)
	if err != nil {
		s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("object reading failed: %w", err))
		break
	}
	if dat.IsDir() {
		// Watch new dir
		err := s.Watcher.Add(event.Name)
		if err != nil {
			s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("FS watcher add failed: %w", err))
		}
		s.evProc.SendVerbose(EventType("info"), SourceFileSystemWatcher.String(), fmt.Sprintf("%s added to watcher", event.Name))

		// Scan dir
		_, _, err = s.FW.ProcessDirectory(event.Name)
		if err != nil {
			s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("error processing %s: %w", event.Name, err))
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
			s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("error getting hash: %w", err))
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
		s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("error making record for %s: %w", event.Name, err))
	}

	select {
	case s.ConnNotifier <- notifyEvent:
		if s.Config.FilesystemVerboseLogging {
			s.evProc.SendVerbose(EventType("info"), SourceFileSystemWatcher.String(), fmt.Sprintf("%s %s", event.Name, event.Op))
		}
	default:

	}

} else if event.Op.String() == "WRITE" {
	dir, child := filepath.Split(event.Name)
	dir = strings.TrimSuffix(dir, string(filepath.Separator))
	dat, err := os.Stat(event.Name)
	if err != nil {
		s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("object %s reading failed: %w", event.Op.String(), err))
		break
	}
	if dat.IsDir() {
		var folder fsworker.Folder

		if err := s.DB.Where("name = ? and path = ?", child, s.FW.EscapeAddress(dir)).First(&folder).Error; err != nil {
			s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("file reading failed: %w", err))
		} else {
			// Update data in DB
			folder.FSUpdatedAt = dat.ModTime()
			if err := s.DB.Table("folders").Save(&folder).Error; err != nil && err != gorm.ErrEmptySlice {
				s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("dir saving failed: %w", err))
			}
		}

		notifyEvent.Object = folder
		select {
		case s.ConnNotifier <- notifyEvent:
			if s.Config.FilesystemVerboseLogging {
				s.evProc.Send(EventType("info"), SourceFileSystemWatcher.String(), fmt.Sprintf("%s %s", event.Name, event.Op))
			}
		default:

		}
	} else {
		var file fsworker.File
		if err := s.DB.Where("name = ? and path = ?", child, s.FW.EscapeAddress(dir)).First(&file).Error; err != nil {
			s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("file reading failed: %w", err))
		} else {
			// Update data in DB
			hash := ""
			hash, err := hasher.HashFilePath(event.Name, hasher.SHA256, 8192)
			if err != nil {
				s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("error getting hash: %w", err))

			}
			file.FSUpdatedAt = dat.ModTime()
			file.Size = dat.Size()
			file.Hash = hash
			if err := s.DB.Table("files").Save(&file).Error; err != nil && err != gorm.ErrEmptySlice {
				s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("file saving failed: %w", err))
			}

		}
		notifyEvent.Object = file
		select {
		case s.ConnNotifier <- notifyEvent:
			if s.Config.FilesystemVerboseLogging {
				s.evProc.Send(EventType("info"), SourceFileSystemWatcher.String(), fmt.Sprintf("%s %s", event.Name, event.Op))
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
		s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("error getting %s from DB: %w", dir+child, err))
	}
	// ID becomes real if object found in DB
	if file.ID > 0 {
		err = s.DB.Delete(&file).Error
		if err != nil {
			s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("error deleting %s: %w", dir+child, err))
		}
		notifyEvent.Object = file

		select {
		case s.ConnNotifier <- notifyEvent:
			if s.Config.FilesystemVerboseLogging {
				s.evProc.Send(EventType("info"), SourceFileSystemWatcher.String(), fmt.Sprintf("%s %s", event.Name, event.Op))
			}
		default:

		}
		break
	}

	err = s.DB.Where("name = ? and path = ?", child, s.FW.EscapeAddress(dir)).First(&folder).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("error getting %s from DB: %w", dir+child, err))
	}
	// ID becomes real if object found in DB
	if folder.ID > 0 {
		err = s.DB.Delete(&folder).Error
		if err != nil {
			s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("error deleting %s: %w", event.Name, err))
		}
		// Manually delete all files connected to this dir
		err = s.DB.Where("path = ?", s.FW.EscapeAddress(event.Name)).Delete(&file).Error
		if err != nil {
			s.evProc.Send(EventType("error"), SourceFileSystemWatcher.String(), fmt.Errorf("error deleting files assiciated to %s: %w", event.Name, err))
		}

		folder.Path = s.FW.EscapeAddress(folder.Path)
		folder.FSUpdatedAt = time.Now()
		notifyEvent.Object = folder
		select {
		case s.ConnNotifier <- notifyEvent:
			if s.Config.FilesystemVerboseLogging {
				s.evProc.Send(EventType("info"), SourceFileSystemWatcher.String(), fmt.Sprintf("%s %s", event.Name, event.Op))
			}
		default:

		}

		break
	}
}*/
