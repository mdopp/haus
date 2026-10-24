package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		`<div id=box-inbox data-teil=inbox aria-busy=true><h2>Inbox</h2>`, `<div id=box-journal data-teil=journal aria-busy=true><h2>Journal · 24h</h2>`,
		`["stand","inbox","einladen","journal"].forEach`, "fetch('/box/teil?was='+was", `<noscript><meta http-equiv=refresh content="0;url=/box?ganz=1"></noscript>`, saysPartFailed)
	mustNotContain(t, body, "Was wird genutzt", "Werkstatt", "Operator:")
	before(t, body, "box-stand", "box-inbox")
	before(t, body, "box-bericht", "box-laeuft")
	before(t, body, "box-laeuft", "box-journal")
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
	mustContain(t, body, "<div id=box-stand data-teil=stand aria-busy=false>", "Operator:", "<h2>Inbox</h2><p class=k>leer</p>", "<h2>Was läuft</h2>", "paperless")
	mustNotContain(t, body, "fetch('/box/teil", saysPartLoading)
}
