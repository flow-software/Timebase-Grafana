package plugin

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestObjectCacheReusesTheListUntilItExpires(t *testing.T) {
	clock := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	c := objectCache{now: func() time.Time { return clock }}
	loads := 0
	load := func(context.Context) ([]objectOption, error) {
		loads++
		return []objectOption{{ElementID: "b"}, {ElementID: "a"}}, nil
	}

	got, _ := c.get(context.Background(), "objects", load)
	if loads != 1 || got[0].ElementID != "a" {
		t.Fatalf("first read: %d loads, %v", loads, got)
	}
	clock = clock.Add(objectCacheTTL - time.Second)
	_, _ = c.get(context.Background(), "objects", load)
	if loads != 1 {
		t.Fatalf("read again within the TTL: %d loads", loads)
	}
	_, _ = c.get(context.Background(), "objects?refresh=1", load)
	if loads != 2 {
		t.Fatalf("refresh=1 should reload: %d loads", loads)
	}
	clock = clock.Add(objectCacheTTL)
	_, _ = c.get(context.Background(), "objects", load)
	if loads != 3 {
		t.Fatalf("expired list should reload: %d loads", loads)
	}
}

func TestObjectCacheDoesNotKeepFailures(t *testing.T) {
	var c objectCache
	fail := true
	load := func(context.Context) ([]objectOption, error) {
		if fail {
			return nil, errors.New("historian down")
		}
		return []objectOption{{ElementID: "a"}}, nil
	}
	if _, err := c.get(context.Background(), "objects", load); err == nil {
		t.Fatal("want the error")
	}
	fail = false
	if got, err := c.get(context.Background(), "objects", load); err != nil || len(got) != 1 {
		t.Fatalf("after recovery: %v %v", got, err)
	}
}
