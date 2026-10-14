package pages

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mdopp/haus/internal/cards"
)

// Dokument is a document's page (haus#28), the page a documents widget row
// opens: Paperless's own, behind its own login. Haus only sends the tap on.
// Without Paperless's public address there is nowhere to send it, and no
// row names this page: a 404, like an id that is none.
func Dokument(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Das Dokument") {
			return
		}
		if cfg.Docs == nil {
			toStart(w, r)
			return
		}
		to := ""
		if id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, cards.DocumentPages)); err == nil && id > 0 {
			to = cfg.Docs.Page(id)
		}
		if to == "" {
			missing{http.StatusNotFound, "Dieses Dokument gibt es nicht.", "Dokument", "/start", "Start"}.write(w, r, cfg)
			return
		}
		http.Redirect(w, r, to, http.StatusFound)
	}
}
