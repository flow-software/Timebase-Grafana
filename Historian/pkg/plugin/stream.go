package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flow-software/Timebase-Grafana/Historian/pkg/series"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

var _ backend.StreamHandler = (*Datasource)(nil)

// Live channels carry one tag each: "tag/<flags>/<id>", where id is the
// ElementId in unpadded base64url (channel paths only allow [A-Za-z0-9_-/=.],
// and tag names contain spaces, '#' and ':') and flags is "n" when samples
// whose quality is not Good are blanked, else "-". The frontend builds the
// same path in src/live.ts.
const livePathPrefix = "tag/"

type liveChannel struct {
	ElementID        string
	BadQualityAsNull bool
}

func parseLivePath(path string) (liveChannel, error) {
	rest, ok := strings.CutPrefix(path, livePathPrefix)
	if !ok {
		return liveChannel{}, fmt.Errorf("unknown channel %q", path)
	}
	flags, encoded, ok := strings.Cut(rest, "/")
	if !ok || (flags != "n" && flags != "-") {
		return liveChannel{}, fmt.Errorf("malformed channel %q", path)
	}
	id, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(id) == 0 {
		return liveChannel{}, fmt.Errorf("malformed channel %q", path)
	}
	return liveChannel{ElementID: string(id), BadQualityAsNull: flags == "n"}, nil
}

func (ch liveChannel) options() series.Options {
	return series.Options{BadQualityAsNull: ch.BadQualityAsNull}
}

// subscribeData is sent by the frontend with each subscription: the panel's
// time range in epoch milliseconds, used to backfill history.
type subscribeData struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

// Default backfill when the frontend sends no range.
const defaultBackfill = 5 * time.Minute

// SubscribeStream runs for every panel that subscribes. It answers with the
// tag's history for the panel's time range, so the panel starts full and
// live samples are appended to it.
func (d *Datasource) SubscribeStream(ctx context.Context, req *backend.SubscribeStreamRequest) (*backend.SubscribeStreamResponse, error) {
	notFound := &backend.SubscribeStreamResponse{Status: backend.SubscribeStreamStatusNotFound}
	if d.configErr != nil {
		return notFound, nil
	}
	ch, err := parseLivePath(req.Path)
	if err != nil {
		return notFound, nil
	}

	to := time.Now()
	from := to.Add(-defaultBackfill)
	var sd subscribeData
	if len(req.Data) > 0 && json.Unmarshal(req.Data, &sd) == nil && sd.From > 0 && sd.From < to.UnixMilli() {
		from = time.UnixMilli(sd.From)
	}

	frame, _, err := d.liveSnapshot(ctx, ch, from, to)
	if err != nil {
		log.DefaultLogger.Warn("live subscribe failed", "elementId", ch.ElementID, "error", err)
		return notFound, nil
	}
	initial, err := backend.NewInitialFrame(frame, data.IncludeAll)
	if err != nil {
		return nil, err
	}
	return &backend.SubscribeStreamResponse{Status: backend.SubscribeStreamStatusOK, InitialData: initial}, nil
}

// liveSnapshot reads one tag's history as a live channel's starting frame.
// The returned meta is fixed for the channel: every later frame must keep
// the same field types, so a tag that has held text stays a text series.
func (d *Datasource) liveSnapshot(ctx context.Context, ch liveChannel, from, to time.Time) (*data.Frame, series.Meta, error) {
	results, err := d.historian.History(ctx, []string{ch.ElementID}, from, to)
	if err != nil {
		return nil, series.Meta{}, err
	}
	if len(results) != 1 {
		return nil, series.Meta{}, fmt.Errorf("expected one result, got %d", len(results))
	}
	r := results[0]
	if r.Err != "" {
		return nil, series.Meta{}, fmt.Errorf("%s", r.Err)
	}
	meta := r.Meta
	meta.Text = meta.Text || series.HasText(r.Samples)
	return series.Frame(ch.ElementID, r.Samples, meta, ch.options()), meta, nil
}

// Reconnect delays after a dropped stream.
const (
	minRetry = time.Second
	maxRetry = 30 * time.Second
)

// RunStream runs once per channel while at least one panel is subscribed;
// Grafana cancels ctx when the last one leaves. It keeps a stream to the
// historian open and forwards each sample, reconnecting with backoff if the
// connection drops.
func (d *Datasource) RunStream(ctx context.Context, req *backend.RunStreamRequest, sender *backend.StreamSender) error {
	if d.configErr != nil {
		return d.configErr
	}
	ch, err := parseLivePath(req.Path)
	if err != nil {
		return err
	}

	// Fix the field type the same way SubscribeStream did. A short window is
	// enough; if it fails, the tag is treated as numeric.
	now := time.Now()
	_, meta, err := d.liveSnapshot(ctx, ch, now.Add(-defaultBackfill), now)
	if err != nil {
		log.DefaultLogger.Warn("live stream: could not read recent history", "elementId", ch.ElementID, "error", err)
	}

	retry := minRetry
	for {
		err := d.historian.Live(ctx, ch.ElementID, func(s series.Sample) {
			retry = minRetry
			frame := series.Frame(ch.ElementID, []series.Sample{s}, meta, ch.options())
			if err := sender.SendFrame(frame, data.IncludeDataOnly); err != nil {
				log.DefaultLogger.Warn("live stream: send failed", "elementId", ch.ElementID, "error", err)
			}
		})
		if ctx.Err() != nil {
			return nil
		}
		log.DefaultLogger.Warn("live stream dropped; reconnecting", "elementId", ch.ElementID, "error", err, "retryIn", retry)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(retry):
		}
		retry = min(retry*2, maxRetry)
	}
}

// PublishStream refuses publishing: channels are read-only.
func (d *Datasource) PublishStream(context.Context, *backend.PublishStreamRequest) (*backend.PublishStreamResponse, error) {
	return &backend.PublishStreamResponse{Status: backend.PublishStreamStatusPermissionDenied}, nil
}
