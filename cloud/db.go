package cloud

import (
	"log"
	"os"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gLogger "gorm.io/gorm/logger"
)

// OpenSQLite opens or creates a SQLite file
func OpenSQLite(dbName string) (db *gorm.DB, err error) {
	db, err = gorm.Open(sqlite.Open(dbName), &gorm.Config{Logger: gLogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gLogger.Config{LogLevel: gLogger.Silent},
	)})

	return
}
