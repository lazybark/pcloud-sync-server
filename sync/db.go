package sync

import (
	"log"
	"os"

	"github.com/lazybark/pcloud-sync-server/users"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gLogger "gorm.io/gorm/logger"
)

// OpenSQLite opens or creates a SQLite file
func (s *Server) OpenSQLite() (err error) {
	sqlite, err := gorm.Open(sqlite.Open(s.Config.SQLiteDBName), &gorm.Config{Logger: gLogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gLogger.Config{LogLevel: gLogger.Silent},
	)})
	if err != nil {
		return err
	}

	s.DB = sqlite

	return
}

// Init drops all tables in DB and creates new ones
func (s *Server) InitDB() (err error) {
	err = s.DB.Migrator().DropTable(&File{}, &Folder{})
	if err != nil {
		return err
	}
	err = s.DB.AutoMigrate(&users.User{}, &users.Client{}, &File{}, &Folder{}, &Statistics{})

	return
}
