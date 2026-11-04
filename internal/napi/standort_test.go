package napi

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/reitti"
)

// Standortverlauf (owner 2026-11-02): the app's points pass through Homes
// to the person's Reitti, with the person's own token. A fake Reitti
// records what reached it.

type fakeReitti struct {
	*httptest.Server
	mu     sync.Mutex
	tokens []string
	bodies []string
	answer func(token string) (int, string) // default 200 {"result":"ok"}
}

func newFakeReitti(t *testing.T) *fakeReitti {
	t.Helper()
	f := &fakeReitti{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != reitti.IngestPath || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "not the overland ingest", http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		f.tokens = append(f.tokens, tok)
		f.bodies = append(f.bodies, string(b))
		ans := f.answer
		f.mu.Unlock()
		code, body := 200, `{"result":"ok"}`
		if ans != nil {
			code, body = ans(tok)
		}
		w.WriteHeader(code)
		io.WriteString(w, body)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeReitti) calls() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokens...), append([]string(nil), f.bodies...)
}

// a box with Reitti: anna and ben connected, cara not; waise1 is a device of nobody
func standortHouse(t *testing.T, f *fakeReitti) (*reitti.Reitti, http.Handler) {
	t.Helper()
	r := &reitti.Reitti{URL: f.URL, Dir: t.TempDir(), Now: func() time.Time { return time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC) }}
	for user, tok := range map[string]string{"anna": "anna-geraet-token", "ben": "ben-geraet-token"} {
		if err := r.Verbinden(t.Context(), user, tok); err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	f.tokens, f.bodies = nil, nil // the checks while connecting
	f.mu.Unlock()
	people := map[string]string{"anna1": "anna", "ben1": "ben", "cara1": "cara", "waise1": ""}
	return r, Handler(Config{Validate: StaticValidator([]string{"anna1", "ben1", "cara1", "waise1"}), Reitti: r,
		Whoami: func(b string) (string, bool) { p, ok := people[b]; return p, ok }})
}

func sendStandort(t *testing.T, h http.Handler, method, bearer, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/napi/standort", rd)
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

const zweiPunkte = `{"punkte":[
 {"zeit":"2026-11-02T07:15:00Z","breite":52.52,"laenge":13.405,"genauigkeit":12.5,"hoehe":34,"tempo":1.4},
 {"zeit":"2026-11-02T08:15:00+01:00","breite":52.5201,"laenge":13.4051,"genauigkeit":30}]}`

// The relay forwards exactly the person's points, in order, as Overland
// GeoJSON ([longitude, latitude]), with that person's token — anna's
// with anna's, ben's with ben's.
func TestStandortForwardsThePersonsPointsWithTheirToken(t *testing.T) {
	f := newFakeReitti(t)
	_, h := standortHouse(t, f)
	rec, out := sendStandort(t, h, "POST", "anna1", zweiPunkte)
	if rec.Code != 200 || out["angenommen"] != 2.0 || out["verworfen"] != 0.0 {
		t.Fatalf("→ %d %v", rec.Code, out)
	}
	rec, out = sendStandort(t, h, "POST", "ben1", `{"punkte":[{"zeit":"2026-11-02T10:00:00Z","breite":48.1,"laenge":11.5,"genauigkeit":8}]}`)
	if rec.Code != 200 || out["angenommen"] != 1.0 {
		t.Fatalf("ben → %d %v", rec.Code, out)
	}
	tokens, bodies := f.calls()
	if len(tokens) != 2 || tokens[0] != "anna-geraet-token" || tokens[1] != "ben-geraet-token" {
		t.Fatalf("tokens %q", tokens)
	}
	var got struct {
		Locations []struct {
			Type     string `json:"type"`
			Geometry struct {
				Type        string    `json:"type"`
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties map[string]any `json:"properties"`
		} `json:"locations"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Locations) != 2 {
		t.Fatalf("anna's batch reached Reitti as %s", bodies[0])
	}
	a, b := got.Locations[0], got.Locations[1]
	if a.Type != "Feature" || a.Geometry.Type != "Point" || a.Geometry.Coordinates[0] != 13.405 || a.Geometry.Coordinates[1] != 52.52 {
		t.Errorf("first point %+v", a)
	}
	if a.Properties["timestamp"] != "2026-11-02T07:15:00Z" || a.Properties["horizontal_accuracy"] != 12.5 || a.Properties["altitude"] != 34.0 || a.Properties["speed"] != 1.4 {
		t.Errorf("first point's properties %v", a.Properties)
	}
	if b.Properties["timestamp"] != "2026-11-02T07:15:00Z" || b.Geometry.Coordinates[1] != 52.5201 {
		t.Errorf("second point (08:15+01:00 is 07:15Z) %v %v", b.Properties, b.Geometry)
	}
	if _, ok := b.Properties["altitude"]; ok {
		t.Errorf("an altitude nobody measured: %v", b.Properties)
	}
	if strings.Contains(bodies[1], "52.52") || !strings.Contains(bodies[1], "48.1") {
		t.Errorf("ben's batch carries another's points: %s", bodies[1])
	}
}

// Without a token Homes keeps nothing and sends nothing: 409 with the
// sentence that says what to do, so the app keeps the batch.
func TestStandortWithoutATokenKeepsTheBatchOnThePhone(t *testing.T) {
	f := newFakeReitti(t)
	_, h := standortHouse(t, f)
	rec, out := sendStandort(t, h, "POST", "cara1", zweiPunkte)
	if rec.Code != 409 || out["grund"] != "kein_schluessel" || !strings.Contains(out["fehler"].(string), "Mehr → Standortverlauf") {
		t.Fatalf("→ %d %v", rec.Code, out)
	}
	if tokens, _ := f.calls(); len(tokens) != 0 {
		t.Fatalf("Reitti was called without a token: %v", tokens)
	}
}

// Reitti not taking the points — an error, no answer, a yes that is not
// one — is 503 with Retry-After: the app keeps the batch and comes again.
// A refused token is 409, said as such. Never 200.
func TestStandortAReittiErrorKeepsTheBatch(t *testing.T) {
	f := newFakeReitti(t)
	r, h := standortHouse(t, f)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	for _, c := range []struct {
		name       string
		code       int
		body       string
		want       int
		grund      string
		retryAfter bool
	}{
		{"error", 500, `{"error":"Token has no device attached"}`, 503, "reitti_weg", true},
		{"busy", 503, ``, 503, "reitti_weg", true},
		{"login page", 200, `<html>login</html>`, 503, "reitti_weg", true},
		{"redirect to login", 302, ``, 503, "reitti_weg", true},
		{"token refused", 401, ``, 409, "schluessel_abgelehnt", false},
	} {
		f.mu.Lock()
		f.answer = func(string) (int, string) { return c.code, c.body }
		f.mu.Unlock()
		rec, out := sendStandort(t, h, "POST", "anna1", zweiPunkte)
		if rec.Code != c.want || out["grund"] != c.grund || out["fehler"] == nil || out["angenommen"] != nil {
			t.Errorf("%s: → %d %v", c.name, rec.Code, out)
		}
		if (rec.Header().Get("Retry-After") != "") != c.retryAfter {
			t.Errorf("%s: Retry-After %q", c.name, rec.Header().Get("Retry-After"))
		}
	}
	f.Close() // Reitti gone
	rec, out := sendStandort(t, h, "POST", "anna1", zweiPunkte)
	if rec.Code != 503 || out["grund"] != "reitti_weg" {
		t.Errorf("Reitti down → %d %v", rec.Code, out)
	}
	if strings.Contains(logs.String(), "anna-geraet-token") {
		t.Errorf("the token is in the log:\n%s", logs.String())
	}
	if _, ok := r.Konto("anna"); !ok {
		t.Errorf("a refusal forgot anna's token: she may only have to wait")
	}
}

// A point Homes cannot read is not passed on and is counted; one batch
// is at most 1000 points; an unreadable body is 400.
func TestStandortCountsWhatItCannotRead(t *testing.T) {
	f := newFakeReitti(t)
	_, h := standortHouse(t, f)
	rec, out := sendStandort(t, h, "POST", "anna1", `{"punkte":[
 {"zeit":"2026-11-02T07:15:00Z","breite":52.52,"laenge":13.405,"genauigkeit":12},
 {"zeit":"gestern","breite":52.52,"laenge":13.405,"genauigkeit":12},
 {"zeit":"2026-11-02T07:16:00Z","breite":95,"laenge":13.405,"genauigkeit":12},
 {"zeit":"2026-11-02T07:17:00Z","breite":52.52,"laenge":13.405}]}`)
	if rec.Code != 200 || out["angenommen"] != 1.0 || out["verworfen"] != 3.0 {
		t.Fatalf("→ %d %v", rec.Code, out)
	}
	if _, bodies := f.calls(); len(bodies) != 1 || strings.Count(bodies[0], `"Feature"`) != 1 {
		t.Fatalf("Reitti got %v", bodies)
	}
	rec, out = sendStandort(t, h, "POST", "anna1", `{"punkte":[{"zeit":"nie"}]}`)
	if rec.Code != 200 || out["angenommen"] != 0.0 || out["verworfen"] != 1.0 {
		t.Fatalf("nothing readable → %d %v", rec.Code, out)
	}
	if _, bodies := f.calls(); len(bodies) != 1 {
		t.Fatalf("an empty batch went to Reitti: %v", bodies)
	}
	many := `{"punkte":[` + strings.TrimSuffix(strings.Repeat(`{"zeit":"2026-11-02T07:15:00Z","breite":1,"laenge":1,"genauigkeit":1},`, 1001), ",") + `]}`
	if rec, _ := sendStandort(t, h, "POST", "anna1", many); rec.Code != 413 {
		t.Errorf("1001 points → %d", rec.Code)
	}
	if rec, out := sendStandort(t, h, "POST", "anna1", `{"punkte":`); rec.Code != 400 || out["fehler"] == nil {
		t.Errorf("broken body → %d %v", rec.Code, out)
	}
}

// GET says whether the person connected Reitti and since when — never the token.
func TestStandortSaysWhetherConnected(t *testing.T) {
	f := newFakeReitti(t)
	_, h := standortHouse(t, f)
	rec, out := sendStandort(t, h, "GET", "anna1", "")
	if rec.Code != 200 || out["verbunden"] != true || out["seit"] != "2026-11-02T09:00:00Z" || strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("anna → %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
	}
	if rec, out := sendStandort(t, h, "GET", "cara1", ""); rec.Code != 200 || out["verbunden"] != false || out["seit"] != nil {
		t.Errorf("cara → %d %v", rec.Code, out)
	}
	if rec, out := sendStandort(t, h, "POST", "waise1", zweiPunkte); rec.Code != 403 || out["fehler"] == nil {
		t.Errorf("a device of nobody → %d %v", rec.Code, out)
	}
	if rec, _ := sendStandort(t, h, "POST", "fremd", zweiPunkte); rec.Code != 401 {
		t.Errorf("no device → %d", rec.Code)
	}
	bare := Handler(Config{Validate: StaticValidator([]string{"anna1"}), Whoami: func(string) (string, bool) { return "anna", true }})
	for _, m := range []string{"GET", "POST"} {
		if rec, out := sendStandort(t, bare, m, "anna1", map[string]string{"GET": "", "POST": zweiPunkte}[m]); rec.Code != 503 || out["grund"] != "nicht_eingerichtet" {
			t.Errorf("%s without Reitti → %d %v", m, rec.Code, out)
		}
	}
}

// A batch Reitti took keeps its newest point's time for the page
// Standortverlauf (never past now); a batch it did not take keeps nothing.
func TestStandortKeepsWhenTheNewestPointCame(t *testing.T) {
	f := newFakeReitti(t)
	r, h := standortHouse(t, f)
	if rec, _ := sendStandort(t, h, "POST", "anna1", zweiPunkte); rec.Code != 200 {
		t.Fatalf("→ %d", rec.Code)
	}
	if k, _ := r.Konto("anna"); !k.Zuletzt.Equal(time.Date(2026, 11, 2, 7, 15, 0, 0, time.UTC)) {
		t.Fatalf("anna's newest point: %v", k.Zuletzt)
	}
	f.mu.Lock()
	f.answer = func(string) (int, string) { return 503, "" }
	f.mu.Unlock()
	if rec, _ := sendStandort(t, h, "POST", "ben1", `{"punkte":[{"zeit":"2026-11-02T08:00:00Z","breite":48.1,"laenge":11.5,"genauigkeit":8}]}`); rec.Code != 503 {
		t.Fatalf("ben, Reitti down → %d", rec.Code)
	}
	if k, _ := r.Konto("ben"); !k.Zuletzt.IsZero() {
		t.Fatalf("a batch Reitti did not take counts: %v", k.Zuletzt)
	}
}
