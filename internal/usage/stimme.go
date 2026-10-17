package usage

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/ha"
)

// Assist is where the voice control's runs come from: Home Assistant.
type Assist interface {
	AssistRuns(ctx context.Context, since time.Time, known func(id string) bool) ([]ha.AssistRun, error)
}

// VoiceApp is the voice control's name in usage-metrics.
const VoiceApp = "stimme"

// Voice counts Home Assistant Assist's runs (haus#50): erkannt — HA's own
// intents answered; weitergereicht — the fallback conversation agent did;
// nicht-erkannt — the answer was an error, and its sentence is a miss. HA
// keeps the last ten runs of each pipeline in memory, so Poll runs often
// (every minute); each run counts once, and one from before Since (a
// restart's ten) not at all.
type Voice struct {
	HA      Assist
	Counter *Counter
	Since   time.Time
	Log     func(format string, args ...any)

	mu      sync.Mutex
	seen    map[string]time.Time
	refused bool
}

// Poll counts the runs that ended since the last poll.
func (v *Voice) Poll(ctx context.Context) {
	if v == nil || v.HA == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.seen == nil {
		v.seen = map[string]time.Time{}
	}
	runs, err := v.HA.AssistRuns(ctx, v.Since, func(id string) bool { _, ok := v.seen[id]; return ok })
	if errors.Is(err, ha.ErrNotAdmin) {
		if !v.refused && v.Log != nil {
			v.Log("haus: voice: Home Assistant refuses the Assist runs: the HA token's user must be an Administrator to count the voice control")
		}
		v.refused = true
		return
	}
	if err != nil {
		return // HA not answering: the runs wait for the next poll, as long as HA keeps them
	}
	v.refused = false
	for _, r := range runs {
		if !r.Done {
			continue // still running: next time
		}
		v.seen[r.ID] = r.Time
		switch {
		case !r.Intent:
		case r.Failed:
			v.Counter.CountIn(VoiceApp, "nicht-erkannt")
			v.Counter.Miss(VoiceApp, r.Sentence)
		case r.Local:
			v.Counter.CountIn(VoiceApp, "erkannt")
		default:
			v.Counter.CountIn(VoiceApp, "weitergereicht")
		}
	}
	for id, at := range v.seen { // a run HA no longer keeps cannot come back
		if time.Since(at) > 24*time.Hour {
			delete(v.seen, id)
		}
	}
}

// Refused says HA refused the runs at the last poll: the token's user is
// no administrator, so the voice is not counted.
func (v *Voice) Refused() bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.refused
}

// Run polls every interval until ctx ends.
func (v *Voice) Run(ctx context.Context, every time.Duration) {
	if v == nil {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, every/2)
			v.Poll(pctx)
			cancel()
		}
	}
}
