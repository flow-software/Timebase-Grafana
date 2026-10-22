package plugin

import (
	"context"
	"net/url"
	"sort"
	"sync"
	"time"
)

// objectCacheTTL is how long the editor's object list is reused. Listing a
// large historian reads every dataset's tags, and each query editor on a
// dashboard asks for the list when it opens; a minute keeps that to one read
// while new tags still show up soon.
const objectCacheTTL = time.Minute

// objectCache holds the last object list, sorted by ElementId. Concurrent
// callers wait for one read rather than each starting their own. Failures
// are not cached. Grafana builds a new Datasource when settings change, so
// the cache never outlives the settings it was read with.
type objectCache struct {
	mu      sync.Mutex
	objects []objectOption
	readAt  time.Time
	now     func() time.Time // for tests; nil means time.Now
}

// get returns the cached list, reading it with load when it is missing or
// older than objectCacheTTL, or when the request URL has refresh=1.
func (c *objectCache) get(ctx context.Context, requestURL string, load func(context.Context) ([]objectOption, error)) ([]objectOption, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now
	if c.now != nil {
		now = c.now
	}
	if c.objects != nil && now().Sub(c.readAt) < objectCacheTTL && !wantsRefresh(requestURL) {
		return c.objects, nil
	}

	objects, err := load(ctx)
	if err != nil {
		return nil, err
	}
	if objects == nil {
		objects = []objectOption{}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].ElementID < objects[j].ElementID })
	c.objects, c.readAt = objects, now()
	return objects, nil
}

func wantsRefresh(requestURL string) bool {
	u, err := url.Parse(requestURL)
	return err == nil && u.Query().Get("refresh") == "1"
}
