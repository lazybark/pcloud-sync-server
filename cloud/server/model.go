package server

import (
	"net"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/pcloud-sync-server/cloud/events"
	"github.com/lazybark/pcloud-sync-server/fsworker"
	"gorm.io/gorm"
)

type (
	AppLevel int

	Server struct {
		appVersion      string
		appVersionLabel string
		appLevel        AppLevel
		config          *Config
		db              *gorm.DB
		// Channel to send events that occur in the watcher filesystem
		fsEventsChan chan (fsnotify.Event)
		// serverDone signals that server has been stopped
		serverDoneChan      chan (bool)
		externalStartedChan chan (bool)
		// Mutex for writing into activeConnections
		connectionPoolMutex     sync.RWMutex
		activeConnections       []ActiveConnection
		activeConnectionsNumber int

		watcher   *fsnotify.Watcher
		fswOnline bool
		timeStart time.Time
		fw        *fsworker.Fsworker
		// evProc is the interface which recieves all events occuring in the server
		evProc EventProcessor
		// ewg is the waitgroup for events processor, in case server interrupt will happen before all events are done
		ewg *sync.WaitGroup
	}

	FSEventNotification struct {
		// Event in fsnotify.Event format (with Op, Name fields and Op.String() method)
		Event fsnotify.Event
		// Filesystem object (file or directory)
		Object interface{}
	}

	EventProcessor interface {
		// Send message that should be processed at any circumstances
		Send(events.Level, string, interface{})
		// Send message that should be ignored in case verbosity is set to false
		SendVerbose(events.Level, string, interface{})
		// Signal EventProcessor that there will be no events anymore
		Close()
	}

	ActiveConnection struct {
		EventsChan     chan (FSEventNotification)
		Active         bool
		IP             net.Addr
		DeviceName     string
		BytesSent      int
		BytesRecieved  int
		ClientErrors   uint
		ServerErrors   uint
		Uid            uint
		NumerInPool    int
		Token          string
		ConnectAt      time.Time
		LastOperation  time.Time
		DisconnectedAt time.Time
		StateChan      chan (ConnectionEvent) // Channel for closing the sync routine
		SyncActive     bool                   // If the client has been authed and has an active sync messages channel

	}

	ConnectionEvent int
)

const (
	ConnectionClose ConnectionEvent = iota + 1
	ConnectionSyncStart
	ConnectionSyncStop
)

const (
	ALServerBase AppLevel = iota + 1
	ALServerBackup
)

func (l AppLevel) String() string {

	levelNames := [...]string{"Unknown", "Base sync server", "Backup server"}
	if l > AppLevel(len(levelNames)) {
		return "Unknown"
	}

	return levelNames[l]
}
