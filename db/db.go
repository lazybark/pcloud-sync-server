package db

import (
	"log"
	"os"

	"github.com/lazybark/go-pretty-code/logs"
	"github.com/lazybark/pcloud-sync-server/config"
	"github.com/lazybark/pcloud-sync-server/models"
	"github.com/lazybark/pcloud-sync-server/sync"
	"github.com/lazybark/pcloud-sync-server/users"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gLogger "gorm.io/gorm/logger"
)

// OpenSQLite opens or creates a SQLite file
func OpenSQLite(logger *logs.Logger) *gorm.DB {
	sqlite, err := gorm.Open(sqlite.Open(config.Current.SQLiteDBName), &gorm.Config{Logger: gLogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gLogger.Config{LogLevel: gLogger.Silent},
	)})
	if err != nil {
		logger.Fatal("Error opening DB: ", err)
	}

	return sqlite
}

// Init drops all tables in DB and creates new ones
func Init(db *gorm.DB) (err error) {
	err = db.Migrator().DropTable(&models.SyncFile{}, &models.SyncFolder{})
	if err != nil {
		return
	}
	err = db.AutoMigrate(&users.User{}, &users.Client{}, &models.SyncFile{}, &models.SyncFolder{}, &sync.Statistics{})

	return
}
