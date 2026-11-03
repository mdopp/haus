package napi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/reitti"
)

// Standortverlauf übernehmen (owner 2026-11-03): a history shared to the
// app arrives in pieces, Homes reads it and hands its points to the
// person's Reitti with their token, and GET says how it ended. A fake
// Reitti takes the GeoJSON import and records what reached it.

type fakeImport struct {
	*httptest.Server
	mu      sync.Mutex
	tokens  []string
	points  int
	answer  func(n int) (int, string) // default: the yes with the count
	batches []int
}

func newFakeImport(t *testing.T) *fakeImport {
	t.Helper()
	f := &fakeImport{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == reitti.IngestPath { // Verbinden's check
			io.WriteString(w, `{"result":"ok"}`)
			return
		}
		if r.Method != "POST" || r.URL.Path != reitti.ImportPath {
			http.NotFound(w, r)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file", 400)
			return
		}
		var fc struct {
			Features []json.RawMessage `json:"features"`
		}
		json.NewDecoder(file).Decode(&fc)
		f.mu.Lock()
		f.tokens = append(f.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		ans := f.answer
		f.mu.Unlock()
		code, body := 200, `{"success":true,"pointsScheduled":`+strconv.Itoa(len(fc.Features))+`}`
		if ans != nil {
			code, body = ans(len(fc.Features))
		}
		if code == 200 {
			f.mu.Lock()
			f.points += len(fc.Features)
			f.batches = append(f.batches, len(fc.Features))
			f.mu.Unlock()
		}
		w.WriteHeader(code)
		io.WriteString(w, body)
	}))
	t.Cleanup(f.Close)
	return f
}

type importHouse struct {
	t   *testing.T
	h   http.Handler
	r   *reitti.Reitti
	dir string
}

// anna and ben connected Reitti, cara did not.
func newImportHouse(t *testing.T, f *fakeImport) *importHouse {
	t.Helper()
	r := &reitti.Reitti{URL: f.URL, Dir: t.TempDir(), HTTP: f.Client()}
	for user, tok := range map[string]string{"anna": "anna-geraet-token", "ben": "ben-geraet-token"} {
		if err := r.Verbinden(t.Context(), user, tok); err != nil {
			t.Fatal(err)
		}
	}
	people := map[string]string{"anna1": "anna", "ben1": "ben", "cara1": "cara"}
	h := Handler(Config{Validate: StaticValidator([]string{"anna1", "ben1", "cara1"}), Reitti: r,
		Whoami: func(b string) (string, bool) { p, ok := people[b]; return p, ok }})
	return &importHouse{t: t, h: h, r: r, dir: filepath.Join(r.Dir, "standort-import")}
}

func (ih *importHouse) do(method, bearer, path string, body []byte, header map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	ih.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+bearer)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	ih.h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func (ih *importHouse) create(bearer, name string, size int) (int, map[string]any) {
	rec, out := ih.do("POST", bearer, "/napi/standort/import", []byte(`{"name":"`+name+`","groesse":`+strconv.Itoa(size)+`}`), nil)
	return rec.Code, out
}

func (ih *importHouse) patch(bearer, id string, at int, piece []byte) (int, map[string]any) {
	rec, out := ih.do("PATCH", bearer, "/napi/standort/import/"+id, piece,
		map[string]string{"Content-Type": "application/offset+octet-stream", "Upload-Offset": strconv.Itoa(at)})
	return rec.Code, out
}

// wait polls GET until the import ended, as the app does.
func (ih *importHouse) wait(bearer, id string) map[string]any {
	ih.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec, out := ih.do("GET", bearer, "/napi/standort/import/"+id, nil, nil)
		if rec.Code != 200 {
			ih.t.Fatalf("GET → %d %v", rec.Code, out)
		}
		if out["stand"] == standFertig || out["stand"] == standFehler {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
	ih.t.Fatal("the import did not end")
	return nil
}

// upload sends file whole in pieces of size piece and waits for the end.
func (ih *importHouse) upload(bearer, name string, file []byte, piece int) (string, map[string]any) {
	ih.t.Helper()
	code, out := ih.create(bearer, name, len(file))
	if code != 201 || out["offset"] != 0.0 {
		ih.t.Fatalf("create → %d %v", code, out)
	}
	id := out["id"].(string)
	for at := 0; at < len(file); at += piece {
		end := min(at+piece, len(file))
		code, out := ih.patch(bearer, id, at, file[at:end])
		if code != 200 || out["offset"] != float64(end) || out["fertig"] != (end == len(file)) {
			ih.t.Fatalf("PATCH at %d → %d %v", at, code, out)
		}
	}
	return id, ih.wait(bearer, id)
}

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "reitti", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Each format the person may share comes over: the points with anna's
// token, the count and the time span in one sentence, and the file gone.
func TestImportBringsEachFormatToTheirReitti(t *testing.T) {
	for _, c := range []struct {
		file, format string
		punkte       float64
		besuche      float64
		text         string
	}{
		{"records.json", "records", 3, 0, "3 Orte von März 2014 bis Dezember 2024 übernommen."},
		{"timeline-android.json", "timeline", 4, 1, "4 Orte aus dem Mai 2020 übernommen, darunter 1 Aufenthalt."},
		{"timeline-ios.json", "timeline-ios", 4, 1, "4 Orte aus dem Juli 2021 übernommen"},
		{"punkte.geojson", "geojson", 2, 0, "2 Orte aus dem Januar 2022 übernommen."},
		{"spur.gpx", "gpx", 3, 0, "3 Orte aus dem Juni 2023 übernommen."},
	} {
		t.Run(c.file, func(t *testing.T) {
			f := newFakeImport(t)
			ih := newImportHouse(t, f)
			file := testdata(t, c.file)
			id, out := ih.upload("anna1", c.file, file, 100)
			if out["stand"] != standFertig || out["format"] != c.format || out["punkte"] != c.punkte || out["besuche"] != c.besuche ||
				out["gelesen"] != float64(len(file)) || !strings.HasPrefix(out["text"].(string), c.text) {
				t.Fatalf("→ %v", out)
			}
			if f.points != int(c.punkte) || len(f.tokens) != 1 || f.tokens[0] != "anna-geraet-token" {
				t.Fatalf("reitti got %d points with %q", f.points, f.tokens)
			}
			if _, err := os.Stat(filepath.Join(ih.dir, "anna", id+".teil")); !os.IsNotExist(err) {
				t.Fatalf("the file is still on the box: %v", err)
			}
		})
	}
}

// A long history goes in batches; the sentence counts them all.
func TestImportSendsBatches(t *testing.T) {
	defer func(n int) { importStapel = n }(importStapel)
	importStapel = 2
	f := newFakeImport(t)
	ih := newImportHouse(t, f)
	var b strings.Builder
	b.WriteString(`{"locations":[`)
	for i := 0; i < 12345; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"latitudeE7":470000000,"longitudeE7":80000000,"timestamp":"` + time.Date(2014, 3, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i)*7*time.Hour).Format(time.RFC3339) + `"}`)
	}
	b.WriteString(`]}`)
	_, out := ih.upload("anna1", "Records.json", []byte(b.String()), 64<<10)
	if out["stand"] != standFertig || out["punkte"] != 12345.0 || !strings.HasPrefix(out["text"].(string), "12.345 Orte von März 2014 bis Januar 2024 übernommen.") {
		t.Fatalf("→ %v", out)
	}
	if len(f.batches) != 6173 || f.batches[0] != 2 || f.batches[len(f.batches)-1] != 1 {
		t.Fatalf("%d batches", len(f.batches))
	}
}

// Without a Reitti token Homes takes nothing and says where to connect.
func TestImportWithoutReittiPointsToStandort(t *testing.T) {
	ih := newImportHouse(t, newFakeImport(t))
	code, out := ih.create("cara1", "Timeline.json", 10)
	if code != 409 || out["grund"] != grundKeinSchluessel || !strings.Contains(out["fehler"].(string), "Mehr → Standortverlauf") {
		t.Fatalf("→ %d %v", code, out)
	}
	if es, _ := os.ReadDir(filepath.Join(ih.dir, "cara")); len(es) != 0 {
		t.Fatalf("kept %v", es)
	}
}

// A cut connection resumes where the box stands: HEAD says it, a PATCH at
// another offset is told where, and the same file named again is the same
// upload. Another person's id is none of theirs.
func TestImportResumes(t *testing.T) {
	f := newFakeImport(t)
	ih := newImportHouse(t, f)
	file := testdata(t, "spur.gpx")
	_, out := ih.create("anna1", "spur.gpx", len(file))
	id := out["id"].(string)
	if code, _ := ih.patch("anna1", id, 0, file[:300]); code != 200 {
		t.Fatal(code)
	}
	rec, _ := ih.do("HEAD", "anna1", "/napi/standort/import/"+id, nil, nil)
	if rec.Code != 204 || rec.Header().Get("Upload-Offset") != "300" {
		t.Fatalf("HEAD → %d %q", rec.Code, rec.Header().Get("Upload-Offset"))
	}
	if code, out := ih.patch("anna1", id, 0, file[:300]); code != 409 || out["offset"] != 300.0 {
		t.Fatalf("a PATCH at the wrong offset → %d %v", code, out)
	}
	if code, out := ih.create("anna1", "spur.gpx", len(file)); code != 201 || out["id"] != id || out["offset"] != 300.0 {
		t.Fatalf("the same file again → %d %v", code, out)
	}
	if rec, _ := ih.do("HEAD", "ben1", "/napi/standort/import/"+id, nil, nil); rec.Code != 404 {
		t.Fatalf("ben sees anna's upload: %d", rec.Code)
	}
	if code, _ := ih.patch("anna1", id, 300, file[300:]); code != 200 {
		t.Fatal(code)
	}
	if out := ih.wait("anna1", id); out["stand"] != standFertig || out["punkte"] != 3.0 {
		t.Fatalf("→ %v", out)
	}
	if rec, _ := ih.do("HEAD", "anna1", "/napi/standort/import/"+id, nil, nil); rec.Code != 204 || rec.Header().Get("Upload-Offset") != strconv.Itoa(len(file)) {
		t.Fatalf("HEAD after the end → %d %q", rec.Code, rec.Header().Get("Upload-Offset"))
	}
}

// What cannot come over ends in a German sentence, and the file is gone.
func TestImportEndsInWords(t *testing.T) {
	defer func(d time.Duration) { importPause = d }(importPause)
	importPause = time.Millisecond
	for _, c := range []struct {
		name   string
		file   []byte
		answer func(int) (int, string)
		grund  string
		says   string
	}{
		{"fremd.json", testdata(t, "fremd.json"), nil, "format", "kein Standortverlauf, den Homes kennt"},
		{"leer.json", []byte(`{"locations":[]}`), nil, "keine_orte", "keine Orte"},
		{"abgeschnitten.json", testdata(t, "abgeschnitten.json"), nil, "format", "Reitti hat bis dahin 1 Ort übernommen"},
		{"records.json", testdata(t, "records.json"), func(int) (int, string) { return 401, "" }, grundAbgelehnt, "Reitti kennt deinen Schlüssel nicht mehr"},
		{"records.json", testdata(t, "records.json"), func(int) (int, string) { return 503, "" }, grundReittiWeg, "Reitti nimmt gerade nichts an"},
		{"records.json", testdata(t, "records.json"), func(int) (int, string) { return 400, `{"error":"Token has no device attached."}` }, grundReittiWeg, "Token has no device"},
	} {
		t.Run(c.name+" "+c.grund, func(t *testing.T) {
			f := newFakeImport(t)
			f.answer = c.answer
			ih := newImportHouse(t, f)
			id, out := ih.upload("ben1", c.name, c.file, 1<<20)
			if out["stand"] != standFehler || out["grund"] != c.grund || !strings.Contains(out["fehler"].(string), c.says) {
				t.Fatalf("→ %v", out)
			}
			if _, err := os.Stat(filepath.Join(ih.dir, "ben", id+".teil")); !os.IsNotExist(err) {
				t.Fatal("the file stayed")
			}
		})
	}
}

// A Homes restarted mid-import goes on when the phone asks.
func TestImportGoesOnAfterARestart(t *testing.T) {
	f := newFakeImport(t)
	ih := newImportHouse(t, f)
	file := testdata(t, "punkte.geojson")
	id := importID("anna", "punkte.geojson", int64(len(file)))
	os.MkdirAll(filepath.Join(ih.dir, "anna"), 0o700)
	os.WriteFile(filepath.Join(ih.dir, "anna", id+".teil"), file, 0o600)
	st, _ := json.Marshal(importStand{Name: "punkte.geojson", Groesse: int64(len(file)), Stand: standLaeuft, Geaendert: time.Now()})
	os.WriteFile(filepath.Join(ih.dir, "anna", id+".json"), st, 0o600)
	if out := ih.wait("anna1", id); out["stand"] != standFertig || out["punkte"] != 2.0 {
		t.Fatalf("→ %v", out)
	}
}

func TestImportRefusesWhatItCannotTake(t *testing.T) {
	ih := newImportHouse(t, newFakeImport(t))
	for body, want := range map[string]int{
		`{"name":"a.json"}`:                      400,
		`{"name":"a.json","groesse":0}`:          400,
		`{"name":"","groesse":5}`:                400,
		`{"name":"a/b.json","groesse":5}`:        400,
		`{"name":"a.json","groesse":6000000000}`: 413,
	} {
		rec, out := ih.do("POST", "anna1", "/napi/standort/import", []byte(body), nil)
		if rec.Code != want || out["fehler"] == nil {
			t.Errorf("%s → %d %v", body, rec.Code, out)
		}
	}
	if rec, _ := ih.do("GET", "anna1", "/napi/standort/import/0123456789abcdef0123456789abcdef", nil, nil); rec.Code != 404 {
		t.Errorf("an unknown id → %d", rec.Code)
	}
	if rec, _ := ih.do("GET", "anna1", "/napi/standort/import/..%2F..%2Fx", nil, nil); rec.Code != 404 {
		t.Errorf("a path as id → %d", rec.Code)
	}
}

func TestUebernommenSays(t *testing.T) {
	von, bis := time.Date(2014, 3, 2, 0, 0, 0, 0, time.UTC), time.Date(2024, 12, 30, 0, 0, 0, 0, time.UTC)
	if s := uebernommen(importStand{Punkte: 12345, Von: &von, Bis: &bis}); !strings.HasPrefix(s, "12.345 Orte von März 2014 bis Dezember 2024 übernommen.") {
		t.Error(s)
	}
	if s := uebernommen(importStand{Punkte: 1, Von: &von, Bis: &von}); !strings.HasPrefix(s, "1 Ort aus dem März 2014 übernommen.") {
		t.Error(s)
	}
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1.000", 1234567: "1.234.567"} {
		if zahl(n) != want {
			t.Errorf("zahl(%d) = %s", n, zahl(n))
		}
	}
}
