package server

import (
	f "github.com/lazybark/pcloud-sync-server/fsworker"
	u "github.com/lazybark/pcloud-sync-server/users"
)

func (s *Server) InitDB() (err error) {
	err = s.db.Migrator().DropTable(&f.File{}, &f.Folder{})
	if err != nil {
		return err
	}
	err = s.db.AutoMigrate(&u.User{}, &u.Client{}, &f.File{}, &f.Folder{}, &Statistics{})

	return
}
