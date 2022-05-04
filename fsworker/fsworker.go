package fsworker

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/go-helpers/hasher"
	"gorm.io/gorm"
)

type (
	Fsworker struct {
		Root    string
		DB      *gorm.DB
		Watcher *fsnotify.Watcher
	}

	Filesystem struct {
		Folders []Folder
		Files   []File
	}

	// File represents file data into DB
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

func NewWorker(Root string, db *gorm.DB, watcher *fsnotify.Watcher) *Fsworker {
	fw := new(Fsworker)
	fw.Root = Root
	fw.Watcher = watcher
	fw.DB = db

	return fw
}

func (f *Fsworker) RecordFile(record File) error {
	if err := f.DB.Model(&File{}).Save(&record).Error; err != nil && err != gorm.ErrEmptySlice {
		return err
	}
	return nil
}

func (f *Fsworker) RecordDir(record Folder) error {
	if err := f.DB.Model(&Folder{}).Save(&record).Error; err != nil && err != gorm.ErrEmptySlice {
		return err
	}
	return nil
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
			return err
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

func CheckEscaped(path string) bool {
	return strings.Contains(path, ",")
}

func (f *Fsworker) GetOwner(path string) uint {
	// ownerId will extract user id that owns directory in root
	var ownerId = regexp.MustCompile(`%ROOT_DIR%,(\d*)`)
	// Only work with escaped addresses to avoid ambiguity
	if !CheckEscaped(path) {
		path = f.EscapeAddress(path)
	}
	user := ownerId.FindStringSubmatch(path)

	if len(user) < 2 {
		return 0
	}

	userReal, err := strconv.Atoi(user[1])
	if err != nil {
		return 0
	}

	return uint(userReal)
}
func (f *Fsworker) ProcessFolder(oInfo fs.FileInfo, fullPath string) (folder Folder, err error) {
	if !oInfo.IsDir() {
		return folder, fmt.Errorf("[ProcessFolder] provided object is NOT a dir")
	}

	dir, _ := filepath.Split(fullPath)
	dir = strings.TrimSuffix(dir, string(filepath.Separator))

	folder.Name = oInfo.Name()
	folder.FSUpdatedAt = oInfo.ModTime()
	folder.Path = f.EscapeAddress(dir)
	folder.Size = oInfo.Size()
	folder.FSUpdatedAt = oInfo.ModTime()
	folder.Owner = f.GetOwner(dir)

	err = f.RecordDir(folder)
	if err != nil && err.Error() != "UNIQUE constraint failed: files.name, files.path" {
		return folder, fmt.Errorf("error making record for %s: %w", fullPath, err)
	}

	return
}

func (f *Fsworker) ProcessFile(oInfo fs.FileInfo, fullPath string) (file File, err error) {
	if oInfo.IsDir() {
		return file, fmt.Errorf("[ProcessFile] provided object is a dir")
	}
	var hash string
	dir, _ := filepath.Split(fullPath)
	dir = strings.TrimSuffix(dir, string(filepath.Separator))

	// retry lets us avoid most stupid errors, like "the previous app didn't release it as fast as we would like"
	retry := 0
	for {
		hash, err = hasher.HashFilePath(fullPath, hasher.SHA256, 8192)
		if err == nil {
			break
		} else if err != nil && retry < 10 {
			time.Sleep(100 * time.Millisecond)
		} else {
			return file, fmt.Errorf("error getting hash: %w", err)
		}
	}

	file.Name = oInfo.Name()
	file.FSUpdatedAt = oInfo.ModTime()
	file.Path = f.EscapeAddress(dir)
	file.Hash = hash
	file.Size = oInfo.Size()
	file.FSUpdatedAt = oInfo.ModTime()
	file.Type = filepath.Ext(oInfo.Name())
	file.Owner = f.GetOwner(dir)

	err = f.RecordFile(file)
	if err != nil && err.Error() != "UNIQUE constraint failed: files.name, files.path" {
		return file, fmt.Errorf("error making record for %s: %w", fullPath, err)
	}

	return
}

func (f *Fsworker) ScanObject(path string) (oInfo fs.FileInfo, err error) {
	oInfo, err = os.Stat(path)
	if err != nil {
		return oInfo, fmt.Errorf("[ScanFile] os.Stat failed: %w", err)
	}

	return
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
		p := filepath.Join(path, item.Name())
		if item.IsDir() {
			// Send dirname for recursive scanning
			dirsRec = append(dirsRec, item.Name())
			// Append to dirs list for output
			dir, err = f.ProcessFolder(item, p)
			if err != nil {
				return fmt.Errorf("[ScanDir] error processing folder %s: %w", p, err)
			}
			*dirs = append(*dirs, dir)
		} else {
			file, err = f.ProcessFile(item, p)
			if err != nil {
				return fmt.Errorf("[ScanDir] error processing file %s: %w", p, err)
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
	/*if err = f.DB.Model(&Folder{}).Save(dirs).Error; err != nil && err != gorm.ErrEmptySlice {
		return
	}*/
	/*if err = f.DB.Model(&File{}).Save(files).Error; err != nil && err != gorm.ErrEmptySlice {
		return
	}*/

	return nil
}

// ProcessDirectory scans dir and fills DB with dir's data
func (f *Fsworker) ProcessDirectory(path string) (files int, dirs int, err error) {
	var dirsArray []Folder
	var filesArray []File
	unescaped := f.UnEscapeAddress(path)

	ok := f.CheckPathConsistency(unescaped)
	if !ok {
		return files, dirs, fmt.Errorf("provided path is not valid or consistent")
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
			return files, dirs, err
		} else {
			fmt.Printf("%s added to watcher\n", dirPath)
		}
	}

	files = len(filesArray)
	dirs = len(dirsArray)

	return
}

// EscapeAddress returns FS-safe filepath for storing in DB
func EscapeAddress(address string, root string) string {
	address = strings.ReplaceAll(address, root, "%ROOT_DIR%")
	return strings.ReplaceAll(address, string(filepath.Separator), ",")
}

// UnEscapeAddress returns correct filepath in current FS
func UnEscapeAddress(address string, root string) string {
	address = strings.ReplaceAll(address, "%ROOT_DIR%", root)
	return strings.ReplaceAll(address, ",", string(filepath.Separator))
}

// EscapeAddress returns FS-safe filepath for storing in DB
func (f *Fsworker) EscapeAddress(address string) string {
	address = strings.ReplaceAll(address, f.Root, "%ROOT_DIR%")
	return strings.ReplaceAll(address, string(filepath.Separator), ",")
}

func (f *Fsworker) EscapeWithUser(address string, u int) string {
	address = strings.ReplaceAll(address, fmt.Sprint(u)+string(filepath.Separator), "")
	address = strings.ReplaceAll(address, f.Root, "%ROOT_DIR%")
	return strings.ReplaceAll(address, string(filepath.Separator), ",")
}

func (f *Fsworker) ExtractUser(address string, u uint) string {
	return strings.ReplaceAll(address, "%ROOT_DIR%,"+fmt.Sprint(u), "%ROOT_DIR%")
}

func (f *Fsworker) InsertUser(address string, u uint) string {
	return strings.ReplaceAll(address, "%ROOT_DIR%", "%ROOT_DIR%,"+fmt.Sprint(u))
}

// UnEscapeAddress returns correct filepath in current FS
func (f *Fsworker) UnEscapeAddress(address string) string {
	address = strings.ReplaceAll(address, "%ROOT_DIR%", f.Root)
	return strings.ReplaceAll(address, ",", string(filepath.Separator))
}

// CheckPathConsistency checks path for being part of root dir, absolute, existing and a directory
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
