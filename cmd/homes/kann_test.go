package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/modul"
)

// kannSite is fullSite with its module list, as main builds it.
func kannSite(t *testing.T) site {
	t.Helper()
	s, _, _ := fullSite(t)
	l, err := modul.Bauen(katalog(s))
	if err != nil {
		t.Fatal(err)
	}
	s.napi.Module = l
	s.napi.Einrichtung = einrichtung(s, nil, nil)
	return s
}

// TestKannAnswersTheGolden: the shape of ADR 0001 §2 for fullSite, every
// list present ([] rather than null). UPDATE_GOLDEN=1 writes the file.
func TestKannAnswersTheGolden(t *testing.T) {
	h := routes(kannSite(t))
	req := httptest.NewRequest("GET", "/napi/kann", nil)
	req.Header.Set("Authorization", "Bearer dev1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("ETag") == "" {
		t.Fatalf("GET /napi/kann → %d, ETag %q: %s", rec.Code, rec.Header().Get("ETag"), rec.Body)
	}
	const golden = "testdata/kann_fullsite.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.MkdirAll("testdata", 0o755)
		os.WriteFile(golden, rec.Body.Bytes(), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("GET /napi/kann differs from %s:\n got %s\nwant %s", golden, rec.Body, want)
	}
	if strings.Contains(rec.Body.String(), "null") {
		t.Errorf("a list is null: %s", rec.Body)
	}
	// the same ETag again: 304, no body
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	again := httptest.NewRecorder()
	h.ServeHTTP(again, req)
	if again.Code != 304 || again.Body.Len() != 0 {
		t.Errorf("If-None-Match → %d %q, want 304 and nothing", again.Code, again.Body)
	}
	req.Header.Set("If-None-Match", `"anders"`)
	other := httptest.NewRecorder()
	h.ServeHTTP(other, req)
	if other.Code != 200 {
		t.Errorf("another ETag → %d, want 200", other.Code)
	}
}

func TestKannNeedsADevice(t *testing.T) {
	h := routes(kannSite(t))
	for _, bearer := range []string{"", "Bearer fremd"} {
		req := httptest.NewRequest("GET", "/napi/kann", nil)
		if bearer != "" {
			req.Header.Set("Authorization", bearer)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Errorf("%q → %d, want 401", bearer, rec.Code)
		}
	}
}

// TestKannWidgetsAreTheToolDefs: app.widgets' tool:<id> keys are
// /napi/defs/tool's ids, because both come from the same backends.
func TestKannWidgetsAreTheToolDefs(t *testing.T) {
	for name, s := range map[string]site{"full": kannSite(t), "no documents, no notes": func() site {
		s := kannSite(t)
		s.hh.Documents, s.hh.Notes = nil, nil
		s.napi.Haushalt = s.hh
		s.napi.Module, _ = modul.Bauen(katalog(s))
		return s
	}()} {
		h := routes(s)
		var tools []string
		for _, w := range s.napi.Module.Kann(modul.Wuensche{}).App.Widgets {
			if id, ok := strings.CutPrefix(w, "tool:"); ok {
				tools = append(tools, id)
			}
		}
		var defs []string
		for _, d := range napiGet(t, h, "/napi/defs/tool")["defs"].([]any) {
			defs = append(defs, d.(map[string]any)["tool-id"].(string))
		}
		slices.Sort(tools)
		slices.Sort(defs)
		if !slices.Equal(tools, defs) || len(defs) == 0 {
			t.Errorf("%s: kann's tool widgets %v, /napi/defs/tool %v", name, tools, defs)
		}
	}
}
