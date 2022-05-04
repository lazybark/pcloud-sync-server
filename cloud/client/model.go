package client

import (
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/go-pretty-code/logs"
	"github.com/lazybark/pcloud-sync-server/cloud/proto"
	"github.com/lazybark/pcloud-sync-server/fsworker"
	"gorm.io/gorm"
)

type (
	Client struct {
		AppVersion         string
		Config             *Config
		Logger             *logs.Logger
		Watcher            *fsnotify.Watcher
		fsEventsChan       chan (fsnotify.Event)
		DB                 *gorm.DB
		TimeStart          time.Time
		ServerToken        string
		FW                 *fsworker.Fsworker
		CurrentToken       string
		SyncActive         bool
		FileGetter         chan (proto.GetFile)
		FilesInRow         []proto.GetFile
		ActionsBuffer      map[string][]BufferedAction
		ActionsBufferMutex sync.RWMutex
	}

	Config struct {
		Login              string `mapstructure:"LOGIN"`
		Password           string `mapstructure:"PASSWORD"`
		ServerCert         string `mapstructure:"CERT_PATH"`
		Token              string `mapstructure:"TOKEN"`
		ServerAddress      string `mapstructure:"SERVER_ADDRESS"`
		LogDirMain         string `mapstructure:"LOG_DIR_MAIN"`
		CacheDir           string `mapstructure:"CACHE_DIR"`
		ServerPort         int    `mapstructure:"SERVER_PORT"`
		DeviceName         string `mapstructure:"DEVICE_NAME"`
		UserName           string `mapstructure:"USER_NAME"`
		DeviceLabel        string `mapstructure:"DEVICE_LABEL"`
		FileSystemRootPath string `mapstructure:"FILE_SYSTEM_ROOT_PATH"`
		SQLiteDBName       string `mapstructure:"SQLITE_DB_NAME"`
	}

	BufferedAction struct {
		Action    fsnotify.Op
		Skipped   bool
		Timestamp time.Time
	}
)
