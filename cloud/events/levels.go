package events

type (
	Level int
)

// Predefined levels, most of them indicate simple console colors for pretty output
//
// Also, levels will be used as log methods of go.uber.org/zap
//
// And processed by logger using exactly Info, Warn, Error of Fatal methos of Zap
//
// All colored levels work accordingly to github.com/lazybark/go-pretty-code/logs
//
// And are simply colored versions of Info
const (
	event_level_start Level = iota

	UnknownLevel

	Info
	InfoCyan
	InfoGreen
	InfoMagenta
	InfoYellow
	InfoRed
	InfoBackRed

	Warn
	Error
	Fatal

	event_level_end
)

func (l Level) String() string {
	if !l.CheckEventLevel() {
		return "Illegal"
	}
	return [...]string{"Illegal", "Unknown", "Information", "Information", "Information", "Information", "Information", "Information", "Warning", "Error", "Fatal", "Illegal"}[l]
}

func (l Level) CheckEventLevel() bool {
	if event_level_start < l && l < event_level_end {
		return true
	}
	return false
}
