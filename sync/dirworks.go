package sync

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"gorm.io/gorm"
)

// ScanDir scans dir contents and fills inputing slices with file and dir models
func (s *Server) ScanDir(path string, dirs *[]Folder, files *[]File) (err error) {
	var dirsRec []string
	var dir Folder
	var file File

	contents, err := ioutil.ReadDir(path)
	if err != nil {
		return
	}

	for _, item := range contents {
		if item.IsDir() {
			// Send dirname for recursive scanning
			dirsRec = append(dirsRec, item.Name())
			// Append to dirs list for output
			dir = Folder{
				Name:        item.Name(),
				Path:        s.EscapeAddress(path),
				Size:        item.Size(),
				FSUpdatedAt: item.ModTime(),
			}
			*dirs = append(*dirs, dir)
		} else {
			hash := ""
			hash, err = s.FileHash(filepath.Join(path, item.Name()))
			if err != nil /*&& errors.Is(err, os.SyscallError)*/ {
				s.Logger.Error(err)
			}

			file = File{
				Name:        item.Name(),
				Size:        item.Size(),
				Hash:        hash,
				Path:        s.EscapeAddress(path),
				FSUpdatedAt: item.ModTime(),
				Type:        filepath.Ext(item.Name()),
			}
			// Append to list for output
			*files = append(*files, file)
		}
	}

	// Recursively scan all sub dirs
	for _, dirName := range dirsRec {
		err = s.ScanDir(filepath.Join(path, dirName), dirs, files)
		if err != nil {
			return err
		}
	}

	return
}

// StoreDirData fills DB with file and dir data provided
func (s *Server) StoreDirData(dirs *[]Folder, files *[]File) (err error) {
	if err = s.DB.Table("sync_folders").Save(dirs).Error; err != nil && err != gorm.ErrEmptySlice {
		return
	}
	if err = s.DB.Table("sync_files").Save(files).Error; err != nil && err != gorm.ErrEmptySlice {
		return
	}

	return nil
}

// ProcessDirectory scans dir and fills DB with dir's data
func (s *Server) ProcessDirectory(path string) (err error) {
	var dirsArray []Folder
	var filesArray []File
	unescaped := s.UnEscapeAddress(path)

	ok := s.CheckPathConsistency(unescaped)
	if !ok {
		return fmt.Errorf("provided path is not valid or consistent")
	}

	err = s.ScanDir(unescaped, &dirsArray, &filesArray)
	if err != nil {
		return
	}

	err = s.StoreDirData(&dirsArray, &filesArray)
	if err != nil {
		return
	}

	var dirPath string
	for _, dir := range dirsArray {
		dirPath = s.UnEscapeAddress(filepath.Join(dir.Path, dir.Name))
		err = s.Watcher.Add(dirPath)
		if err != nil {
			s.Logger.Error("FS watcher add failed:", err)
		} else {
			s.Logger.Info(fmt.Sprintf("%s added to watcher", dirPath))
		}
	}

	return
}
