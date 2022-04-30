package fsworker

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/go-helpers/hasher"
	"github.com/lazybark/go-pretty-code/logs"
	"gorm.io/gorm"
)

type (
	Fsworker struct {
		Root    string
		DB      *gorm.DB
		Logger  *logs.Logger
		Watcher *fsnotify.Watcher
	}

	Filesystem struct {
		Folders []Folder
		Files   []File
	}

	// File represents file data (except it's bytes) to exchange current sync status information
	File struct {
		ID            uint `gorm:"primaryKey"`
		Hash          string
		Name          string `gorm:"uniqueIndex:file"`
		Path          string `gorm:"uniqueIndex:file"`
		Owner         uint
		Size          int64
		FSUpdatedAt   time.Time
		CreatedAt     time.Time
		UpdatedAt     time.Time
		CurrentStatus string
		LocationDirId int
		Type          string
	}

	// Folder represents folder data to exchange current sync status information
	Folder struct {
		ID            uint   `gorm:"primaryKey"`
		Name          string `gorm:"uniqueIndex:folder"`
		Path          string `gorm:"uniqueIndex:folder"`
		Owner         uint
		FSUpdatedAt   time.Time
		CreatedAt     time.Time
		UpdatedAt     time.Time
		CurrentStatus string
		LocationDirId int
		Items         int
		Size          int64
	}
)

func NewWorker(Root string, db *gorm.DB, logger *logs.Logger, watcher *fsnotify.Watcher) *Fsworker {
	fw := new(Fsworker)
	fw.Root = Root
	fw.Logger = logger
	fw.Watcher = watcher
	fw.DB = db

	return fw
}

func (f *Fsworker) MakeDBRecord(item fs.FileInfo, path string) error {
	dir, _ := filepath.Split(path)
	dir = strings.TrimSuffix(dir, string(filepath.Separator))

	if item.IsDir() {
		record := Folder{
			Name:        item.Name(),
			Path:        f.EscapeAddress(dir),
			Size:        item.Size(),
			FSUpdatedAt: item.ModTime(),
		}
		if err := f.DB.Model(&Folder{}).Save(&record).Error; err != nil && err != gorm.ErrEmptySlice {
			return err
		}
	} else {

		hash := ""
		hash, err := hasher.HashFilePath(path, hasher.SHA256, 8192)
		if err != nil {
			f.Logger.Error(err)
		}

		record := File{
			Name:        item.Name(),
			Size:        item.Size(),
			Hash:        hash,
			Path:        f.EscapeAddress(dir),
			FSUpdatedAt: item.ModTime(),
			Type:        filepath.Ext(item.Name()),
		}
		if err = f.DB.Model(&File{}).Save(&record).Error; err != nil && err != gorm.ErrEmptySlice {
			return err
		}
	}
	return nil
}

// ScanDir scans dir contents and fills inputing slices with file and dir models
func (f *Fsworker) ScanDir(path string, dirs *[]Folder, files *[]File) (err error) {
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
				Path:        f.EscapeAddress(path),
				Size:        item.Size(),
				FSUpdatedAt: item.ModTime(),
			}
			*dirs = append(*dirs, dir)
		} else {
			hash := ""
			hash, err = hasher.HashFilePath(filepath.Join(path, item.Name()), hasher.SHA256, 8192)
			if err != nil /*&& errors.Is(err, os.SyscallError)*/ {
				f.Logger.Error(err)
			}

			file = File{
				Name:        item.Name(),
				Size:        item.Size(),
				Hash:        hash,
				Path:        f.EscapeAddress(path),
				FSUpdatedAt: item.ModTime(),
				Type:        filepath.Ext(item.Name()),
			}
			// Append to list for output
			*files = append(*files, file)
		}
	}

	// Recursively scan all sub dirs
	for _, dirName := range dirsRec {
		err = f.ScanDir(filepath.Join(path, dirName), dirs, files)
		if err != nil {
			return err
		}
	}

	return
}

// StoreDirData fills DB with file and dir data provided
func (f *Fsworker) StoreDirData(dirs *[]Folder, files *[]File) (err error) {
	if err = f.DB.Model(&Folder{}).Save(dirs).Error; err != nil && err != gorm.ErrEmptySlice {
		return
	}
	if err = f.DB.Model(&File{}).Save(files).Error; err != nil && err != gorm.ErrEmptySlice {
		return
	}

	return nil
}

// ProcessDirectory scans dir and fills DB with dir's data
func (f *Fsworker) ProcessDirectory(path string) (err error) {
	var dirsArray []Folder
	var filesArray []File
	unescaped := f.UnEscapeAddress(path)

	ok := f.CheckPathConsistency(unescaped)
	if !ok {
		return fmt.Errorf("provided path is not valid or consistent")
	}

	err = f.ScanDir(unescaped, &dirsArray, &filesArray)
	if err != nil {
		return
	}

	err = f.StoreDirData(&dirsArray, &filesArray)
	if err != nil {
		return
	}

	var dirPath string
	for _, dir := range dirsArray {
		dirPath = f.UnEscapeAddress(filepath.Join(dir.Path, dir.Name))
		err = f.Watcher.Add(dirPath)
		if err != nil {
			f.Logger.Error("FS watcher add failed:", err)
		} else {
			f.Logger.Info(fmt.Sprintf("%s added to watcher", dirPath))
		}
	}

	return
}

// EscapeAddress returns FS-safe filepath for storing in DB
func (f *Fsworker) EscapeAddress(address string) string {
	address = strings.ReplaceAll(address, f.Root, "%ROOT_DIR%")
	return strings.ReplaceAll(address, string(filepath.Separator), ",")
}

// UnEscapeAddress returns correct filepath in current FS
func (f *Fsworker) UnEscapeAddress(address string) string {
	address = strings.ReplaceAll(address, "%ROOT_DIR%", f.Root)
	return strings.ReplaceAll(address, ",", string(filepath.Separator))
}

// CheckPathConsistency checks path for being part of root dir, absolute, existing and directory
func (f *Fsworker) CheckPathConsistency(path string) bool {
	// Check if path belongs to root dir
	if ok := strings.Contains(path, f.Root); !ok {
		return ok
	}
	// Only absolute paths are available
	if ok := filepath.IsAbs(path); !ok {
		return ok
	}
	// Must exist and be a directory
	dir, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !dir.IsDir() {
		return false
	}

	return true
}
