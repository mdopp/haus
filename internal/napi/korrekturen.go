package napi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/mdopp/homes/internal/kontaktantwort"
)

// Kontakt prüfen lassen (owner 2026-11-07): the corrections of a person's
// own Handy-Kontakte that they accepted on Homes' review wait here for
// their phone. The app fetches them, writes each into the phone's contact
// whose lookup key is the UID „Kontakte übertragen“ sent, and says which
// it wrote. Only accepted changes are ever served, and only the device's
// person's (Whoami), never the request's.

const saysKeineKorrektur = "Diese Korrektur wartet nicht mehr auf das Handy."

type korrekturen struct {
	store  *kontaktantwort.Store
	whoami func(string) (string, bool)
}

// korrekturAenderung is one accepted change as the app reads it; nr is its
// place in the answer, the number the app sends back.
type korrekturAenderung struct {
	Nr int `json:"nr"`
	kontaktantwort.Aenderung
}

type korrektur struct {
	ID          string               `json:"id"`
	UID         string               `json:"uid"`
	Name        string               `json:"name"`
	Aenderungen []korrekturAenderung `json:"aenderungen"`
}

func (k *korrekturen) person(w http.ResponseWriter, r *http.Request) (string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	person := ""
	if bearer, ok := bearerOf(r); ok && k.whoami != nil {
		person, _ = k.whoami(bearer)
	}
	if !validPerson(person) {
		fehler(w, http.StatusForbidden, saysNoPerson)
		return "", false
	}
	return person, true
}

// GET /napi/kontakte/korrekturen → {korrekturen: [{id, uid, name, aenderungen: [{nr, art, vorher, nachher, …}]}]}
func (k *korrekturen) list(w http.ResponseWriter, r *http.Request) {
	person, ok := k.person(w, r)
	if !ok {
		return
	}
	out := []korrektur{}
	for _, f := range k.store.FuersHandy(person) {
		kr := korrektur{ID: f.ID, UID: f.UID, Name: f.Name()}
		for i, a := range f.Aenderungen {
			if a.Stand == kontaktantwort.Angenommen {
				a.Hinweise, a.Stand = nil, ""
				kr.Aenderungen = append(kr.Aenderungen, korrekturAenderung{Nr: i, Aenderung: a})
			}
		}
		out = append(out, kr)
	}
	answer(w, http.StatusOK, map[string]any{"korrekturen": out})
}

// POST /napi/kontakte/korrekturen/{id}, {geschrieben: [nr…]}: the phone
// wrote these; every other accepted change of it it could not.
func (k *korrekturen) done(w http.ResponseWriter, r *http.Request) {
	person, ok := k.person(w, r)
	if !ok {
		return
	}
	var body struct {
		Geschrieben []int `json:"geschrieben"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		fehler(w, http.StatusBadRequest, "Die Antwort des Handys war nicht lesbar. Aktualisiere die Homes-App.")
		return
	}
	f, err := k.store.VomHandy(r.PathValue("id"), person, body.Geschrieben)
	if errors.Is(err, kontaktantwort.ErrWeg) {
		fehler(w, http.StatusNotFound, saysKeineKorrektur)
		return
	}
	if err != nil {
		log.Printf("homes: korrektur %s: not kept: %v", r.PathValue("id"), err)
		fehler(w, http.StatusInternalServerError, "Die Box konnte sich das nicht merken. Die App versucht es beim nächsten Mal wieder.")
		return
	}
	log.Printf("homes: korrektur %s: the phone wrote %d", f.ID, len(body.Geschrieben))
	answer(w, http.StatusOK, map[string]bool{"ok": true})
}

func (k *korrekturen) routes(gs gruppen) {
	if k.store == nil {
		return
	}
	gs.add("kontakte", "GET /napi/kontakte/korrekturen", k.list)
	gs.add("kontakte", "POST /napi/kontakte/korrekturen/{id}", k.done)
}
