package plugin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flow-software/Timebase-Grafana/Historian/pkg/i3x"
	"github.com/flow-software/Timebase-Grafana/Historian/pkg/series"
	"github.com/flow-software/Timebase-Grafana/Historian/pkg/timebase"
)

// historian is what the datasource needs from an API. Both APIs identify a
// tag as "<dataset>:<tag name>", so queries, dashboards and alert rules keep
// working when a datasource is switched between them.
type historian interface {
	// Check proves the URL and credentials work and describes the server.
	Check(ctx context.Context) (string, error)
	// Objects lists datasets, folders and tags for the query editor.
	Objects(ctx context.Context) ([]objectOption, error)
	// History reads the given ElementIds, one result per id, in order.
	History(ctx context.Context, elementIDs []string, from, to time.Time) ([]seriesResult, error)
	// Live calls fn with each new sample of one tag until ctx ends or the
	// connection drops; it never returns nil.
	Live(ctx context.Context, elementID string, fn func(series.Sample)) error
}

type objectOption struct {
	ElementID   string `json:"elementId"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
	ParentID    string `json:"parentId,omitempty"`
	// IsFolder marks compositions (datasets, folders): they structure the
	// tree but have no values of their own, so they cannot be queried.
	IsFolder bool `json:"isFolder"`
	// IsSystem marks the historian's own health tags. Only the native API
	// reports this; for i3X the editor recognises them by name.
	IsSystem    bool   `json:"isSystem,omitempty"`
	Description string `json:"description,omitempty"`
	Unit        string `json:"unit,omitempty"`
}

type seriesResult struct {
	ElementID string
	Samples   []series.Sample
	Meta      series.Meta
	// Err is a per-tag failure; the other tags in the query still load.
	Err string
}

// i3xHistorian reads through the i3X API.
type i3xHistorian struct{ c *i3x.Client }

func (h i3xHistorian) Check(ctx context.Context) (string, error) {
	info, err := h.c.Info(ctx)
	if err != nil {
		return "", err
	}
	if !info.Capabilities.Query.History {
		return "", fmt.Errorf("connected to %s %s, but it does not support history queries", info.ServerName, info.ServerVersion)
	}
	return fmt.Sprintf("Connected to %s %s (i3X %s)", info.ServerName, info.ServerVersion, info.SpecVersion), nil
}

func (h i3xHistorian) Objects(ctx context.Context) ([]objectOption, error) {
	objects, err := h.c.Objects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]objectOption, 0, len(objects))
	for _, o := range objects {
		out = append(out, objectOption{
			ElementID:   o.ElementID,
			DisplayName: series.DisplayName(o.ElementID),
			Type:        o.TypeElementID,
			ParentID:    o.ParentID,
			IsFolder:    o.IsComposition,
		})
	}
	return out, nil
}

func (h i3xHistorian) History(ctx context.Context, ids []string, from, to time.Time) ([]seriesResult, error) {
	items, err := h.c.History(ctx, ids, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]seriesResult, 0, len(items))
	for _, item := range items {
		r := seriesResult{ElementID: item.ElementID}
		switch {
		case !item.Success:
			r.Err = item.ResponseDetail.String()
		case item.Result.IsComposition:
			r.Err = "is a folder or dataset, not a tag; pick the tags inside it"
		default:
			r.Samples = item.Result.Values
		}
		out = append(out, r)
	}
	return out, nil
}

func (h i3xHistorian) Live(ctx context.Context, elementID string, fn func(series.Sample)) error {
	return h.c.Stream(ctx, elementID, fn)
}

// nativeHistorian reads through the native Timebase API.
type nativeHistorian struct{ c *timebase.Client }

func (h nativeHistorian) Live(ctx context.Context, elementID string, fn func(series.Sample)) error {
	dataset, tag, ok := strings.Cut(elementID, ":")
	if !ok || dataset == "" || tag == "" {
		return fmt.Errorf("%s is not in dataset:tag form", elementID)
	}
	return h.c.Stream(ctx, dataset, tag, func(v timebase.TagValue) {
		fn(series.Sample{Value: v.V, Quality: series.QualityFromOPC(v.Q), Timestamp: v.T})
	})
}

// Bounds the parallel requests made for one editor load or one query.
const nativeParallelism = 4

func (h nativeHistorian) Check(ctx context.Context) (string, error) {
	datasets, err := h.c.Datasets(ctx)
	if err != nil {
		return "", err
	}
	names := make([]string, len(datasets))
	for i, d := range datasets {
		names[i] = d.Name
	}
	sort.Strings(names)
	return fmt.Sprintf("Connected to the Timebase Historian (native API); %d datasets: %s", len(names), strings.Join(names, ", ")), nil
}

func (h nativeHistorian) Objects(ctx context.Context) ([]objectOption, error) {
	datasets, err := h.c.Datasets(ctx)
	if err != nil {
		return nil, err
	}

	lists := make([]*timebase.TagList, len(datasets))
	errs := make([]error, len(datasets))
	parallel(len(datasets), func(i int) {
		lists[i], errs[i] = h.c.Tags(ctx, datasets[i].Name)
	})

	var out []objectOption
	for i, d := range datasets {
		if errs[i] != nil {
			return nil, fmt.Errorf("dataset %s: %w", d.Name, errs[i])
		}
		out = append(out, objectOption{ElementID: d.Name, DisplayName: d.Name, Type: "Dataset", IsFolder: true})
		add := func(tags []timebase.Tag, system bool) {
			for _, t := range tags {
				unit, _ := unitsMeta(t.Units)
				out = append(out, objectOption{
					ElementID:   d.Name + ":" + t.Name,
					DisplayName: t.Name,
					Type:        "Tag",
					ParentID:    d.Name,
					IsSystem:    system,
					Description: t.Description,
					Unit:        unit,
				})
			}
		}
		add(lists[i].User, false)
		add(lists[i].System, true)
	}
	return out, nil
}

func (h nativeHistorian) History(ctx context.Context, ids []string, from, to time.Time) ([]seriesResult, error) {
	out := make([]seriesResult, len(ids))
	byDataset := map[string][]int{} // dataset -> positions in ids
	var order []string
	for i, id := range ids {
		out[i].ElementID = id
		dataset, tag, ok := strings.Cut(id, ":")
		if !ok || dataset == "" || tag == "" {
			out[i].Err = "is not in dataset:tag form"
			continue
		}
		if _, seen := byDataset[dataset]; !seen {
			order = append(order, dataset)
		}
		byDataset[dataset] = append(byDataset[dataset], i)
	}

	for _, dataset := range order {
		positions := byDataset[dataset]
		tags := make([]string, len(positions))
		for j, p := range positions {
			_, tags[j], _ = strings.Cut(ids[p], ":")
		}

		data, err := h.c.Data(ctx, dataset, tags, from, to)
		var apiErr *timebase.APIError
		switch {
		case err == nil:
			fill(out, positions, tags, data)
		case errors.As(err, &apiErr) && apiErr.Key == timebase.ErrKeyTagNotFound && len(tags) > 1:
			// One unknown tag fails the whole read, so read each tag on its
			// own; the good ones still load and the bad one gets its error.
			parallel(len(tags), func(j int) {
				one, err := h.c.Data(ctx, dataset, tags[j:j+1], from, to)
				if err != nil {
					out[positions[j]].Err = errorText(err)
					return
				}
				fill(out, positions[j:j+1], tags[j:j+1], one)
			})
		case errors.As(err, &apiErr) && (apiErr.Key == timebase.ErrKeyTagNotFound || apiErr.Key == timebase.ErrKeyDatasetNotFound):
			for _, p := range positions {
				out[p].Err = apiErr.Message
			}
		default:
			// Connection, auth or server failures affect every tag alike.
			return nil, err
		}
	}
	return out, nil
}

// fill copies each returned tag's samples to its result slot.
func fill(out []seriesResult, positions []int, tags []string, data []timebase.TagData) {
	byName := make(map[string]timebase.TagData, len(data))
	for _, d := range data {
		byName[d.Tag.Name] = d
	}
	for j, p := range positions {
		d, ok := byName[tags[j]]
		if !ok {
			out[p].Err = "the historian returned no data for this tag"
			continue
		}
		out[p].Samples = nativeSamples(d.Data)
		out[p].Meta = nativeMeta(d.Tag)
	}
}

func nativeSamples(values []timebase.TagValue) []series.Sample {
	out := make([]series.Sample, 0, len(values))
	for _, v := range values {
		// A range with no data comes back as one placeholder sample dated
		// 0001-01-01 with no value; it is not a real point.
		if v.T.Year() <= 1 {
			continue
		}
		out = append(out, series.Sample{Value: v.V, Quality: series.QualityFromOPC(v.Q), Timestamp: v.T})
	}
	return out
}

func nativeMeta(t timebase.Tag) series.Meta {
	unit, valueText := unitsMeta(t.Units)
	return series.Meta{Unit: unit, Description: t.Description, ValueText: valueText, Text: t.IsText()}
}

// unitsMeta reads the historian's units map: one entry is an engineering
// unit ({"1":"°F"}), several entries label discrete values
// ({"0":"Off","1":"On"}).
func unitsMeta(units map[string]string) (unit string, valueText map[string]string) {
	switch len(units) {
	case 0:
		return "", nil
	case 1:
		for _, u := range units {
			return u, nil
		}
	}
	return "", units
}

func errorText(err error) string {
	var apiErr *timebase.APIError
	if errors.As(err, &apiErr) && apiErr.Message != "" {
		return apiErr.Message
	}
	return err.Error()
}

// parallel runs fn(0..n-1) with at most nativeParallelism at a time.
func parallel(n int, fn func(i int)) {
	sem := make(chan struct{}, nativeParallelism)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
