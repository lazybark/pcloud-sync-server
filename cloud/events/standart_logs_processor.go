package events

import (
	"fmt"
	"log"

	"github.com/lazybark/go-pretty-code/logs"
	"go.uber.org/zap"
)

// NewProcessor creates new standart processor for making logs only
func NewProcessor(logfile string, isVerbose bool) (p *Processor) {
	p = new(Processor)
	p.ec = make(chan Event)
	p.sc = make(chan bool)
	p.IsVerbose = isVerbose

	p.StandartLogs(logfile)

	return
}

// Standart events processor for making logs only
//
// You can add own processors here or below if needed
func (p *Processor) StandartLogs(logfile string) {
	go p.StandartLogsProcessor(logfile)
}

// EventProcessor is a routine that aggregates all events and takes actions if needed
func (p *Processor) StandartLogsProcessor(logpath string) {

	// Connect Logger
	logger, err := logs.Double(logpath, false, zap.InfoLevel)
	if err != nil {
		log.Fatal("[EventProcessor] error -> unable to make logger: ", err)
		return
	}
	defer logger.Info("[EventProcessor] warning -> events channel closed")
	defer func() { p.sc <- true }()

	for {
		select {
		case event, ok := <-p.ec:
			if !ok {
				return
			}

			if !event.Level.CheckEventLevel() {
				logger.Error("[EventProcessor] error -> illegal event level: ", event.Level.String())
				continue
			}

			// Act according to logging config (mainly for base server needs)
			if !p.IsVerbose && event.Verbose {
				continue
			}

			err, isErr := event.Data.(error)
			if isErr {
				if event.Level < Warn {
					logger.Error("[EventProcessor] error -> event type & level mismatch: wanted Warn or above, got less")
					continue
				} else if event.Level == Warn {
					logger.Warn(fmt.Sprintf("[%s] ", event.Source), err)
				} else if event.Level == Error {
					logger.Error(fmt.Sprintf("[%s] ", event.Source), err)
				} else if event.Level == Fatal {
					logger.Fatal(fmt.Sprintf("[%s] ", event.Source), err)
				} else {
					logger.Error(fmt.Sprintf("((%s level event))[%s] ", event.Level.String(), event.Source), err)
				}
			}

			text, isText := event.Data.(string)
			if isText {
				if event.Level == Info {
					logger.Info(fmt.Sprintf("[%s] ", event.Source), text)
				} else if event.Level == InfoCyan {
					logger.InfoCyan(fmt.Sprintf("[%s] ", event.Source), text)
				} else if event.Level == InfoBackRed {
					logger.InfoBackRed(fmt.Sprintf("[%s] ", event.Source), text)
				} else if event.Level == InfoRed {
					logger.InfoRed(fmt.Sprintf("[%s] ", event.Source), text)
				} else if event.Level == InfoYellow {
					logger.InfoYellow(fmt.Sprintf("[%s] ", event.Source), text)
				} else if event.Level == InfoGreen {
					logger.InfoGreen(fmt.Sprintf("[%s] ", event.Source), text)
				} else if event.Level == InfoMagenta {
					logger.InfoMagenta(fmt.Sprintf("[%s] ", event.Source), text)
				} else if event.Level == Warn {
					logger.Warn(fmt.Sprintf("[%s] ", event.Source), text)
				} else if event.Level == Error {
					logger.Error(fmt.Sprintf("[%s] ", event.Source), text)
				} else if event.Level == Fatal {
					logger.Fatal(fmt.Sprintf("[%s] ", event.Source), text)
				} else {
					logger.Info(fmt.Sprintf("((%s level event))[%s] ", event.Level.String(), event.Source), text)
				}
			}

			if !isErr && !isText {
				logger.Error("[EventProcessor] error -> wrong event payload type: 'error' or 'string' only")
				continue
			}

		case stop, ok := <-p.sc:
			if !ok || stop {
				return
			}
		}

	}

}
