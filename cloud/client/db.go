package client

import "github.com/lazybark/pcloud-sync-server/fsworker"

func (c *Client) InitDB() (err error) {
	err = c.DB.Migrator().DropTable(&fsworker.File{}, &fsworker.Folder{})
	if err != nil {
		return err
	}
	err = c.DB.AutoMigrate(&fsworker.File{}, &fsworker.Folder{})

	return
}
