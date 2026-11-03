package reitti

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The histories a person shares are read the way Reitti's own importers
// read them: the same points, times and default accuracies. The files in
// testdata are made up, no one's places.

func lies(t *testing.T, name string) (Format, []Fund, error) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Fund
	format, err := Lesen(f, func(fd Fund) error { out = append(out, fd); return nil })
	return format, out, err
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestLesenEachFormat(t *testing.T) {
	type want struct {
		zeit   string
		lat    float64
		lng    float64
		genau  float64
		besuch bool
	}
	cases := []struct {
		file   string
		format Format
		points []want
	}{
		{"records.json", FormatRecords, []want{
			{"2014-03-02T08:00:00Z", 47, 8, 12, false},
			{"2014-03-02T09:00:00Z", 47.001, 8.001, 10, false}, // timestampMs, Reitti's default accuracy
			// a latitude that is no number, and one without, are left out
			{"2024-12-30T17:00:00Z", 47.004, 8.004, 10, false},
		}},
		{"timeline-android.json", FormatTimeline, []want{
			{"2020-05-01T06:00:00Z", 47.1, 8.1, 10, true}, // the visit's start and end
			{"2020-05-01T07:30:00Z", 47.1, 8.1, 10, true},
			{"2020-05-01T07:35:00Z", 47.11, 8.11, 10, false},
			{"2020-05-01T07:45:00Z", 47.12, 8.12, 10, false},
			// "kein Ort" is left out; an activity and rawSignals are not points, as in Reitti
		}},
		{"timeline-ios.json", FormatTimelineIOS, []want{
			{"2021-07-10T10:00:00Z", 47.2, 8.2, 10, true},
			{"2021-07-10T11:00:00Z", 47.2, 8.2, 10, true},
			{"2021-07-10T11:10:00Z", 47.21, 8.21, 10, false}, // start + 10 minutes
			{"2021-07-10T12:10:00Z", 47.22, 8.22, 10, false},
		}},
		{"punkte.geojson", FormatGeoJSON, []want{
			{"2022-01-05T10:00:00Z", 47.3, 8.3, 7, false},
			{"2022-01-05T11:00:00Z", 47.31, 8.31, 50, false}, // epoch seconds, GeoJSON's default accuracy
		}},
		{"spur.gpx", FormatGPX, []want{
			{"2023-06-01T08:00:00Z", 47.4, 8.4, 10, false},
			{"2023-06-01T08:01:00Z", 47.401, 8.401, 120, false}, // hdop > 5
			{"2023-06-01T08:02:00Z", 47.402, 8.402, 4.5, false}, // <accuracy> in the extensions
		}},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			format, got, err := lies(t, c.file)
			if err != nil || format != c.format {
				t.Fatalf("format %q, err %v; want %q", format, err, c.format)
			}
			if len(got) != len(c.points) {
				t.Fatalf("%d points, want %d: %+v", len(got), len(c.points), got)
			}
			for i, w := range c.points {
				g := got[i]
				if !g.Zeit.Equal(at(w.zeit)) || abs(g.Breite-w.lat) > 1e-9 || abs(g.Laenge-w.lng) > 1e-9 || g.Genauigkeit != w.genau || g.Besuch != w.besuch {
					t.Errorf("point %d = %v %v,%v ±%v besuch %v; want %+v", i, g.Zeit, g.Breite, g.Laenge, g.Genauigkeit, g.Besuch, w)
				}
			}
		})
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestLesenKeepsHeightWhereThereIsOne(t *testing.T) {
	_, got, _ := lies(t, "spur.gpx")
	if got[0].Hoehe == nil || *got[0].Hoehe != 500.5 || got[1].Hoehe != nil {
		t.Fatalf("heights: %v, %v", got[0].Hoehe, got[1].Hoehe)
	}
	_, got, _ = lies(t, "punkte.geojson")
	if got[0].Hoehe == nil || *got[0].Hoehe != 410 {
		t.Fatalf("geojson height: %v", got[0].Hoehe)
	}
}

func TestLesenRefusesWhatItDoesNotKnow(t *testing.T) {
	for _, body := range []string{"", "   ", "hallo", `{"irgendwas": 1}`, `<html><body>nein</body></html>`, "\xEF\xBB\xBF"} {
		if _, err := Lesen(strings.NewReader(body), func(Fund) error { return nil }); !errors.Is(err, ErrFormat) {
			t.Errorf("%q: %v, want ErrFormat", body, err)
		}
	}
	if _, _, err := lies(t, "fremd.json"); !errors.Is(err, ErrFormat) {
		t.Errorf("fremd.json: %v", err)
	}
}

// A file cut off on the way: what came before it was handed on, and the
// error says the file broke, not that it was unknown.
func TestLesenACutFileKeepsWhatCameBefore(t *testing.T) {
	format, got, err := lies(t, "abgeschnitten.json")
	var fe *FormatError
	if !errors.As(err, &fe) || errors.Is(err, ErrFormat) || format != FormatRecords {
		t.Fatalf("format %q err %v", format, err)
	}
	if len(got) != 1 {
		t.Fatalf("%d points before the cut, want 1", len(got))
	}
}

// emit's own error stops the reading and comes back as it is.
func TestLesenStopsOnEmitsError(t *testing.T) {
	stop := errors.New("reitti weg")
	f, _ := os.Open(filepath.Join("testdata", "records.json"))
	defer f.Close()
	n := 0
	_, err := Lesen(f, func(Fund) error { n++; return stop })
	if err != stop || n != 1 {
		t.Fatalf("err %v after %d points", err, n)
	}
}

// A large value Homes does not need (an Android export's rawSignals) is
// passed over, before or after the segments.
func TestLesenPassesOverTheRest(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"rawSignals": [`)
	for i := 0; i < 20000; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"position": {"LatLng": "1°, 2°", "x": [1, {"y": "}]"}]}}`)
	}
	b.WriteString(`], "semanticSegments": [{"startTime": "2020-01-01T00:00:00Z", "endTime": "2020-01-01T01:00:00Z", "visit": {"topCandidate": {"placeLocation": {"latLng": "1.5°, 2.5°"}}}}], "tail": {"a": [[[]]]}}`)
	n := 0
	format, err := Lesen(strings.NewReader(b.String()), func(Fund) error { n++; return nil })
	if err != nil || format != FormatTimeline || n != 2 {
		t.Fatalf("%q %v %d", format, err, n)
	}
}

func TestLatLng(t *testing.T) {
	for in, want := range map[string][2]float64{
		"53.8633043°, 10.7011529°": {53.8633043, 10.7011529},
		"geo:55.605843,13.007508":  {55.605843, 13.007508},
		" -33.5°,-70.25° ":         {-33.5, -70.25},
	} {
		lat, lng, ok := LatLng(in)
		if !ok || lat != want[0] || lng != want[1] {
			t.Errorf("%q = %v %v %v", in, lat, lng, ok)
		}
	}
	for _, in := range []string{"", "geo:", "1,2,3", "91°, 0°", "0°, 181°", "a, b"} {
		if _, _, ok := LatLng(in); ok {
			t.Errorf("%q read as a place", in)
		}
	}
}
