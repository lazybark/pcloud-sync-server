package sync

import "time"

type (
	// Statistics
	Statistics struct {
		ID                 uint `gorm:"primaryKey"`
		Date               time.Time
		TotalBytesRecieved int
		TotalBytesSent     int
		FilesSent          int
		FilesRecieved      int
		ActiveUsers        int
		ActiveClients      int
	}
)
