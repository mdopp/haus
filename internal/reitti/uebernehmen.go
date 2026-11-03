package reitti

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// ImportPath is Reitti's GeoJSON import (controller/api/GeoJsonApiController
// at v5.9.0), the one file import Reitti takes with a token: its
// Google, GPX-from-the-page and FIT imports sit under /settings/import and
// want the logged-in session (SecurityConfig; the token filter reads only
// /api/). /api/v1/gpx/import takes a token too, but a GPX file is read here
// like the others, so the person hears one count and one time span.
//
// Reitti stages what it gets and files it into the timeline after its
// grace time (reitti.import.grace-time-seconds, 300 s): the visits and
// trips appear a few minutes after the import. A point Reitti has already
// (same device, same second) is left as it is, so an import that is sent
// again adds nothing twice.
const ImportPath = "/api/v1/geojson/import"

// ImportFehler is Reitti answering an import with no yes and its reason.
type ImportFehler struct {
	Status int
	Grund  string
}

func (e *ImportFehler) Error() string { return "reitti: import answered " + e.Grund }

// importClient waits for Reitti to stage one batch: tens of thousands of
// points go into its database before it answers.
var importClient = &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// Uebernehmen hands ps to Reitti's import with the person's token, as a
// GeoJSON file called name (Reitti shows it in its job list): how many
// points Reitti scheduled, ErrToken when it refused the token, *ImportFehler
// when it said no, *Unavailable when it did not answer.
func (r *Reitti) Uebernehmen(ctx context.Context, token, name string, ps []Punkt) (int, error) {
	body, ctype := importBody(name, ps)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL+ImportPath, body)
	if err != nil {
		return 0, &Unavailable{Err: err}
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+token)
	c := r.HTTP
	if c == nil {
		c = importClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, &Unavailable{Err: err}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return 0, ErrToken
	}
	var ans struct {
		Success   bool   `json:"success"`
		Scheduled *int   `json:"pointsScheduled"`
		Error     string `json:"error"`
	}
	jerr := json.Unmarshal(b, &ans)
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300 && jerr == nil && ans.Success && ans.Scheduled != nil:
		return *ans.Scheduled, nil
	case jerr == nil && ans.Error != "": // Reitti's own no: a token with no device, a file it could not read
		return 0, &ImportFehler{Status: resp.StatusCode, Grund: ans.Error}
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return 0, &ImportFehler{Status: resp.StatusCode, Grund: "a yes without pointsScheduled"}
	}
	return 0, &Unavailable{Status: resp.StatusCode}
}

// importBody is ps as the multipart form Reitti's import reads: one part
// "file", a FeatureCollection of Points with timestamp and accuracy.
func importBody(name string, ps []Punkt) (io.Reader, string) {
	type props struct {
		Timestamp string  `json:"timestamp"`
		Accuracy  float64 `json:"accuracy"`
	}
	type feat struct {
		Type     string `json:"type"`
		Geometry struct {
			Type        string    `json:"type"`
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties props `json:"properties"`
	}
	fc := struct {
		Type     string `json:"type"`
		Features []feat `json:"features"`
	}{Type: "FeatureCollection", Features: make([]feat, 0, len(ps))}
	for _, p := range ps {
		f := feat{Type: "Feature", Properties: props{Timestamp: p.Zeit.UTC().Format(time.RFC3339), Accuracy: p.Genauigkeit}}
		f.Geometry.Type = "Point"
		f.Geometry.Coordinates = []float64{p.Laenge, p.Breite}
		if p.Hoehe != nil {
			f.Geometry.Coordinates = append(f.Geometry.Coordinates, *p.Hoehe)
		}
		fc.Features = append(fc.Features, f)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", importName(name))
	json.NewEncoder(fw).Encode(fc)
	mw.Close()
	return &buf, mw.FormDataContentType()
}

// importName is name as a file Reitti takes: it must end in .geojson.
func importName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < ' ' || strings.ContainsRune(`"\/`, r) {
			return '_'
		}
		return r
	}, name)
	if name == "" {
		name = "homes"
	}
	return strings.TrimSuffix(name, ".geojson") + ".geojson"
}
