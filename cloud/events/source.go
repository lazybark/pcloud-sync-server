package events

type (
	EventsSource int
)

const (
	event_source_start EventsSource = iota

	SourceSyncServer
	SourceSyncServerListener
	SourceSyncServerStats
	SourceSyncClient
	SourceFileSystemWatcher
	SourceFileSystemProcessor
	SourceAdminRoutine
	SourceFsEvents

	event_source_end
)

func (s EventsSource) String() string {
	if !s.CheckEventSource() {
		return "Unknown"
	}
	return [...]string{"Unknown", "Sync Server", "Sync Server listener", "Server stats", "Sync Client", "Filesystem Watcher", "Filesystem Processor", "Server admin routine", "FS events", "Unknown"}[s]
}

func (s *EventsSource) CheckEventSource() bool {
	if event_source_start < *s && *s < event_source_end {
		return true
	}
	return false
}
