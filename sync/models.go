package sync

import (
	"net"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/go-pretty-code/logs"
	"github.com/lazybark/pcloud-sync-server/fsworker"
	"gorm.io/gorm"
)

var (
	MessageTerminator       = byte('\n')
	MessageTerminatorString = "\n"
	ConnectionCloser        = "CLOSE_CONNECTION"
)

type (
	ConnNotifierEvent struct {
		Event  fsnotify.Event
		Object interface{}
	}

	Server struct {
		ChannelMutex            sync.RWMutex
		Config                  *ConfigServer
		AppVersion              string
		Logger                  *logs.Logger
		Watcher                 *fsnotify.Watcher
		DB                      *gorm.DB
		TimeStart               time.Time
		ConnNotifier            chan (ConnNotifierEvent)
		ActiveConnections       []ActiveConnection
		ActiveConnectionsNumber int
		FW                      *fsworker.Fsworker
	}

	Client struct {
		AppVersion         string
		Config             *ConfigClient
		Logger             *logs.Logger
		Watcher            *fsnotify.Watcher
		ConnNotifier       chan (fsnotify.Event)
		DB                 *gorm.DB
		TimeStart          time.Time
		ServerToken        string
		FW                 *fsworker.Fsworker
		CurrentToken       string
		SyncActive         bool
		FileGetter         chan (GetFile)
		FilesInRow         []GetFile
		ActionsBuffer      map[string][]BufferedAction
		ActionsBufferMutex sync.RWMutex
	}

	BufferedAction struct {
		Action    fsnotify.Op
		Skipped   bool
		Timestamp time.Time
	}

	GetFile struct {
		Name      string
		Path      string
		Hash      string
		UpdatedAt time.Time
	}

	ConfigClient struct {
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

	ActiveConnection struct {
		EventsChan     chan (ConnNotifierEvent)
		Active         bool
		IP             net.Addr
		DeviceName     string
		BytesSent      int
		BytesRecieved  int
		ClientErrors   uint
		ServerErrors   uint
		NumerInPool    int
		Token          string
		ConnectAt      time.Time
		LastOperation  time.Time
		DisconnectedAt time.Time
		StateChan      chan (ConnectionEvent) // Channel for closing the sync routine
		SyncActive     bool                   // If the client has been authed and has an active sync messages channel

	}

	ConnectionEvent int

	// Config is a struct to define server behaviour
	ConfigServer struct {
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
		ServerName               string `mapstructure:"SERVER_NAME"`
		MaxConnectionsPerUser    int    `mapstructure:"MAX_USER_CONNECTIONS_PER_USER"`
	}
)

const (
	ConnectionClose ConnectionEvent = iota + 1
	ConnectionSyncStart
	ConnectionSyncStop
)
