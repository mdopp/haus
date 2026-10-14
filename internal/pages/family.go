package pages

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/cards"
)

// The family's pages (haus#12): Start, Räume, Kalender, Notizen. They read
// one config (StartConfig), share one nav and the persona's header, and
// show without changing anything (haus#1). A page whose backend is not set
// is not served: it lands on Start, like every path Haus does not serve.

// familyNav is Start · Räume · Kalender · Notizen, each only when its
// backend is set, and Box for admins; the page itself is marked.
func familyNav(r *http.Request, cfg StartConfig, current string) string {
	var b strings.Builder
	b.WriteString("<nav>")
	link := func(href, name string) {
		b.WriteString("<a href=" + href)
		if name == current {
			b.WriteString(" aria-current=page")
		}
		b.WriteString(">" + name + "</a>")
	}
	link("/start", "Start")
	if cfg.HA != nil {
		link("/raeume", "Räume")
	}
	if cfg.Calendar != nil {
		link("/kalender", "Kalender")
	}
	if cfg.Notes != nil {
		link("/notizen", "Notizen")
	}
	if isAdmin(r) {
		link("/box", "Box")
	}
	b.WriteString("</nav>")
	return b.String()
}

// familyHead opens a family page: the persona's header, the cards' style
// and the nav. The page writes its own h1.
func familyHead(r *http.Request, cfg StartConfig, title, current string) *strings.Builder {
	b := &strings.Builder{}
	b.WriteString(head(title))
	b.WriteString(cards.Style)
	b.WriteString("<style>.sr{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}" +
		".notiz{white-space:pre-wrap;overflow-wrap:anywhere}input[type=search]{font:inherit;padding:.25rem .5rem;border:1px solid #c9c3b6;border-radius:.4rem;width:min(24rem,60vw)}</style>")
	b.WriteString(familyNav(r, cfg, current))
	return b
}

// showOnly answers anything but GET and HEAD with 405: the family's pages
// show, they change nothing (haus#1).
func showOnly(w http.ResponseWriter, r *http.Request, what string) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, what+" zeigt nur, ändert nichts.", http.StatusMethodNotAllowed)
	return false
}

// toStart sends a page whose backend is not set to Start.
func toStart(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/start", http.StatusFound)
}

// clock is the page's now, in the house's time zone.
func (cfg StartConfig) clock() (time.Time, *time.Location) {
	loc := cfg.Loc
	if loc == nil {
		loc = time.Local
	}
	now := time.Now()
	if cfg.Now != nil {
		now = cfg.Now()
	}
	return now.In(loc), loc
}

// fetch runs each backend read at once, each within backendWait: a slow
// one is left out, the page still comes.
func fetch(r *http.Request, reads ...func(context.Context)) {
	var wg sync.WaitGroup
	for _, f := range reads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), backendWait)
			defer cancel()
			f(ctx)
		}()
	}
	wg.Wait()
}

// logErrs puts a page's backend errors in Haus's log, not on the page.
func logErrs(page string, errs map[string]error) {
	for what, err := range errs {
		if err != nil {
			log.Printf("haus: %s page: %s: %v", page, what, err)
		}
	}
}

// writePage sends the page.
func writePage(w http.ResponseWriter, code int, b *strings.Builder) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	w.Write([]byte(b.String()))
}

// noteHref is a note's own page: its file name, each part escaped.
func noteHref(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "/notizen/" + strings.Join(parts, "/")
}

// more closes a cut list: how many were left out.
func more(c *cards.Card, n int) {
	if n > 0 {
		c.Add("und "+strconv.Itoa(n)+" weitere", "")
	}
}
