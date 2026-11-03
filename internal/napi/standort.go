package napi

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/mdopp/homes/internal/reitti"
)

// Standortverlauf (owner 2026-11-02): the Homes app records where its
// person was and sends the points here in batches; Homes passes them on,
// inside the box, to Reitti's Overland ingest with that person's own
// device token (package reitti). The person is the device's (Whoami),
// never the request's, and so is the token.
//
// No point is lost silently: Homes answers 200 only once Reitti said yes.
// Without a token (409, grund kein_schluessel), with a token Reitti
// refuses (409, schluessel_abgelehnt), or with Reitti not taking them now
// (503, reitti_weg), the app keeps the batch and sends it again. A point
// Homes cannot read is not passed on; the reply counts it (verworfen).

var (
	standortMaxBody   int64 = 2 << 20 // one batch
	standortMaxPunkte       = 1000    // points in one batch
)

const (
	saysNoReitti        = "Der Standortverlauf (Reitti) ist auf dieser Box nicht eingerichtet."
	saysReittiNoToken   = "Verbinde zuerst Reitti: füge in Homes unter Mehr → Standortverlauf einmal deinen Reitti-Schlüssel ein."
	saysReittiRefused   = "Reitti kennt deinen Schlüssel nicht mehr: füge in Homes unter Mehr → Standortverlauf einen neuen ein."
	saysReittiDown      = "Reitti nimmt gerade nichts an. Die Homes-App behält die Punkte und schickt sie später noch einmal."
	saysStandortBroken  = "Die Punkte waren nicht lesbar. Aktualisiere die Homes-App."
	saysStandortTooMany = "Zu viele Punkte auf einmal: höchstens 1000 je Anfrage. Aktualisiere die Homes-App."
)

// What the app reads in a refusal besides the sentence: why, as a word.
const (
	grundNichtEingerichtet = "nicht_eingerichtet"
	grundKeinSchluessel    = "kein_schluessel"
	grundAbgelehnt         = "schluessel_abgelehnt"
	grundReittiWeg         = "reitti_weg"
)

// punkt is one point as the app sends it.
type punkt struct {
	Zeit        string   `json:"zeit"`        // RFC 3339
	Breite      *float64 `json:"breite"`      // latitude
	Laenge      *float64 `json:"laenge"`      // longitude
	Genauigkeit *float64 `json:"genauigkeit"` // metres
	Hoehe       *float64 `json:"hoehe"`       // metres, optional
	Tempo       *float64 `json:"tempo"`       // m/s, optional
}

func finite(f *float64) bool { return f != nil && !math.IsNaN(*f) && !math.IsInf(*f, 0) }

// lesbar is p as Reitti takes it, or false: a time, a place on earth and an accuracy.
func (p punkt) lesbar() (reitti.Punkt, bool) {
	t, err := time.Parse(time.RFC3339Nano, p.Zeit)
	if err != nil || !finite(p.Breite) || !finite(p.Laenge) || !finite(p.Genauigkeit) ||
		*p.Breite < -90 || *p.Breite > 90 || *p.Laenge < -180 || *p.Laenge > 180 || *p.Genauigkeit < 0 {
		return reitti.Punkt{}, false
	}
	out := reitti.Punkt{Zeit: t, Breite: *p.Breite, Laenge: *p.Laenge, Genauigkeit: *p.Genauigkeit}
	if finite(p.Hoehe) {
		out.Hoehe = p.Hoehe
	}
	if finite(p.Tempo) && *p.Tempo >= 0 {
		out.Tempo = p.Tempo
	}
	return out, true
}

// standort is the relay; r nil: the box has no Reitti.
type standort struct {
	r      *reitti.Reitti
	whoami func(string) (string, bool)
}

func (s *standort) routes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /napi/standort", auth(s.person(s.get)))
	mux.HandleFunc("POST /napi/standort", auth(s.person(s.post)))
}

func standortFehler(w http.ResponseWriter, code int, grund, say string) {
	answer(w, code, map[string]any{"fehler": say, "grund": grund})
}

// person hands a route the device's person: 403 for a device of nobody,
// 503 while the box has no Reitti.
func (s *standort) person(next personHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		person := ""
		if bearer, ok := bearerOf(r); ok && s.whoami != nil {
			person, _ = s.whoami(bearer)
		}
		if !validPerson(person) {
			fehler(w, http.StatusForbidden, saysNoPerson)
			return
		}
		if s.r == nil {
			standortFehler(w, http.StatusServiceUnavailable, grundNichtEingerichtet, saysNoReitti)
			return
		}
		next(w, r, person)
	}
}

// GET /napi/standort → {verbunden, seit}: whether the person connected
// Reitti, and since when (RFC 3339; absent when not). The token never.
func (s *standort) get(w http.ResponseWriter, r *http.Request, person string) {
	k, ok := s.r.Konto(person)
	out := map[string]any{"verbunden": ok}
	if ok {
		out["seit"] = k.Seit.UTC().Format(time.RFC3339)
	}
	answer(w, http.StatusOK, out)
}

// POST /napi/standort {punkte: [{zeit, breite, laenge, genauigkeit, hoehe, tempo}]}
// → {angenommen, verworfen} once Reitti took them.
func (s *standort) post(w http.ResponseWriter, r *http.Request, person string) {
	var body struct {
		Punkte []punkt `json:"punkte"`
	}
	if r.ContentLength > standortMaxBody {
		fehler(w, http.StatusRequestEntityTooLarge, saysStandortTooMany)
		return
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, standortMaxBody)).Decode(&body); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			fehler(w, http.StatusRequestEntityTooLarge, saysStandortTooMany)
			return
		}
		fehler(w, http.StatusBadRequest, saysStandortBroken)
		return
	}
	if len(body.Punkte) > standortMaxPunkte {
		fehler(w, http.StatusRequestEntityTooLarge, saysStandortTooMany)
		return
	}
	k, ok := s.r.Konto(person)
	if !ok {
		standortFehler(w, http.StatusConflict, grundKeinSchluessel, saysReittiNoToken)
		return
	}
	ps := make([]reitti.Punkt, 0, len(body.Punkte))
	for _, p := range body.Punkte {
		if q, ok := p.lesbar(); ok {
			ps = append(ps, q)
		}
	}
	verworfen := len(body.Punkte) - len(ps)
	if len(ps) > 0 {
		err := s.r.Send(r.Context(), k.Token, ps)
		if errors.Is(err, reitti.ErrToken) {
			log.Printf("homes: standort: reitti refused %s's token; %d points stay on the phone", person, len(ps))
			standortFehler(w, http.StatusConflict, grundAbgelehnt, saysReittiRefused)
			return
		}
		if err != nil {
			log.Printf("homes: standort: %s: %d points not taken: %v", person, len(ps), err)
			w.Header().Set("Retry-After", "300")
			standortFehler(w, http.StatusServiceUnavailable, grundReittiWeg, saysReittiDown)
			return
		}
	}
	if verworfen > 0 {
		log.Printf("homes: standort: %s: %d points to reitti, %d unreadable left out", person, len(ps), verworfen)
	}
	answer(w, http.StatusOK, map[string]any{"angenommen": len(ps), "verworfen": verworfen})
}
