package sync

import (
	"net"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/go-pretty-code/logs"
	"gorm.io/gorm"
)

var (
	MessageTerminator = byte('\n')
	ConnectionCloser  = "CLOSE_CONNECTION"
)

type (
	Server struct {
		ChannelMutex            sync.RWMutex
		Config                  *Config
		AppVersion              string
		Logger                  *logs.Logger
		Watcher                 *fsnotify.Watcher
		DB                      *gorm.DB
		TimeStart               time.Time
		ConnNotifier            chan (fsnotify.Event)
		ActiveConnections       []ActiveConnection
		ActiveConnectionsNumber int
	}

	ConfigClient struct {
		Login         string
		Password      string
		Token         string
		ServerAddress string
		ServerPort    int
		DeviceName    string
		UserName      string
		DeviceLabel   string
	}

	ActiveConnection struct {
		EventsChan     chan (fsnotify.Event)
		Active         bool
		IP             net.Addr
		DeviceName     string
		ClientErrors   uint
		ServerErrors   uint
		NumerInPool    int
		ConnectAt      time.Time
		LastOperation  time.Time
		DisconnectedAt time.Time
		StateChan      chan (ConnectionEvent) // Channel for closing the sync routine
		SyncActive     bool                   // If the client has been authed and has an active sync messages channel
	}

	ConnectionEvent int

	// Config is a struct to define server behaviour
	Config struct {
		ServerToken              string `mapstructure:"SERVER_TOKEN"`
		CertPath                 string `mapstructure:"CERT_PATH"`
		KeyPath                  string `mapstructure:"KEY_PATH"`
		HostName                 string `mapstructure:"HOST_NAME"`
		Port                     string `mapstructure:"PORT"`
		MaxClientErrors          uint   // Limit for client-side errors (or any other party) until problematic connection will be closed and ErrTooMuchClientErrors sent
		MaxServerErrors          uint   // Limit for server-side errors until problematic connection will be closed and ErrTooMuchServerErrors sent
		LogStats                 bool
		CollectStats             bool
		TokenValidDays           int
		ServerVerboseLogging     bool   `mapstructure:"SERVER_VERBOSE_LOGGING"`
		CountStats               bool   `mapstructure:"COUNT_STATS"`
		FilesystemVerboseLogging bool   `mapstructure:"FILESYSTEM_VERBOSE_LOGGING"`
		SilentMode               bool   `mapstructure:"SILENT_MODE"`
		LogDirMain               string `mapstructure:"LOG_DIR_MAIN"`
		FileSystemRootPath       string `mapstructure:"FILE_SYSTEM_ROOT_PATH"`
		SQLiteDBName             string `mapstructure:"SQLITE_DB_NAME"`
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

const (
	ConnectionClose ConnectionEvent = iota + 1
	ConnectionSyncStart
	ConnectionSyncStop
)
