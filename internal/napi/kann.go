package napi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/texte"
)

// kannWait is how long /napi/kann waits for Laib's wishes; after it, the
// last known stand (or none) answers.
var kannWait = 3 * time.Second

// wishWait is how long a wish may take to be filed.
var wishWait = 20 * time.Second

// kann is GET /napi/kann (ADR 0001 §2, §10): what this box can do, for
// the app, behind the paired device's bearer like every /napi path. The
// answer carries an ETag; the same one in If-None-Match is a 304. Without
// a list (a test's Config) it is a path Homes does not serve.
func (c Config) kann(w http.ResponseWriter, r *http.Request) {
	if c.Module == nil {
		noSuchPath(w, r)
		return
	}
	body, err := json.Marshal(c.kannMit(c.wuensche(r.Context())))
	if err != nil {
		fail(w, err)
		return
	}
	sum := sha256.Sum256(body)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(append(body, '\n'))
}

// kannMit is the list with the wishes, each trace in German beside its key.
func (c Config) kannMit(ws modul.Wuensche) modul.Kann {
	k := c.Module.Kann(ws)
	for i, e := range k.Module {
		if e.SpurID != "" {
			k.Module[i].Spur = texte.T(texte.ID("spur." + e.SpurID))
		}
	}
	return k
}

// wuensche are the modules' wishes as Laib knows them (laib#1038), within
// kannWait; an older Laib takes none, and then no trace offers a wish.
func (c Config) wuensche(ctx context.Context) modul.Wuensche {
	if c.Laib == nil {
		return modul.Wuensche{}
	}
	ctx, cancel := context.WithTimeout(ctx, kannWait)
	defer cancel()
	if geht, _ := c.Laib.NimmtWuensche(ctx); !geht {
		return modul.Wuensche{}
	}
	je, err := c.Laib.Wuensche(ctx, c.Module.Frage())
	if err != nil {
		log.Printf("homes: kann: the wishes from Laib: %v", err)
	}
	return laib.ModulWuensche(je, true)
}

// anfordern is POST /napi/kann/anfordern {"modul": …} (ADR 0001 §10): a
// wish for an off module, for the device's person, filed with Laib; the
// answer is the module's entry with its new state. A wish already open
// files nothing and answers it.
func (c Config) anfordern(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Modul string `json:"modul"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in)
	var m modul.Modul
	ok := c.Module != nil
	if ok {
		m, ok = c.Module.Eins(in.Modul)
	}
	if !ok || m.An() || m.Abgeschaltet() { // on, or switched off on purpose: nothing to wish for
		answer(w, http.StatusNotFound, map[string]any{"ok": false, "reason": "not_found", "error": texte.T(texte.AnfordernGibtsNicht)})
		return
	}
	ws := c.wuensche(r.Context())
	switch {
	case !ws.Geht:
		answer(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": texte.T(texte.AnfordernKeinLaib)})
		return
	case m.Anfordern(ws):
		bearer, _ := bearerOf(r)
		uid, _ := c.Whoami(bearer)
		ctx, cancel := context.WithTimeout(r.Context(), wishWait)
		_, err := c.Laib.Wuenschen(ctx, m.Name, m.Titel, uid)
		cancel()
		if err != nil {
			log.Printf("homes: anfordern %s for %s: %v", m.Name, uid, err)
			say, code := texte.AnfordernNicht, http.StatusBadGateway
			if errors.Is(err, context.DeadlineExceeded) {
				say, code = texte.AnfordernStumm, http.StatusGatewayTimeout
			}
			answer(w, code, map[string]any{"ok": false, "error": texte.T(say)})
			return
		}
		log.Printf("homes: anfordern %s for %s", m.Name, uid)
		ws = c.wuensche(r.Context())
	}
	for _, e := range c.kannMit(ws).Module {
		if e.Name == m.Name {
			answer(w, http.StatusOK, e)
		}
	}
}
