// Package sse reads Server-Sent Events streams, the format both the i3X and
// the native Timebase streaming endpoints use.
package sse

import (
	"bufio"
	"io"
	"strings"
)

// Event is one dispatched event. Event is empty when the server named none.
type Event struct {
	Event string
	Data  string
}

// maxLine bounds one line. Text tags can carry values of hundreds of kB
// (the OPC PLC simulator has a 100 kB string tag), so the default 64 kB
// scanner limit is too small.
const maxLine = 16 << 20

// Read calls fn for each event until the stream ends, fn returns an error,
// or reading fails. A clean end of stream returns io.EOF so callers can tell
// a server that hung up from one that never answered.
func Read(r io.Reader, fn func(Event) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxLine)

	var ev Event
	var data []string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				ev.Data = strings.Join(data, "\n")
				if err := fn(ev); err != nil {
					return err
				}
			}
			ev, data = Event{}, nil
		case strings.HasPrefix(line, ":"):
			// Comment, e.g. i3X's ": stream open" keep-alive.
		default:
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				ev.Event = value
			case "data":
				data = append(data, value)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}
