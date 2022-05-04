package events

type (
	Processor struct {
		// Internal channel for events
		ec chan (Event)
		// External channel to recieve 'stop'
		sc chan (bool)
		// IsVerbose indicates whether this instance will process events marked as Verbose
		IsVerbose bool
	}

	Event struct {
		// Level indicates the way to process Event
		Level Level
		// Source in most cases will be printed out in logs at the beginning of each record
		Source string
		// Verbose is useful to avoid unnecessary fuzz in logs (in case sender don't filter events on the source)
		Verbose bool
		// Data should contain payload of error or string
		Data interface{}
	}
)

// SendVerbose sends message marked as non-verbose that will be treated accordingly to IsVerbose property of the Processor
func (p *Processor) Send(l Level, es string, data interface{}) {
	p.ec <- Event{Level: l, Source: es, Verbose: false, Data: data}
}

// SendVerbose sends message marked as verbose that will be treated accordingly to IsVerbose property of the Processor
func (p *Processor) SendVerbose(l Level, es string, data interface{}) {
	p.ec <- Event{Level: l, Source: es, Verbose: true, Data: data}
}

func (p *Processor) Close() {
	p.sc <- true
	<-p.sc
}
