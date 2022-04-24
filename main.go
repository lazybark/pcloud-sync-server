package main

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/pcloud-sync-server/config"
	"github.com/lazybark/pcloud-sync-server/db"
	"github.com/lazybark/pcloud-sync-server/sync"
	"go.uber.org/zap"

	"github.com/lazybark/go-pretty-code/logs"
)

var (
	AppVersion = "0.0.0 2022.04.22"
)

func main() {
	//To DO: -v flag for server verbose logging, -f for filesystem verbose logging
	//-s flag for total silent mode
	//-t for full user+token truncate at start
	//-b for backing up the DB
	//--stats
	//-c use colorful output in console (true by default)

	//TO DO: divide files and folders between users
	//TO DO: encode files on the fly
	// Single client mode - delete all tokens if new registered

	// Duplicate servers to avoid data loss
	// Data archiving and file versioning

	/// QR-код, чтобы поделиться файлом
	// Полноценная возможность делиться файлами из приватного хранилища

	// Полностью консольная версия облака

	start := time.Now()
	// Get config into config.Current struct
	err := config.LoadConfig(".")
	if err != nil {
		log.Fatal("Error getting config: ", err)
	}
	// Connect Logger
	logfileName := fmt.Sprintf("pcloud-sync-server_%v-%v-%v_%v-%v-%v.log", start.Year(), start.Month(), start.Day(), start.Hour(), start.Minute(), start.Second())
	logger, err := logs.Double(filepath.Join(config.Current.LogDirMain, logfileName), false, zap.InfoLevel)
	if err != nil {
		log.Fatal("Error getting logger: ", err)
	}
	logger.InfoCyan("App Version: ", AppVersion)
	// Connect DB
	sqlite := db.OpenSQLite(logger)
	// Connect FS watcher
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Fatal("New Watcher failed:", err)
	}
	defer watch.Close()

	// Force rescan filesystem and flush old DB-records
	err = db.Init(sqlite)
	if err != nil {
		logger.Fatal("Error flushing DB: ", err)
	}

	// Watch root dir
	logger.InfoGreen("Starting watcher")
	go FilesystemWatcherRoutine(logger, sqlite, watch)
	// Process and watch all subdirs
	err = ProcessDirectory(config.Current.FileSystemRootPath, sqlite, watch, logger)
	if err != nil {
		logger.Fatal("Error processing FS: ", err)
	}
	logger.InfoGreen("Root directory processed. Total time: ", time.Since(start))

	//users.CreateUserCLI(sqlite)

	logger.InfoGreen("Starting server")
	sync.Start(logger, sqlite, watch)
}
