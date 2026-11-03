// Package reitti is Homes' way into Reitti, the household's own timeline
// (reitti.<domain>, a Laib service behind Authelia): the Homes app records
// where a person was, sends the points to Homes (napi/standort.go), and Homes
// passes them on inside the box to Reitti's Overland ingest, with that
// person's own device token. The phone never needs a route to Reitti, and
// Authelia is never bypassed from outside.
//
// Each person pastes their Reitti token once on Homes' page Standortverlauf
// (pages/standort.go). Homes keeps it in HOMES_DATA/reitti-konten.json (0600),
// for that person alone; it is never logged and never shown back.
package reitti

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// IngestPath is Reitti's Overland endpoint (controller/api/ingestion/overland).
const IngestPath = "/api/v1/ingest/overland"

// Punkt is one recorded position: when, where, and how sure.
type Punkt struct {
	Zeit        time.Time
	Breite      float64  // latitude, degrees
	Laenge      float64  // longitude, degrees
	Genauigkeit float64  // horizontal accuracy, metres; Reitti drops a point without it
	Hoehe       *float64 // altitude, metres; nil: unknown
	Tempo       *float64 // speed, m/s; nil: unknown
}

// ErrToken is Reitti refusing the person's token (401/403): it was
// deleted there, or is not Reitti's at all. Sending again does not help;
// the person pastes a new one.
var ErrToken = errors.New("reitti: the token was refused")

// Unavailable is Reitti not taking the points now: not answering, or an
// answer that is not a yes (Reitti answers 500 too for a token with no
// device attached). The points stay on the phone and come again.
type Unavailable struct {
	Status int // 0: no answer at all
	Err    error
}

func (u *Unavailable) Error() string {
	if u.Status == 0 {
		return fmt.Sprintf("reitti: no answer: %v", u.Err)
	}
	return fmt.Sprintf("reitti: answered %d", u.Status)
}

func (u *Unavailable) Unwrap() error { return u.Err }

// Konto is what Homes keeps of a person's Reitti connection.
type Konto struct {
	Token string    `json:"token"`
	Seit  time.Time `json:"seit"`
}

// Reitti is the household's Reitti as Homes reaches it inside the box, and
// every person's token.
type Reitti struct {
	URL       string       // HOMES_REITTI_URL, e.g. http://host.containers.internal:<port>
	PublicURL string       // HOMES_REITTI_PUBLIC_URL, https://reitti.<domain>: where a person makes their token; "": no link
	Dir       string       // HOMES_DATA: reitti-konten.json
	HTTP      *http.Client // nil: a client with a 20 s timeout
	Now       func() time.Time

	mu sync.Mutex
}

// FromEnv is Reitti when HOMES_REITTI_URL names it, else nil: Homes then
// answers that the box has no timeline.
func FromEnv(dataDir string) *Reitti {
	u := strings.TrimRight(strings.TrimSpace(os.Getenv("HOMES_REITTI_URL")), "/")
	if u == "" {
		return nil
	}
	return &Reitti{URL: u, PublicURL: strings.TrimSpace(os.Getenv("HOMES_REITTI_PUBLIC_URL")), Dir: dataDir}
}

const kontenFile = "reitti-konten.json"

func (r *Reitti) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reitti) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	// a redirect is no yes (a login page would answer 200)
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (r *Reitti) load() (map[string]Konto, error) {
	out := map[string]Konto{}
	b, err := os.ReadFile(filepath.Join(r.Dir, kontenFile))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", kontenFile, err)
	}
	return out, nil
}

func (r *Reitti) save(all map[string]Konto) error {
	b, _ := json.MarshalIndent(all, "", "  ")
	p := filepath.Join(r.Dir, kontenFile)
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// Konto is the person's connection, if they made one.
func (r *Reitti) Konto(user string) (Konto, bool) {
	if r == nil || user == "" {
		return Konto{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.load()
	if err != nil {
		return Konto{}, false
	}
	k, ok := all[user]
	return k, ok && k.Token != ""
}

// Verbinden checks token with Reitti (an empty batch: Reitti answers 200
// only for a token it knows that has a device attached) and keeps it for
// user. Nothing is kept unless Reitti said yes.
func (r *Reitti) Verbinden(ctx context.Context, user, token string) error {
	token = strings.TrimSpace(token)
	if user == "" || token == "" {
		return ErrToken
	}
	if err := r.Send(ctx, token, nil); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.load()
	if err != nil {
		return err
	}
	all[user] = Konto{Token: token, Seit: r.now()}
	return r.save(all)
}

// Trennen forgets user's token (Reitti keeps it until the person deletes it there).
func (r *Reitti) Trennen(user string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.load()
	if err != nil {
		return err
	}
	delete(all, user)
	return r.save(all)
}

// overland is the body Reitti's Overland ingest reads
// (dto/OverlandLocationRequest.java): GeoJSON points, [longitude, latitude].
type overland struct {
	Locations []feature `json:"locations"`
}

type feature struct {
	Type     string `json:"type"`
	Geometry struct {
		Type        string     `json:"type"`
		Coordinates [2]float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		Timestamp          string   `json:"timestamp"`
		HorizontalAccuracy float64  `json:"horizontal_accuracy"`
		Altitude           *float64 `json:"altitude,omitempty"`
		Speed              *float64 `json:"speed,omitempty"`
	} `json:"properties"`
}

// Body is ps as Reitti's Overland ingest reads them.
func Body(ps []Punkt) []byte {
	o := overland{Locations: make([]feature, 0, len(ps))}
	for _, p := range ps {
		var f feature
		f.Type = "Feature"
		f.Geometry.Type = "Point"
		f.Geometry.Coordinates = [2]float64{p.Laenge, p.Breite}
		f.Properties.Timestamp = p.Zeit.UTC().Format(time.RFC3339Nano)
		f.Properties.HorizontalAccuracy = p.Genauigkeit
		f.Properties.Altitude = p.Hoehe
		f.Properties.Speed = p.Tempo
		o.Locations = append(o.Locations, f)
	}
	b, _ := json.Marshal(o)
	return b
}

// Send posts ps to Reitti with token: nil once Reitti took them, ErrToken
// when it refused the token, *Unavailable otherwise.
func (r *Reitti) Send(ctx context.Context, token string, ps []Punkt) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL+IngestPath, bytes.NewReader(Body(ps)))
	if err != nil {
		return &Unavailable{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.client().Do(req)
	if err != nil {
		return &Unavailable{Err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// Reitti's yes is {"result":"ok"}; anything else took nothing for sure
		var yes struct {
			Result string `json:"result"`
		}
		if json.Unmarshal(body, &yes) != nil || yes.Result != "ok" {
			return &Unavailable{Status: resp.StatusCode, Err: errors.New("reitti: a 2xx without its result ok")}
		}
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrToken
	default:
		return &Unavailable{Status: resp.StatusCode}
	}
}
