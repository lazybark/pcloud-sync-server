package sync

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
)

func (s *Server) FileHash(path string) (hash string, err error) {
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

func (s *Server) MakeDBRecord(item fs.FileInfo, path string) error {
	dir, _ := filepath.Split(path)
	dir = strings.TrimSuffix(dir, string(filepath.Separator))

	if item.IsDir() {
		record := Folder{
			Name:        item.Name(),
			Path:        s.EscapeAddress(dir),
			Size:        item.Size(),
			FSUpdatedAt: item.ModTime(),
		}
		if err := s.DB.Model(&Folder{}).Save(&record).Error; err != nil && err != gorm.ErrEmptySlice {
			return err
		}
	} else {
		hash := ""
		hash, err := s.FileHash(path)
		if err != nil {
			s.Logger.Error(err)
		}

		record := File{
			Name:        item.Name(),
			Size:        item.Size(),
			Hash:        hash,
			Path:        s.EscapeAddress(dir),
			FSUpdatedAt: item.ModTime(),
			Type:        filepath.Ext(item.Name()),
		}
		if err = s.DB.Model(&File{}).Save(&record).Error; err != nil && err != gorm.ErrEmptySlice {
			return err
		}
	}
	return nil
}
