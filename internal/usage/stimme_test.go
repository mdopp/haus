package usage

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
	"github.com/mdopp/haus/internal/usage/usagefake"
)

func voice(t *testing.T) (*hafake.HA, *usagefake.Fake, *Counter, *Voice, *[]string) {
	t.Helper()
	fakeHA := hafake.New("ha-token")
	t.Cleanup(fakeHA.Close)
	fake, c, logged := counter(t)
	v := &Voice{HA: &ha.Client{URL: fakeHA.URL, Token: "ha-token"}, Counter: c, Since: day1.Add(-time.Hour), Log: c.Log}
	return fakeHA, fake, c, v, logged
}

func stimme(fake *usagefake.Fake) map[string]int64 {
	out := map[string]int64{}
	for k, n := range fake.Counts() {
		if k.App == "stimme" {
			out[k.Event] += n
		}
	}
	return out
}

// #50: each Assist run HA keeps is counted once — recognized by HA's own
// intents, handed to the fallback agent, or not recognized — and the
// sentence HA did not recognize goes to usage-metrics alone.
func TestVoiceCountsEachRunOnce(t *testing.T) {
	fakeHA, fake, c, v, _ := voice(t)
	at := day1.Add(-10 * time.Minute)
	fakeHA.SetAssist([]hafake.Pipeline{
		{ID: "01JPIPEKUECHE", Runs: []hafake.AssistRun{
			{ID: "r1", Time: at, Sentence: "Mach das Licht in der Küche an", Local: true},
			{ID: "r2", Time: at, Sentence: "Wann kommt der Müll", Local: true, Code: "no_intent_match"},
			{ID: "r3", Time: at, Sentence: "Erzähl mir einen Witz", Local: false},
		}},
		{ID: "01JPIPEBUERO", Runs: []hafake.AssistRun{
			{ID: "r4", Time: at, Sentence: "Schalte das Ding aus", Local: true, Code: "no_valid_targets"},
		}},
	})
	v.Poll(context.Background())
	v.Poll(context.Background()) // the same runs again: nothing new
	c.Flush(context.Background())
	if got, want := stimme(fake), map[string]int64{"erkannt": 1, "weitergereicht": 1, "nicht-erkannt": 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("voice counts %v, want %v", got, want)
	}
	want := []usagefake.Miss{{Source: "stimme", Text: "Wann kommt der Müll", Day: "2026-10-16"}, {Source: "stimme", Text: "Schalte das Ding aus", Day: "2026-10-16"}}
	if got := fake.Misses(); !reflect.DeepEqual(got, want) {
		t.Errorf("misses %v, want %v", got, want)
	}
	for _, b := range fake.Bodies() {
		for _, leak := range []string{"dev-kueche-satellit", "assist_satellite", "01JCONV", "01JPIPE", "r2"} {
			if strings.Contains(b, leak) {
				t.Errorf("%q left Haus: %s", leak, b)
			}
		}
	}
}

// A run from before Haus started is not counted (a restart does not count
// the ten HA keeps again); a run still going is counted once it ends.
func TestVoiceSinceAndOpenRuns(t *testing.T) {
	fakeHA, fake, c, v, _ := voice(t)
	p := hafake.Pipeline{ID: "p", Runs: []hafake.AssistRun{
		{ID: "old", Time: v.Since.Add(-time.Minute), Sentence: "Licht an", Local: true, Code: "no_intent_match"},
		{ID: "open", Time: v.Since.Add(time.Minute), Sentence: "Wie spät ist es", Open: true},
	}}
	fakeHA.SetAssist([]hafake.Pipeline{p})
	v.Poll(context.Background())
	c.Flush(context.Background())
	if got := stimme(fake); len(got) != 0 {
		t.Fatalf("counted %v: an old run, or one that has not ended", got)
	}
	p.Runs[1].Open, p.Runs[1].Local = false, true
	fakeHA.SetAssist([]hafake.Pipeline{p})
	v.Poll(context.Background())
	c.Flush(context.Background())
	if got := stimme(fake); !reflect.DeepEqual(got, map[string]int64{"erkannt": 1}) {
		t.Errorf("after the run ended: %v, want erkannt 1", got)
	}
}

// HA refuses the debug store to a token whose user is no administrator:
// no voice counts, one log line, and the card can say why.
func TestVoiceNotAdmin(t *testing.T) {
	fakeHA, fake, c, v, logged := voice(t)
	fakeHA.SetAssist([]hafake.Pipeline{{ID: "p", Runs: []hafake.AssistRun{{ID: "r", Time: day1, Sentence: "Licht an", Local: true}}}})
	fakeHA.AssistAdmin(false)
	v.Poll(context.Background())
	v.Poll(context.Background())
	c.Flush(context.Background())
	if !v.Refused() {
		t.Error("the refusal is not reported")
	}
	if len(stimme(fake)) != 0 {
		t.Errorf("counted %v without the debug store", stimme(fake))
	}
	if len(*logged) != 1 || !strings.Contains((*logged)[0], "Administrator") {
		t.Errorf("logged %q, want one line naming the administrator right", *logged)
	}
	fakeHA.AssistAdmin(true)
	v.Poll(context.Background())
	if v.Refused() {
		t.Error("still refused after HA lets it read")
	}
}

// HA not answering is no refusal and no count.
func TestVoiceHADown(t *testing.T) {
	fakeHA, fake, c, v, _ := voice(t)
	fakeHA.Close()
	v.Poll(context.Background())
	c.Flush(context.Background())
	if v.Refused() || len(stimme(fake)) != 0 {
		t.Errorf("HA down: refused %v, counts %v", v.Refused(), stimme(fake))
	}
	var nilVoice *Voice
	nilVoice.Poll(context.Background())
	if nilVoice.Refused() {
		t.Error("no voice is refused")
	}
}
