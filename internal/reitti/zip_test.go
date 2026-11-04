package reitti

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipOf is a ZIP of name → content, a name of testdata/<file> taken from there.
func zipOf(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, content := range files {
		if from, ok := strings.CutPrefix(content, "testdata/"); ok {
			raw, err := os.ReadFile(filepath.Join("testdata", from))
			if err != nil {
				t.Fatal(err)
			}
			content = string(raw)
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	zw.Close()
	return bytes.NewReader(b.Bytes())
}

func liesDatei(r *bytes.Reader) (Format, int, []Fund, error) {
	var out []Fund
	var gelesen int64
	format, n, err := LesenDatei(r, r.Size(), Roh{}, func(g int64) { gelesen = g }, func(fd Fund) error { out = append(out, fd); return nil })
	if err == nil && gelesen == 0 {
		err = errors.New("no progress said")
	}
	return format, n, out, err
}

// The Semantic Location History of an older Takeout: a visit is two points, a way its start,
// its raw path (or its waypoints spread over its time) and its end; a visit without a place
// is passed over.
func TestLesenSemantic(t *testing.T) {
	format, got, err := lies(t, "semantic-2014-03.json")
	if err != nil || format != FormatSemantic {
		t.Fatalf("%s %v", format, err)
	}
	want := []struct {
		zeit   string
		lat    float64
		besuch bool
	}{
		{"2014-03-02T06:00:00Z", 47, true}, {"2014-03-02T07:30:00Z", 47, true},
		{"2014-03-02T07:00:00Z", 47, false}, {"2014-03-02T07:30:00Z", 47.005, false}, {"2014-03-02T08:00:00Z", 47.01, false},
		{"2014-03-02T12:00:00Z", 47.01, false}, {"2014-03-02T12:20:00Z", 47.012, false}, {"2014-03-02T13:00:00Z", 47.02, false},
	}
	if len(got) != len(want) {
		t.Fatalf("%d points: %+v", len(got), got)
	}
	for i, w := range want {
		if !got[i].Zeit.Equal(at(w.zeit)) || abs(got[i].Breite-w.lat) > 1e-9 || got[i].Besuch != w.besuch || got[i].Genauigkeit != genauGoogle {
			t.Errorf("%d: %+v, want %+v", i, got[i], w)
		}
	}
}

// owner 2026-11-04: a Google export as a ZIP. Every file Homes knows, in whatever folder and
// under whatever language's folder name; where Records.json and the Semantic Location History
// cover the same hours, Records' points; files it does not know are passed over.
func TestLesenDateiZIP(t *testing.T) {
	r := zipOf(t, map[string]string{
		"Takeout/Standortverlauf (Zeitachse)/Records.json":                                         "testdata/records.json",
		"Takeout/Historial de ubicaciones/Semantic Location History/2014/2014_MARCH.json":          "testdata/semantic-2014-03.json",
		"Takeout/Standortverlauf (Zeitachse)/Settings.json":                                        `{"deviceSettings": []}`,
		"Takeout/archive_browser.html":                                                             "<html></html>",
		"__MACOSX/Takeout/._Records.json":                                                          "junk",
		"tief/drinnen/noch/tiefer/spur.gpx":                                                        "testdata/spur.gpx",
		"Takeout/Standortverlauf (Zeitachse)/Semantic Location History/2014/2014_MARZ/kaputt.json": `{"timelineObjects": [`,
	})
	format, n, got, err := liesDatei(r)
	if err != nil || format != FormatZIP || n != 3 {
		t.Fatalf("%s %d %v", format, n, err)
	}
	stunden := map[int64]bool{}
	for _, fd := range got {
		stunden[fd.Zeit.Unix()/3600] = true
	}
	// Records has 08:00 and 09:00 on 2 March 2014: the Semantic file's 08:00 end of a way is
	// not taken again; its 06:00, 07:00, 12:00 and 13:00 are, being Records' gaps
	var semantic, um8 int
	for _, fd := range got {
		if fd.Zeit.Equal(at("2014-03-02T08:00:00Z")) {
			um8++
		}
		if fd.Zeit.Year() == 2014 && fd.Zeit.Hour() >= 12 && fd.Zeit.Hour() <= 13 {
			semantic++
		}
	}
	if um8 != 1 || semantic != 3 {
		t.Errorf("08:00 %d times, 12–13 h %d points: %+v", um8, semantic, got)
	}
	if !stunden[at("2014-03-02T06:00:00Z").Unix()/3600] {
		t.Error("the Semantic visit before Records' first point is missing")
	}
}

func TestLesenDateiZIPOhneVerlauf(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		err   error
	}{
		"leer":   {map[string]string{}, ErrZIPLeer},
		"fremde": {map[string]string{"Fotos/bild.json": `{"title": "bild.jpg"}`, "index.html": "<html></html>", "a.gpx": "<kml></kml>", "b.json": `[1, 2]`}, ErrZIPLeer},
		"teil":   {map[string]string{"Takeout/Google Fotos/bild.json": `{"title": "bild.jpg"}`, "Takeout/archive_browser.html": "<html></html>"}, ErrTakeoutTeil},
	} {
		if format, n, _, err := liesDatei(zipOf(t, c.files)); !errors.Is(err, c.err) || format != FormatZIP || n != 0 {
			t.Errorf("%s: %s %d %v", name, format, n, err)
		}
	}
	// a file that is no ZIP is read as it is
	raw, _ := os.ReadFile(filepath.Join("testdata", "records.json"))
	if format, n, got, err := liesDatei(bytes.NewReader(raw)); err != nil || format != FormatRecords || n != 1 || len(got) != 3 {
		t.Errorf("plain: %s %d %d %v", format, n, len(got), err)
	}
	// emit's own error stops it and comes back as it is
	stop := errors.New("reitti ist weg")
	r := zipOf(t, map[string]string{"a/Records.json": "testdata/records.json"})
	if _, _, err := LesenDatei(r, r.Size(), Roh{}, func(int64) {}, func(Fund) error { return stop }); err != stop {
		t.Errorf("%v", err)
	}
}

// A Takeout in two parts is two imports: what the first part's raw points cover, the second
// part's Semantic files do not bring again; a second part's raw points always go.
func TestLesenDateiTeile(t *testing.T) {
	roh := Roh{}
	lies := func(files map[string]string) int {
		r := zipOf(t, files)
		var n int
		if _, _, err := LesenDatei(r, r.Size(), roh, func(int64) {}, func(Fund) error { n++; return nil }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := lies(map[string]string{"Takeout/Standortverlauf/Records.json": "testdata/records.json"}); n != 3 {
		t.Fatalf("part 1: %d", n)
	}
	if n := lies(map[string]string{"Takeout/Standortverlauf/Semantic Location History/2014/2014_MARCH.json": "testdata/semantic-2014-03.json"}); n != 7 {
		t.Errorf("part 2: %d points, the 08:00 Records has is not taken again", n)
	}
	if n := lies(map[string]string{"Takeout/Standortverlauf/Records.json": "testdata/records.json"}); n != 3 {
		t.Errorf("raw points go again (Reitti keeps each once): %d", n)
	}
}
