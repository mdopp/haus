package pages

import (
	"net/http"
	"strconv"

	"github.com/mdopp/haus/internal/cards"
)

// Dokument is a document's page (haus#28), the page a documents widget row
// opens: Paperless's own, behind its own login. Haus only sends the tap on;
// without Paperless's public address there is nowhere to send it.
func Dokument(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Das Dokument") {
			return
		}
		if cfg.Docs == nil {
			toStart(w, r)
			return
		}
		id, err := strconv.Atoi(itemRef(r, cards.DocumentPages))
		if err != nil || id <= 0 {
			notThere(w, r, cfg, http.StatusNotFound, "Dieses Dokument gibt es nicht.", "Dokument", "/start", "Start")
			return
		}
		to := cfg.Docs.Page(id)
		if to == "" {
			toStart(w, r)
			return
		}
		http.Redirect(w, r, to, http.StatusFound)
	}
}
