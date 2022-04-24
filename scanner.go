package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/lazybark/go-pretty-code/logs"
	"github.com/lazybark/pcloud-sync-server/config"
	"github.com/lazybark/pcloud-sync-server/models"

	"github.com/fsnotify/fsnotify"
	"gorm.io/gorm"
)

// EscapeAddress returns FS-safe filepath for storing in DB
func EscapeAddress(address string) string {
	address = strings.ReplaceAll(address, config.Current.FileSystemRootPath, "%ROOT_DIR%")
	return strings.ReplaceAll(address, string(filepath.Separator), ",")
}

// UnEscapeAddress returns correct filepath in current FS
func UnEscapeAddress(address string) string {
	address = strings.ReplaceAll(address, "%ROOT_DIR%", config.Current.FileSystemRootPath)
	return strings.ReplaceAll(address, ",", string(filepath.Separator))
}

// CheckPathConsistency checks path for being part of root dir, absolute, existing and directory
func CheckPathConsistency(path string) bool {
	// Check if path belongs to root dir
	if ok := strings.Contains(path, config.Current.FileSystemRootPath); !ok {
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

// ProcessDirectory scans dir and fills DB with dir's data
func ProcessDirectory(path string, db *gorm.DB, watch *fsnotify.Watcher, logger *logs.Logger) (err error) {
	var dirsArray []models.SyncFolder
	var filesArray []models.SyncFile
	unescaped := UnEscapeAddress(path)

	ok := CheckPathConsistency(unescaped)
	if !ok {
		return fmt.Errorf("provided path is not valid or consistent")
	}

	err = ScanDir(unescaped, &dirsArray, &filesArray, logger)
	if err != nil {
		return
	}

	err = StoreDirData(&dirsArray, &filesArray, db)
	if err != nil {
		return
	}

	var dirPath string
	for _, dir := range dirsArray {
		dirPath = UnEscapeAddress(filepath.Join(dir.Path, dir.Name))
		err = watch.Add(dirPath)
		if err != nil {
			logger.Error("FS watcher add failed:", err)
		} else {
			logger.Info(fmt.Sprintf("%s added to watcher", dirPath))
		}
	}

	return
}

// ScanDir scans dir contents and fills inputing slices with file and dir models
func ScanDir(path string, dirs *[]models.SyncFolder, files *[]models.SyncFile, logger *logs.Logger) (err error) {
	var dirsRec []string
	var dir models.SyncFolder
	var file models.SyncFile

	contents, err := ioutil.ReadDir(path)
	if err != nil {
		return
	}

	for _, item := range contents {
		if item.IsDir() {
			// Send dirname for recursive scanning
			dirsRec = append(dirsRec, item.Name())
			// Append to dirs list for output
			dir = models.SyncFolder{
				Name:        item.Name(),
				Path:        EscapeAddress(path),
				Size:        item.Size(),
				FSUpdatedAt: item.ModTime(),
			}
			*dirs = append(*dirs, dir)
		} else {
			hash := ""
			hash, err = FileHash(filepath.Join(path, item.Name()))
			if err != nil /*&& errors.Is(err, os.SyscallError)*/ {
				logger.Error(err)
			}

			file = models.SyncFile{
				Name:        item.Name(),
				Size:        item.Size(),
				Hash:        hash,
				Path:        EscapeAddress(path),
				FSUpdatedAt: item.ModTime(),
				Type:        filepath.Ext(item.Name()),
			}
			// Append to list for output
			*files = append(*files, file)
		}
	}

	// Recursively scan all sub dirs
	for _, dirName := range dirsRec {
		err = ScanDir(filepath.Join(path, dirName), dirs, files, logger)
		if err != nil {
			return err
		}
	}

	return
}

// StoreDirData fills DB with file and dir data provided
func StoreDirData(dirs *[]models.SyncFolder, files *[]models.SyncFile, db *gorm.DB) (err error) {
	if err = db.Table("sync_folders").Save(dirs).Error; err != nil && err != gorm.ErrEmptySlice {
		return
	}
	if err = db.Table("sync_files").Save(files).Error; err != nil && err != gorm.ErrEmptySlice {
		return
	}

	return nil
}

func MakeDBRecord(item fs.FileInfo, path string, db *gorm.DB, logger *logs.Logger) error {
	dir, _ := filepath.Split(path)
	dir = strings.TrimSuffix(dir, string(filepath.Separator))

	if item.IsDir() {
		record := models.SyncFolder{
			Name:        item.Name(),
			Path:        EscapeAddress(dir),
			Size:        item.Size(),
			FSUpdatedAt: item.ModTime(),
		}
		if err := db.Table("sync_folders").Save(&record).Error; err != nil && err != gorm.ErrEmptySlice {
			return err
		}
	} else {
		hash := ""
		hash, err := FileHash(path)
		if err != nil {
			logger.Error(err)
		}

		record := models.SyncFile{
			Name:        item.Name(),
			Size:        item.Size(),
			Hash:        hash,
			Path:        EscapeAddress(dir),
			FSUpdatedAt: item.ModTime(),
			Type:        filepath.Ext(item.Name()),
		}
		if err = db.Table("sync_files").Save(&record).Error; err != nil && err != gorm.ErrEmptySlice {
			return err
		}
	}
	return nil
}

func FileHash(path string) (hash string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	r := bufio.NewReader(file)

	hasher := sha256.New()

	// Read in 8kb parts
	buf := make([]byte, 8192)
	n := 0

	for {
		n, err = r.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}
		_, err = hasher.Write(buf[:n])
		if err != nil {
			return
		}
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
