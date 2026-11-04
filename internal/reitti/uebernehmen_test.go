package reitti

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The points go to Reitti's GeoJSON import as the multipart file its
// controller reads, with the person's token; Reitti's answers are told
// apart: a yes with its count, a refused token, Reitti's own no, no answer.
func TestUebernehmenSendsReittisGeoJSONImport(t *testing.T) {
	var got struct {
		auth, file string
		body       map[string]any
	}
	answer := func(w http.ResponseWriter) { io.WriteString(w, `{"success":true,"pointsScheduled":2,"message":"ok"}`) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != ImportPath {
			http.NotFound(w, r)
			return
		}
		got.auth = r.Header.Get("Authorization")
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Errorf("no file part: %v", err)
			return
		}
		got.file = h.Filename
		json.NewDecoder(f).Decode(&got.body)
		answer(w)
	}))
	defer srv.Close()
	r := &Reitti{URL: srv.URL, HTTP: srv.Client()}
	h := 512.0
	ps := []Punkt{
		{Zeit: time.Date(2014, 3, 2, 8, 0, 0, 500, time.UTC), Breite: 47, Laenge: 8, Genauigkeit: 12, Hoehe: &h},
		{Zeit: time.Date(2024, 12, 30, 17, 0, 0, 0, time.UTC), Breite: 47.5, Laenge: 8.5, Genauigkeit: 10},
	}
	var f *ImportFehler
	n, err := r.Uebernehmen(t.Context(), "anna-token", "Timeline.json", ps)
	if err != nil || n != 2 {
		t.Fatalf("%d, %v", n, err)
	}
	if got.auth != "Bearer anna-token" || got.file != "Timeline.json.geojson" {
		t.Fatalf("auth %q, file %q", got.auth, got.file)
	}
	b, _ := json.Marshal(got.body)
	want := `{"features":[{"geometry":{"coordinates":[8,47,512],"type":"Point"},"properties":{"accuracy":12,"timestamp":"2014-03-02T08:00:00Z"},"type":"Feature"},` +
		`{"geometry":{"coordinates":[8.5,47.5],"type":"Point"},"properties":{"accuracy":10,"timestamp":"2024-12-30T17:00:00Z"},"type":"Feature"}],"type":"FeatureCollection"}`
	if string(b) != want {
		t.Fatalf("body\n%s\nwant\n%s", b, want)
	}

	// Reitti's real yes (v5.9.0, main): its controller asks for "pointsImported", its importer
	// says "pointsReceived", so pointsScheduled is null — a yes for what was sent (the box,
	// 2026-11-04: Reitti queued the first batch and Homes stopped)
	answer = func(w http.ResponseWriter) {
		io.WriteString(w, `{"success":true,"message":"Successfully imported GeoJSON file","pointsScheduled":null}`)
	}
	if n, err := r.Uebernehmen(t.Context(), "anna-token", "a", ps); err != nil || n != 2 {
		t.Errorf("a yes with pointsScheduled null: %d, %v", n, err)
	}
	answer = func(w http.ResponseWriter) { io.WriteString(w, `{"success":false}`) }
	if _, err := r.Uebernehmen(t.Context(), "x", "a", ps); !errors.As(err, &f) {
		t.Errorf("a 200 that is no yes: %v", err)
	}
	answer = func(w http.ResponseWriter) { w.WriteHeader(401) }
	if _, err := r.Uebernehmen(t.Context(), "x", "a", ps); !errors.Is(err, ErrToken) {
		t.Errorf("401: %v", err)
	}
	answer = func(w http.ResponseWriter) {
		w.WriteHeader(400)
		io.WriteString(w, `{"error":"Token has no device attached."}`)
	}
	if _, err := r.Uebernehmen(t.Context(), "x", "a", ps); !errors.As(err, &f) || !strings.Contains(f.Grund, "no device") {
		t.Errorf("Reitti's no: %v", err)
	}
	answer = func(w http.ResponseWriter) { io.WriteString(w, `{"success":false,"error":"Invalid GeoJSON"}`) }
	if _, err := r.Uebernehmen(t.Context(), "x", "a", ps); !errors.As(err, &f) {
		t.Errorf("a 200 with success false: %v", err)
	}
	answer = func(w http.ResponseWriter) { w.WriteHeader(502) }
	var u *Unavailable
	if _, err := r.Uebernehmen(t.Context(), "x", "a", ps); !errors.As(err, &u) || u.Status != 502 {
		t.Errorf("502: %v", err)
	}
	answer = func(w http.ResponseWriter) { http.Redirect(w, &http.Request{}, "/login", http.StatusFound) }
	if _, err := r.Uebernehmen(t.Context(), "x", "a", ps); !errors.As(err, &u) {
		t.Errorf("a redirect to the login: %v", err)
	}
}
