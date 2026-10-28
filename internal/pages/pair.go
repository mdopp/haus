package pages

import (
	"html"
	"net/http"

	"github.com/mdopp/haus/internal/app"
	"github.com/mdopp/haus/internal/devices"
)

// PairDevice is where the app pairs itself (haus#6): after the person enters
// Haus's address, the app opens /pair-device in a Custom Tab, where the
// browser's Authelia session is. GET only asks; a prefetch mints nothing.
// POST mints a device for the logged-in person and redirects to the app's
// pair link with the token in the fragment, which never reaches a proxy log.
func PairDevice(store *devices.Store, family StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("Remote-User")
		if user == "" {
			http.Error(w, "kein Remote-User: diese Seite gehört hinter Authelia", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(adminHead(r, family, "Telefon koppeln").String() +
				"<h1>Dieses Telefon koppeln</h1><p>Die App bekommt einen eigenen Zugang, der " + html.EscapeString(user) + " gehört. Entfernen geht jederzeit unter Box → Geräte.</p>" +
				"<form method=post action=/pair-device data-ganz><input name=name placeholder='Name, z.B. Pixel von Anna'> <button data-laeuft='Koppelt …'>Koppeln</button></form>"))
			return
		}
		r.ParseForm()
		d, tok, err := store.Mint(r.Form.Get("name"), user)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Location set by hand: the app's intent filter matches …://pair exactly.
		w.Header().Set("Location", app.Scheme+"://pair#token="+tok+"&id="+d.ID)
		w.WriteHeader(http.StatusFound)
	}
}
