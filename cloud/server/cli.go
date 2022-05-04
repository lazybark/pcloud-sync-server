package server

import (
	"fmt"

	"github.com/lazybark/go-pretty-code/console"
)

func (s *Server) PrintHelp() string {

	help := "h (help) for help"
	ver := "v (ver, version) for app version data"

	return fmt.Sprintf("help:\n -%s\n -%s\n", help, ver)
}

func (s *Server) PrintVersion() string {

	modules := "Modules:"

	return fmt.Sprintf("LazyCloud %s v%s (%s)\n%s", s.appLevel, s.appVersion, s.appVersionLabel, modules)
}

func (s *Server) PrintStatus() string {

	return fmt.Sprintf("Server status:\n -filesysem watcher: %v\n -some: %v", ReturnOnline(s.fswOnline), ReturnOnline(false))
}

func (s *Server) PrintStatistics() string {

	modules := "Modules:"

	return fmt.Sprintf("LazyCloud %s v%s (%s)\n%s", s.appLevel, s.appVersion, s.appVersionLabel, modules)
}

func ReturnOnline(b bool) string {
	if b {
		return console.ForeGreen("online")
	} else {
		return console.ForeRed("offline")
	}
}
