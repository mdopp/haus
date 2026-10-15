package library

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/judge"
	"github.com/mdopp/haus/internal/judge/proxyfake"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/qmd"
	"github.com/mdopp/haus/internal/qmd/qmdfake"
)

const house = "gemma-4-e4b"

// tidyShelf is the shelf, its qmd, and Laib's model proxy with the house
// model loaded: "Schornsteinfeger" finds the Kaminkehrer and the Gastherme,
// both over the cut.
type tidied struct {
	dir string
	d   *notes.Dir
	f   *qmdfake.Server
	c   *qmd.Client
	p   *proxyfake.Server
	j   *judge.Client
}

func tidyShelf(t *testing.T) tidied {
	t.Helper()
	dir, d, f := shelf(t)
	p := proxyfake.New(house)
	t.Cleanup(p.Close)
	return tidied{dir, d, f, &qmd.Client{URL: f.URL, Collection: "notizen"}, p, &judge.Client{URL: p.Base(), Model: house, Key: "model-key"}}
}

// neverTakesTheLease is the tidy-up's promise to the card (laib ADR
// 0011): Haus reads the lease and asks the loaded model, nothing else —
// no POST or DELETE of the lease, no request that would load a model, and
// every chat for the house model by name.
func neverTakesTheLease(t *testing.T, p *proxyfake.Server) {
	t.Helper()
	for _, r := range p.Seen() {
		switch {
		case r.Method == http.MethodGet && r.Path == "/api/model-lease":
			if r.Auth != "" {
				t.Errorf("the lease is read without the key: %q", r.Auth)
			}
		case r.Method == http.MethodPost && r.Path == "/v1/chat/completions":
			var body struct {
				Model string `json:"model"`
			}
			json.Unmarshal(r.Body, &body)
			if body.Model != house {
				t.Errorf("asked for %q, which could swap the card; only %q", body.Model, house)
			}
		default:
			t.Errorf("%s %s: Haus never takes the lease or loads a model", r.Method, r.Path)
		}
	}
}

func (s tidied) find(t *testing.T, j *judge.Client, wait time.Duration, q string) Found {
	t.Helper()
	got, err := Find(bg, s.c, Tidy{Judge: j, Wait: wait}, s.d, q, 10)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The house model narrows the hits to those that answer, in qmd's order;
// it reads each note's longer passage, from the note itself, and the
// question.
func TestTidyNarrows(t *testing.T) {
	s := tidyShelf(t)
	p := s.p
	p.Content = `{"ids":[2]}`
	got := s.find(t, s.j, time.Second, "Schornsteinfeger")
	if got.Reduced || names(got.Hits) != "heizung.md" || got.Total != 1 {
		t.Fatalf("narrowed: %+v", got)
	}
	p.Content = `{"ids":[2,1]}`
	if got := s.find(t, s.j, time.Second, "Schornsteinfeger"); names(got.Hits) != "kaminkehrer.md,heizung.md" {
		t.Fatalf("qmd's order: %s", names(got.Hits))
	}
	chats := p.Chats()
	if len(chats) != 2 {
		t.Fatalf("%d chats, want one per search", len(chats))
	}
	var body struct {
		Messages []struct{ Content string } `json:"messages"`
	}
	json.Unmarshal(chats[0].Body, &body)
	user := body.Messages[len(body.Messages)-1].Content
	for _, w := range []string{"Frage: Schornsteinfeger", "[1] Kaminkehrer — Herr Brandl kommt einmal im Jahr im Oktober. Rechnung für die Steuer aufheben.",
		"[2] Gastherme — Wartung im September, Firma Huber."} {
		if !strings.Contains(user, w) {
			t.Fatalf("the prompt lacks %q:\n%s", w, user)
		}
	}
	neverTakesTheLease(t, p)
}

// The passage the model reads is the note's, about 700 characters, not
// qmd's few lines: from a little before qmd's passage (the answer is often
// a line above it: the eval's one miss of the judge), a long note cut.
func TestTidyReadsTheNote(t *testing.T) {
	s := tidyShelf(t)
	long := "# Gastherme\n\nWartungsvertrag mit Huber, jährlich im September.\n\n- Störung F.28: Huber anrufen.\n- Heizkörper im Herbst entlüften.\n" +
		strings.Repeat("| Füllzeile | ohne Belang |\n", 60) + "GANZ-UNTEN\n"
	os.WriteFile(filepath.Join(s.dir, "heizung.md"), []byte(long), 0o644)
	head := "# Kopf\n\n" + strings.Repeat("Vorrede ohne Belang. ", 30) + "\n\nHerr Brandl kommt im Oktober.\n"
	os.WriteFile(filepath.Join(s.dir, "kaminkehrer.md"), []byte(head), 0o644)
	// qmd's passages: the Gastherme's starts at its line 5, the Kaminkehrer's at its line 5
	s.f.Results = `{"results":[` +
		`{"docid":"#a","file":"qmd://notizen/kaminkehrer.md","title":"K","score":1,"context":null,"line":5,"snippet":"5: @@ -5,1 @@ (4 before, 0 after)\n6: Herr Brandl kommt im Oktober.","vec":0.54},` +
		`{"docid":"#b","file":"qmd://notizen/heizung.md","title":"G","score":0.5,"context":null,"line":5,"snippet":"5: @@ -5,2 @@ (4 before, 61 after)\n6: - Störung F.28: Huber anrufen.\n7: - Heizkörper im Herbst entlüften.","vec":0.45}]}`
	s.find(t, s.j, time.Second, "Wer wartet die Heizung?")
	var body struct {
		Messages []struct{ Content string } `json:"messages"`
	}
	json.Unmarshal(s.p.Chats()[0].Body, &body)
	user := body.Messages[1].Content
	second := user[strings.Index(user, "[2] "):]
	second = second[:strings.Index(second, "\n")]
	if !strings.Contains(second, "Wartungsvertrag mit Huber, jährlich im September. Störung F.28: Huber anrufen.") ||
		strings.Contains(second, "GANZ-UNTEN") || strings.Contains(second, "|") || strings.Contains(second, "# ") {
		t.Fatalf("passage: %q", second)
	}
	if n := len([]rune(second)); n > 720 || n < 600 {
		t.Fatalf("passage of %d characters, want about 700", n)
	}
	// a note that fits is read whole, whatever qmd's passage
	first := user[strings.Index(user, "[1] "):strings.Index(user, "\n[2] ")]
	if !strings.HasPrefix(first, "[1] Kopf — Vorrede ohne Belang.") || !strings.HasSuffix(first, "Herr Brandl kommt im Oktober.") {
		t.Fatalf("whole note: %q", first)
	}
}

// The tidy-up never decides on its own failure, and never empties a list
// the cut kept: each of these keeps both hits.
func TestTidyKeepsTheCutsHits(t *testing.T) {
	for _, c := range []struct {
		name  string
		set   func(*proxyfake.Server)
		chats int
	}{
		{"another model loaded", func(p *proxyfake.Server) {
			p.Lease = proxyfake.Lease{State: "ready", Model: "full", Alias: "qwen3.6-35b-a3b", Holder: "pi"}
		}, 0},
		{"the house model still loading", func(p *proxyfake.Server) { p.Lease.State = "preparing" }, 0},
		{"no lease", func(p *proxyfake.Server) { p.Lease = proxyfake.Lease{State: "none", Alias: house} }, 0},
		{"refused", func(p *proxyfake.Server) { p.Status = http.StatusConflict }, 1},
		{"broken", func(p *proxyfake.Server) { p.Status = http.StatusInternalServerError }, 1},
		{"not JSON", func(p *proxyfake.Server) { p.Content = "Die zweite Notiz." }, 1},
		{"no ids", func(p *proxyfake.Server) { p.Content = `{"notes":[1]}` }, 1},
		{"a hit that is not there", func(p *proxyfake.Server) { p.Content = `{"ids":[1,3]}` }, 1},
		{"empty", func(p *proxyfake.Server) { p.Content = `{"ids":[]}` }, 1},
		{"too slow", func(p *proxyfake.Server) { p.Hang = 3 * time.Second }, 1},
	} {
		s := tidyShelf(t)
		p := s.p
		c.set(p)
		start := time.Now()
		got := s.find(t, s.j, 300*time.Millisecond, "Schornsteinfeger")
		if took := time.Since(start); took > time.Second {
			t.Errorf("%s: took %v; the tidy-up has 300 ms", c.name, took)
		}
		if got.Reduced || names(got.Hits) != "kaminkehrer.md,heizung.md" || got.Total != 2 {
			t.Errorf("%s: %+v", c.name, got)
		}
		if n := len(p.Chats()); n != c.chats {
			t.Errorf("%s: %d chats, want %d", c.name, n, c.chats)
		}
		neverTakesTheLease(t, p)
	}
}

// One hit, no hit, or no house model: nothing to tidy, and nothing asked.
func TestTidyOnlyForMoreThanOne(t *testing.T) {
	s := tidyShelf(t)
	p := s.p
	if got := s.find(t, s.j, time.Second, "Brandl"); names(got.Hits) != "kaminkehrer.md" {
		t.Fatalf("one: %+v", got)
	}
	if got := s.find(t, s.j, time.Second, "Quantenchromodynamik"); got.Total != 0 || !got.Reduced {
		t.Fatalf("none: %+v", got)
	}
	if got := s.find(t, nil, time.Second, "Schornsteinfeger"); names(got.Hits) != "kaminkehrer.md,heizung.md" {
		t.Fatalf("no house model: %+v", got)
	}
	if got := s.find(t, s.j, 0, "Schornsteinfeger"); names(got.Hits) != "kaminkehrer.md,heizung.md" {
		t.Fatalf("no time for it: %+v", got)
	}
	if s := p.Seen(); len(s) != 0 {
		t.Fatalf("asked the proxy %d times: %+v", len(s), s)
	}
}

// The caller's time bounds the tidy-up too: with less left than its
// budget, the search still answers in the caller's time.
func TestTidyWithinTheCallersTime(t *testing.T) {
	s := tidyShelf(t)
	p := s.p
	p.Hang = 5 * time.Second
	ctx, cancel := context.WithTimeout(bg, 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	got, err := Find(ctx, s.c, Tidy{Judge: s.j, Wait: BoteTidy}, s.d, "Schornsteinfeger", 10)
	if err != nil || names(got.Hits) != "kaminkehrer.md,heizung.md" {
		t.Fatalf("%+v %v", got, err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %v", took)
	}
}
