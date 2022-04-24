package models

import "time"

type (

	// SyncFile represents file data (except it's bytes) to exchange current sync status information
	SyncFile struct {
		ID            uint `gorm:"primaryKey"`
		Hash          string
		Name          string `gorm:"uniqueIndex:file"`
		Path          string `gorm:"uniqueIndex:file"`
		Size          int64
		FSUpdatedAt   time.Time
		CreatedAt     time.Time
		UpdatedAt     time.Time
		CurrentStatus string
		LocationDirId int
		Type          string
	}

	// SyncFolder represents folder data to exchange current sync status information
	SyncFolder struct {
		ID            uint   `gorm:"primaryKey"`
		Name          string `gorm:"uniqueIndex:folder"`
		Path          string `gorm:"uniqueIndex:folder"`
		FSUpdatedAt   time.Time
		CreatedAt     time.Time
		UpdatedAt     time.Time
		CurrentStatus string
		LocationDirId int
		Items         int
		Size          int64
	}
)
