package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Stat is one period of a statistic from HA's long-term statistics: when
// it starts and how much the counter added in it (HA's "change", in the
// unit asked for).
type Stat struct {
	Start  time.Time
	Change float64
}

// StatPeriods are the periods HA's recorder sums into.
var StatPeriods = map[string]bool{"hour": true, "day": true, "week": true, "month": true}

// Statistics reads HA's long-term statistics (haus#65): the WebSocket's
// recorder/statistics_during_period, for each id what it added per period
// (hour, day, week, month) between start and end, energy in kWh. HA sums
// days and months in its own time zone. An id HA has no statistics for is
// absent; the periods come oldest first, those without a change left out.
func (c *Client) Statistics(ctx context.Context, ids []string, start, end time.Time, period string) (map[string][]Stat, error) {
	if !StatPeriods[period] {
		return nil, fmt.Errorf("ha: statistics period %q", period)
	}
	for _, id := range ids {
		if !EntityRE.MatchString(id) {
			return nil, fmt.Errorf("ha: statistic id %q", id)
		}
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
	}
	w, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer w.close()
	defer w.closeOn(ctx)()
	id, err := w.send(map[string]any{"type": "recorder/statistics_during_period",
		"start_time": start.UTC().Format(time.RFC3339), "end_time": end.UTC().Format(time.RFC3339),
		"statistic_ids": ids, "period": period, "types": []string{"change"}, "units": map[string]string{"energy": "kWh"}})
	if err != nil {
		return nil, err
	}
	var raw map[string][]struct {
		Start  json.RawMessage `json:"start"`
		Change *float64        `json:"change"`
	}
	if err := w.result(id, &raw); err != nil {
		return nil, err
	}
	out := map[string][]Stat{}
	for sid, rows := range raw {
		var stats []Stat
		for _, r := range rows {
			at, ok := statTime(r.Start)
			if !ok || r.Change == nil {
				continue
			}
			stats = append(stats, Stat{Start: at, Change: *r.Change})
		}
		sort.Slice(stats, func(i, j int) bool { return stats[i].Start.Before(stats[j].Start) })
		if len(stats) > 0 {
			out[sid] = stats
		}
	}
	return out, nil
}

// statTime is a period's start as HA sends it: milliseconds since the
// epoch (since 2023.3), or an ISO time (before).
func statTime(b json.RawMessage) (time.Time, bool) {
	var ms float64
	if json.Unmarshal(b, &ms) == nil {
		return time.UnixMilli(int64(ms)), true
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
