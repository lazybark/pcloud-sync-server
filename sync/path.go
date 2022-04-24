package sync

import (
	"os"
	"path/filepath"
	"strings"
)

// EscapeAddress returns FS-safe filepath for storing in DB
func (s *Server) EscapeAddress(address string) string {
	address = strings.ReplaceAll(address, s.Config.FileSystemRootPath, "%ROOT_DIR%")
	return strings.ReplaceAll(address, string(filepath.Separator), ",")
}

// UnEscapeAddress returns correct filepath in current FS
func (s *Server) UnEscapeAddress(address string) string {
	address = strings.ReplaceAll(address, "%ROOT_DIR%", s.Config.FileSystemRootPath)
	return strings.ReplaceAll(address, ",", string(filepath.Separator))
}

// CheckPathConsistency checks path for being part of root dir, absolute, existing and directory
func (s *Server) CheckPathConsistency(path string) bool {
	// Check if path belongs to root dir
	if ok := strings.Contains(path, s.Config.FileSystemRootPath); !ok {
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
