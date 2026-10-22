package pages

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/cards"
)

// Dokument is a document's page (haus#28), the page a documents widget row
// opens: Paperless's own, behind its own login. Haus only sends the tap on.
// Without Paperless's public address there is nowhere to send it, and no
// row names this page: a 404, like an id that is none. Haus reads nothing
// from Paperless here, so there is nothing to say when it does not answer:
// Paperless's own page says that (haus#33).
func Dokument(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Das Dokument") {
			return
		}
		if r.URL.Path == cards.DocumentPages { // no id: the Dokumente page (haus#68)
			http.Redirect(w, r, DokumentePath, http.StatusFound)
			return
		}
		if mark, ok := strings.CutPrefix(r.URL.Path, FiledPages); ok {
			filed(w, r, cfg, mark)
			return
		}
		if cfg.Docs == nil {
			notSetDocs.write(w, r, cfg, "Dokumente")
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

// filedWait is how long a filed file's link waits for Paperless: a short
// look, then the newest documents rather than a page that hangs.
var filedWait = 3 * time.Second

// filed sends a filed file's link on (haus#68): Paperless has read it,
// to its page; still being read, Paperless not answering or no API to
// ask, to Paperless's newest documents, where it will appear. No
// Paperless address: nowhere to send it, a 404, as a mark that is none.
func filed(w http.ResponseWriter, r *http.Request, cfg StartConfig, mark string) {
	newest := cfg.paperless()
	if !markRE.MatchString(mark) || (!newest.Set() && cfg.Docs == nil) {
		missing{http.StatusNotFound, "Dieses Dokument gibt es nicht.", "Dokumente", DokumentePath, "Dokumente"}.write(w, r, cfg)
		return
	}
	if cfg.Docs != nil {
		ctx, cancel := context.WithTimeout(r.Context(), filedWait)
		defer cancel()
		id, err := cfg.Docs.Filed(ctx, mark)
		if err != nil {
			log.Printf("haus: dokumente: filed %s: %v", mark, err)
		}
		if to := cfg.Docs.Page(id); id > 0 && to != "" {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
	}
	if !newest.Set() {
		missing{http.StatusNotFound, "Dieses Dokument gibt es nicht.", "Dokumente", DokumentePath, "Dokumente"}.write(w, r, cfg)
		return
	}
	http.Redirect(w, r, newest.href(newestPath), http.StatusFound)
}
