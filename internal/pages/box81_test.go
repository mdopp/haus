package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
)

// a Laib whose status takes as long as slow says; the rest answers "[]"
func slowStatusLaib(t *testing.T, slow time.Duration) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := "[]"
		if req.Params.Name == "laib_befunde" { // diag ran, found nothing
			text = `{"at":"2026-11-06T10:00:00Z","findings":[]}`
		}
		if req.Params.Name == "laib_status" {
			select {
			case <-time.After(slow):
			case <-r.Context().Done():
				return
			}
			b, _ := json.Marshal(map[string]any{"ready": allGreen(t), "services": []map[string]any{{"name": "paperless", "state": "grün", "detail": "läuft"}}})
			text = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

func boxAt(h http.HandlerFunc, target, groups string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Remote-Groups", groups)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// haus#81: Box comes at once — the frame, each part's place with its
// heading and „lädt …“ — and its script fetches every part at the same
// time from /box/teil; laib's status taking its time holds up nothing.
// Without script the page goes to /box?ganz=1, read whole.
func TestBoxComesAtOnceAndFillsItsParts(t *testing.T) {
	c := slowStatusLaib(t, 800*time.Millisecond)
	h := BoxPage(BoxConfig{Laib: c, LaibURL: "https://laib.box.test"})
	t0 := time.Now()
	rec := boxAt(h, "/box", "admins")
	if d := time.Since(t0); d > 400*time.Millisecond {
		t.Fatalf("the frame took %v", d)
	}
	body := rec.Body.String()
	mustContain(t, body, `<div id=box-stand data-teil=stand aria-busy=true><h1>Box</h1><p class=k>lädt …</p></div>`,
		`<div id=box-operator data-teil=operator aria-busy=true><h2>Operator</h2>`,
		`["stand","operator"].forEach`, "fetch('/box/teil?was='+was", `<noscript><meta http-equiv=refresh content="0;url=/box?ganz=1"></noscript>`, saysPartFailed,
		`<nav class=unter aria-label=Box><a href=/box aria-current=page>Übersicht</a><a href=/box/aufgaben>Aufgaben</a><a href=/box/auslastung>Auslastung</a><a href=/box/haushalt>Haushalt</a></nav>`)
	mustNotContain(t, body, "Was wird genutzt", "Werkstatt", "box-dienste", "box-journal", "box-einladen", "box-aufgaben", ">Nutzung</a>", ">Personen</a>")
	before(t, body, "box-stand", "box-operator")
	// four tabs (owner 2026-11-06); Haushalt's rows are pages of their own, Haushalt marked
	for page, want := range map[string]string{"/box/haushalt": "<h1>Haushalt</h1>", "/box/personen": "<h1>Personen</h1>"} {
		rec := boxAt(h, page, "admins")
		mustContain(t, rec.Body.String(), want, `href=/box/haushalt aria-current=page>`)
	}
	if rec := boxAt(h, "/box/nutzung", "admins"); rec.Code != http.StatusNotFound { // nothing counted on this box
		t.Errorf("Nutzung without counting → %d", rec.Code)
	}
	if rec := boxAt(h, "/box/gibtsnicht", "admins"); rec.Code != http.StatusNotFound {
		t.Errorf("no such page → %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
	}

	// a part: its places as JSON, the slow one too, once it came
	parts := func(was string) map[string]string {
		t.Helper()
		rec := boxAt(h, "/box/teil?was="+was, "admins")
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s: %d %q", was, rec.Code, rec.Header().Get("Content-Type"))
		}
		var got map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := parts("operator")
	mustContain(t, got["box-operator"], "<details id=operator><summary>Operator:")
	got = parts("stand")
	mustContain(t, got["box-stand"], "<h1>Box</h1>", "Alles in Ordnung.", "<h2>Morgenbericht</h2>")
	for _, gone := range []string{"inbox", "bericht", "journal"} {
		if rec := boxAt(h, "/box/teil?was="+gone, "admins"); rec.Code != http.StatusNotFound {
			t.Errorf("part %s → %d", gone, rec.Code)
		}
	}

	// a part that does not come in time says so in its place
	old := boxPartWait
	boxPartWait = 100 * time.Millisecond
	defer func() { boxPartWait = old }()
	got = parts("stand")
	mustContain(t, got["box-stand"], "<h1>Box</h1><p class=rot>"+saysLaibSilent)

	// no such part; the family gets no part
	if rec := boxAt(h, "/box/teil?was=geheim", "admins"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown part → %d", rec.Code)
	}
	if rec := boxAt(h, "/box/teil?was=stand", "family"); rec.Code != http.StatusSeeOther || strings.Contains(rec.Body.String(), "Inbox") {
		t.Errorf("family → %d", rec.Code)
	}
}

// ?ganz=1 reads every part at the same time and comes whole, no script.
func TestBoxWholeReadsThePartsTogether(t *testing.T) {
	h := BoxPage(BoxConfig{Laib: slowStatusLaib(t, 300*time.Millisecond), LaibURL: "https://laib.box.test"})
	rec := boxAt(h, "/box?ganz=1", "admins")
	body := rec.Body.String()
	mustContain(t, body, "<div id=box-stand data-teil=stand aria-busy=false>", "Operator:", "Alles in Ordnung.")
	mustNotContain(t, body, "fetch('/box/teil", saysPartLoading)
}

// haus#96 (laib#593), haus#157: the report part asks for what it shows —
// the newest morning report, nothing else of the journal.
func TestBoxJournalAsksForWhatItShows(t *testing.T) {
	var mu sync.Mutex
	var asked []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := "[]"
		if req.Params.Name == "laib_status" {
			text = `{"ready":{"state":"bereit"},"services":[]}`
		}
		if req.Params.Name == "laib_journal" {
			mu.Lock()
			asked = append(asked, req.Params.Args)
			mu.Unlock()
			if req.Params.Args["kind"] == "report" {
				text = `[{"t":"` + time.Now().UTC().Format(time.RFC3339) + `","actor":"ops","verb":"note","reason":"report","result":"info","detail":"Nachts alles ruhig."}]`
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	h := BoxPage(BoxConfig{Laib: &laib.Client{URL: srv.URL, Token: "t"}, LaibURL: "https://laib.box.test"})
	rec := boxAt(h, "/box/teil?was=stand", "admins")
	mustContain(t, rec.Body.String(), "Nachts alles ruhig.")
	if len(asked) != 1 {
		t.Fatalf("asked %v", asked)
	}
	want := map[string]bool{`{"kind":"report","limit":"1"}`: true}
	for _, a := range asked {
		b, _ := json.Marshal(a)
		if !want[string(b)] {
			t.Errorf("asked %s", b)
		}
	}
}
