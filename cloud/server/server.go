package server

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/pcloud-sync-server/cloud"
	"github.com/lazybark/pcloud-sync-server/cloud/events"
	"github.com/lazybark/pcloud-sync-server/fsworker"
)

var (
	Version      = "0.0.0-alpha 2022.04.29"
	VersionLabel = "unstable pre-alpha"
)

// NewSyncServer creates a sync server instance
func NewServer() *Server {
	return new(Server)
}

func (s *Server) Start(started chan bool) {
	s.timeStart = time.Now()
	s.appVersion = Version
	s.appVersionLabel = VersionLabel
	s.appLevel = ALServerBase
	s.ewg = &sync.WaitGroup{}
	s.externalStartedChan = started
	s.serverDoneChan = make(chan bool)
	// Get config into config.Current struct
	err := s.LoadConfig(".")
	if err != nil {
		log.Fatal("Error getting config: ", err)
	}

	// Start logging routine
	logfileName := fmt.Sprintf("pcloud-sync-server_%v-%v-%v_%v-%v-%v.log", s.timeStart.Year(), s.timeStart.Month(), s.timeStart.Day(), s.timeStart.Hour(), s.timeStart.Minute(), s.timeStart.Second())

	// Catch events
	s.evProc = events.NewProcessor(filepath.Join(s.config.LogDirMain, logfileName), true)
	// Log current version
	s.evProc.Send(EventType("cyan"), events.SourceSyncServer.String(), fmt.Sprintf("App Version: %s", s.appVersion))
	// Launch stat logger routine
	if s.config.LogStats {
		go s.LogStats()
	}
	// Connect DB
	s.db, err = cloud.OpenSQLite(s.config.SQLiteDBName)
	if err != nil {
		s.evProc.Send(EventType("fatal"), events.SourceSyncServer.String(), fmt.Errorf("[Start] OpenSQLite failed: %w", err))
	}
	// Start FSEventsProcessor + connection pool manager
	s.fsEventsChan = make(chan fsnotify.Event)
	go s.FSEventsProcessor()
	// Connect FS watcher
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		s.evProc.Send(EventType("fatal"), events.SourceSyncServer.String(), fmt.Errorf("new Watcher failed: %w", err))
	}
	s.watcher = watch
	// Force rescan filesystem and flush old DB-records
	s.InitDB()
	if err != nil {
		s.evProc.Send(EventType("fatal"), events.SourceSyncServer.String(), fmt.Errorf("error in InitDB: %w", err))
	}
	// New filesystem worker
	s.fw = fsworker.NewWorker(s.config.FileSystemRootPath, s.db, watch)

	//users.CreateUserCLI(s.DB)

	// Watch root dir
	go s.FilesystemWatcherRoutine()
	// Process and watch all subdirs
	s.evProc.SendVerbose(EventType("green"), events.SourceSyncServer.String(), "Processing root directory")
	rpStart := time.Now()
	files, dirs, err := s.fw.ProcessDirectory(s.config.FileSystemRootPath)
	if err != nil {
		s.evProc.Send(EventType("fatal"), events.SourceSyncServer.String(), fmt.Errorf("error processing directory: %w", err))
	}
	s.evProc.SendVerbose(EventType("green"), events.SourceSyncServer.String(), fmt.Sprintf("Root directory processed. Total %d files in %d directories. Time: %v", files, dirs, time.Since(rpStart)))

	s.evProc.SendVerbose(EventType("green"), events.SourceSyncServer.String(), "Starting server")

	go s.AdminRoutine()

	go s.Listen()

	time.Sleep(1 * time.Second)
	s.externalStartedChan <- true
}

func (s *Server) LogStats() {
	for {
		time.Sleep(5 * time.Minute)
		s.evProc.Send(EventType("magenta"), events.SourceSyncServerStats.String(), "Server stats: \n - active users = 0\n - active connections = 0\n - data recieved = 0 Gb\n - data sent = 0 Gb\n - errors last 15 min / hour / 24 hours = 0/0/0")
	}
}

// EventType returns event type according to pcloud-sync-server/cloud/events Events model
func EventType(name string) events.Level {
	if name == "info" {
		return events.InfoGreen
	} else if name == "green" {
		return events.InfoGreen
	} else if name == "cyan" {
		return events.InfoCyan
	} else if name == "red" {
		return events.InfoRed
	} else if name == "magenta" {
		return events.InfoMagenta
	} else if name == "yellow" {
		return events.InfoYellow
	} else if name == "backred" {
		return events.InfoBackRed
	} else if name == "fatal" {
		return events.Fatal
	} else if name == "warn" {
		return events.Warn
	} else if name == "error" {
		return events.Error
	}
	return events.UnknownLevel
}

func (s *Server) closeConnections() {
	for _, c := range s.activeConnections {
		if !c.Active { //
			continue
		}

		select {
		case c.StateChan <- ConnectionClose:
			s.evProc.SendVerbose(EventType("info"), events.SourceAdminRoutine.String(), fmt.Sprintf("(%v) closed", c.IP))
		default:

		}
	}
}

func (s *Server) Stop() {
	s.serverDoneChan <- true
}

func (s *Server) AdminRoutine() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	var sig os.Signal
	for {
		select {
		case sig = <-c:
			s.evProc.Send(EventType("red"), events.SourceAdminRoutine.String(), fmt.Sprintf("Got %s signal", sig))
			s.evProc.SendVerbose(EventType("red"), events.SourceSyncServer.String(), "Stopping server")
			// Close filesystem watcher
			err := s.watcher.Close()
			if err != nil {
				s.evProc.Send(EventType("error"), events.SourceAdminRoutine.String(), fmt.Errorf("[AdminRoutine] error closing fs watcher: %w", err))
			}
			// Signal active clients that server will be stopped
			s.closeConnections()
			// Log server closing
			s.evProc.Send(EventType("backred"), events.SourceAdminRoutine.String(), fmt.Sprintf("Server stopped. Was online for %v", time.Since(s.timeStart)))
			s.evProc.Close()
			os.Exit(1)
		case done := <-s.serverDoneChan:
			if !done {
				continue
			}
			// Send active clients connection breaker
			s.evProc.SendVerbose(EventType("red"), events.SourceAdminRoutine.String(), "Stopping server")
			// TO DO: add Listen breaker
			// here
			// Close filesystem watcher
			s.watcher.Close()
			// Signal clients that server will be stopped
			s.closeConnections()
			// Log server closing
			s.evProc.Send(EventType("backred"), events.SourceAdminRoutine.String(), fmt.Sprintf("Server stopped. Was online for %v", time.Since(s.timeStart)))
			s.evProc.Close()

		}
	}

}
