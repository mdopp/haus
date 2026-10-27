package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/laib"
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
		`<div id=box-inbox data-teil=inbox aria-busy=true><h2>Inbox</h2>`, `<div id=box-bericht data-teil=journal aria-busy=true><h2>Morgenbericht</h2>`,
		`["stand","inbox","journal"].forEach`, "fetch('/box/teil?was='+was", `<noscript><meta http-equiv=refresh content="0;url=/box?ganz=1"></noscript>`, saysPartFailed,
		`<nav class=unter aria-label=Box><a href=/box aria-current=page>Übersicht</a><a href=/box/aufgaben>Aufgaben</a><a href=/box/dienste>Dienste</a><a href=/box/journal>Journal</a><a href=/box/personen>Personen</a><a href=/box/geraete>Geräte</a><a href=/adresse>Adresse</a></nav>`)
	mustNotContain(t, body, "Was wird genutzt", "Werkstatt", "Operator:", "box-laeuft", "box-journal", "box-einladen", "box-aufgaben", ">Nutzung</a>")
	before(t, body, "box-stand", "box-inbox")
	before(t, body, "box-inbox", "box-bericht")
	// each topic on its own page (owner 2026-10-26: "zu viele lange Themen vermischt")
	for page, want := range map[string]string{"/box/dienste": "<h1>Dienste</h1>", "/box/journal": "<h1>Journal</h1>", "/box/personen": "<h1>Personen</h1>"} {
		rec := boxAt(h, page, "admins")
		mustContain(t, rec.Body.String(), want, `href=`+page+` aria-current=page>`)
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
	got := parts("inbox")
	mustContain(t, got["box-inbox"], "<h2>Inbox</h2>", "leer")
	got = parts("journal")
	mustContain(t, got["box-bericht"], "<h2>Morgenbericht</h2>")
	mustContain(t, got["box-journal"], "<h2>Journal · 24h</h2><ul class=zeilen>")
	got = parts("stand")
	mustContain(t, got["box-stand"], "<h1>Box · <span class=gruen>bereit</span>", "Operator:")
	mustContain(t, got["box-laeuft"], "<h2>Was läuft</h2>", "paperless")

	// a part that does not come in time says so in its place
	old := boxPartWait
	boxPartWait = 100 * time.Millisecond
	defer func() { boxPartWait = old }()
	got = parts("stand")
	mustContain(t, got["box-stand"], "<h1>Box</h1><p class=rot>Laib antwortet nicht")

	// no such part; the family gets no part
	if rec := boxAt(h, "/box/teil?was=geheim", "admins"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown part → %d", rec.Code)
	}
	if rec := boxAt(h, "/box/teil?was=inbox", "family"); rec.Code != http.StatusSeeOther || strings.Contains(rec.Body.String(), "Inbox") {
		t.Errorf("family → %d", rec.Code)
	}
}

// ?ganz=1 reads every part at the same time and comes whole, no script.
func TestBoxWholeReadsThePartsTogether(t *testing.T) {
	h := BoxPage(BoxConfig{Laib: slowStatusLaib(t, 300*time.Millisecond), LaibURL: "https://laib.box.test"})
	rec := boxAt(h, "/box?ganz=1", "admins")
	body := rec.Body.String()
	mustContain(t, body, "<div id=box-stand data-teil=stand aria-busy=false>", "Operator:", "<h2>Inbox</h2><p class=k>leer</p>")
	mustContain(t, boxAt(h, "/box/dienste?ganz=1", "admins").Body.String(), "<h2>Was läuft</h2>", "paperless")
	mustNotContain(t, body, "fetch('/box/teil", saysPartLoading)
}

// haus#96 (laib#593): the journal part asks for what it shows — the newest
// 25, and the newest morning report — never 36 hours.
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
	rec := boxAt(h, "/box/teil?was=journal", "admins")
	mustContain(t, rec.Body.String(), "Nachts alles ruhig.")
	if len(asked) != 2 {
		t.Fatalf("asked %v", asked)
	}
	want := map[string]bool{`{"limit":"25"}`: true, `{"kind":"report","limit":"1"}`: true}
	for _, a := range asked {
		b, _ := json.Marshal(a)
		if !want[string(b)] {
			t.Errorf("asked %s", b)
		}
	}
}
