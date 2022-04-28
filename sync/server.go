package sync

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/go-pretty-code/logs"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	AppVersion = "0.0.0 2022.04.22"
)

// NewSyncServer creates a sync server instance
func NewSyncServer() *Server {
	return new(Server)
}

func (s *Server) Start() {
	s.TimeStart = time.Now()
	s.AppVersion = AppVersion
	// Get config into config.Current struct
	err := s.LoadConfig(".")
	if err != nil {
		log.Fatal("Error getting config: ", err)
	}

	// Connect Logger
	logfileName := fmt.Sprintf("pcloud-sync-server_%v-%v-%v_%v-%v-%v.log", s.TimeStart.Year(), s.TimeStart.Month(), s.TimeStart.Day(), s.TimeStart.Hour(), s.TimeStart.Minute(), s.TimeStart.Second())
	logger, err := logs.Double(filepath.Join(s.Config.LogDirMain, logfileName), false, zap.InfoLevel)
	if err != nil {
		log.Fatal("Error getting logger: ", err)
	}
	s.Logger = logger

	s.Logger.InfoCyan("App Version: ", s.AppVersion)
	if s.Config.LogStats {
		go s.LogStats()
	}

	// Connect DB
	s.OpenSQLite()

	// Start connection pool manager
	s.ConnNotifier = make(chan fsnotify.Event)
	defer close(s.ConnNotifier)
	go s.ConnectionsPool()

	// Connect FS watcher
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		s.Logger.Fatal("New Watcher failed:", err)
	}
	s.Watcher = watch

	// Force rescan filesystem and flush old DB-records
	s.InitDB()
	if err != nil {
		s.Logger.Fatal("Error flushing DB: ", err)
	}

	//users.CreateUserCLI(s.DB)

	// Watch root dir
	s.Logger.InfoGreen("Starting watcher")
	go s.FilesystemWatcherRoutine()

	// Process and watch all subdirs
	err = s.ProcessDirectory(s.Config.FileSystemRootPath)
	if err != nil {
		s.Logger.Fatal("Error processing FS: ", err)
	}
	s.Logger.InfoGreen("Root directory processed. Total time: ", time.Since(s.TimeStart))

	s.Logger.InfoGreen("Starting server")
	s.Listen()
}

func (s *Server) ConnectionsPool() {
	s.Logger.InfoCyan("Connectios pool started")
	// Endless await
	for {
		select {
		case event, ok := <-s.ConnNotifier:
			if !ok {
				return
			}
			for _, c := range s.ActiveConnections {
				if !c.SyncActive { // Only share data with connections that are intended to sync
					continue
				}
				c.EventsChan <- event
				fmt.Println("Event sent")
			}
		}
	}
}

// LoadConfig reads configuration from file or environment variables.
func (s *Server) LoadConfig(path string) (err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("json")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	err = viper.Unmarshal(&s.Config)
	if err != nil {
		return
	}

	return
}

func (s *Server) LogStats() {
	for {
		time.Sleep(5 * time.Minute)
		s.Logger.InfoMagenta("Server stats: \n - active users = 0\n - active connections = 0\n - data recieved = 0 Gb\n - data sent = 0 Gb\n - errors last 15 min / hour / 24 hours = 0/0/0")
	}
}

func (s *Server) InterruptCatcher() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	select {
	case sig := <-c:
		s.Watcher.Close()
		s.Logger.InfoRed(fmt.Sprintf("Got %s signal. Closing connections...", sig))
		// Send active clients connection breaker
		for _, c := range s.ActiveConnections {
			if !c.Active { //
				continue
			}

			select {
			case c.StateChan <- ConnectionClose:
				s.Logger.Info(fmt.Sprintf("(%v) closed", c.IP))
			default:

			}
		}
		s.Logger.InfoBackRed("Server stopped")

		os.Exit(1)
	}
}
