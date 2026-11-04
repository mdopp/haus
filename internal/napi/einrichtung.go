package napi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/mdopp/homes/internal/texte"
)

// einrichtung is GET /napi/einrichtung, the device's person's modules with
// their setup tasks (internal/einrichten, docs/REFERENCE.md), and POST
// /napi/einrichtung/apps {"pakete": […]}, the Android packages the phone
// has: each open task whose app is among them is ticked, and the answer is
// the GET's. Without the tasks (a test's Config) both are a path Homes
// does not serve.
func (c Config) einrichtung(w http.ResponseWriter, r *http.Request) {
	bearer, _ := bearerOf(r)
	uid, ok := "", c.Whoami != nil
	if ok {
		uid, ok = c.Whoami(bearer)
	}
	if c.Einrichtung == nil || !ok {
		noSuchPath(w, r)
		return
	}
	if r.Method == http.MethodPost {
		var in struct {
			Pakete []string `json:"pakete"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
			answer(w, http.StatusBadRequest, map[string]any{"ok": false, "reason": "bad_request"})
			return
		}
		if n, err := c.Einrichtung.Installiert(uid, in.Pakete); err != nil {
			log.Printf("homes: einrichtung: the apps of %s: %v", uid, err)
			answer(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": texte.T(texte.HakenNicht)})
			return
		} else if n > 0 {
			log.Printf("homes: einrichtung: %d of %s's tasks done, their apps are on the phone", n, uid)
		}
	}
	writeJSON(w, c.Einrichtung.Napi(uid))
}
