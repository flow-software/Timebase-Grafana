package sse

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadBothServerStyles(t *testing.T) {
	// i3X: a comment, then unnamed events. Native: named "data" events.
	stream := ": stream open\n\n" +
		"data: [{\"elementId\":\"a\"}]\n\n" +
		"event: data\ndata: {\"n\":\"b\"}\n\n" +
		"data: line one\ndata: line two\n\n" +
		"data: no trailing blank line"

	var got []Event
	err := Read(strings.NewReader(stream), func(e Event) error {
		got = append(got, e)
		return nil
	})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want io.EOF at end of stream", err)
	}
	want := []Event{
		{Data: `[{"elementId":"a"}]`},
		{Event: "data", Data: `{"n":"b"}`},
		{Data: "line one\nline two"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReadStopsWhenHandlerFails(t *testing.T) {
	stop := errors.New("stop")
	n := 0
	err := Read(strings.NewReader("data: 1\n\ndata: 2\n\n"), func(Event) error {
		n++
		return stop
	})
	if !errors.Is(err, stop) || n != 1 {
		t.Fatalf("got %v after %d events", err, n)
	}
}

func TestReadAcceptsLongLines(t *testing.T) {
	big := strings.Repeat("x", 300<<10)
	var got string
	_ = Read(strings.NewReader("data: "+big+"\n\n"), func(e Event) error {
		got = e.Data
		return nil
	})
	if len(got) != len(big) {
		t.Fatalf("got %d bytes, want %d", len(got), len(big))
	}
}
