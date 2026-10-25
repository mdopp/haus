package napi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/merken"
)

// Share → Haus (haus#120): the app is a target for a link or a text shared
// from the phone's browser, and posts it here; Haus keeps it as a note the
// way "Aus Link merken" and Joshua do (internal/merken). The body is JSON
// {"text": what was shared}: the first http(s) address in it is the link,
// the words around it are the hint ("#tags" become tags). The answer:
// {"titel", "art", "kurz": the line to show, "schon": kept before,
// "teilweise": only link and title, "notiz": the note's page on Haus or ""}.
// A refusal is {"error": a German sentence} with 4xx; a page that does not
// answer, 502.

// merkenWait is how long keeping may take: a page, then the house model.
var merkenWait = 75 * time.Second

var linkInText = regexp.MustCompile(`https?://[^\s<>"'„“”]+`)

// sharedLink is the link in a shared text and the words around it.
func sharedLink(text string) (string, string) {
	loc := linkInText.FindStringIndex(text)
	if loc == nil {
		return "", ""
	}
	link := strings.TrimRight(text[loc[0]:loc[1]], ".,;:!?)]}")
	var hint []string
	for _, w := range strings.Fields(text[:loc[0]] + " " + text[loc[1]:]) {
		if strings.Trim(w, "„“”\"'«»") != "" { // the quotes the link stood in
			hint = append(hint, w)
		}
	}
	return link, strings.Join(hint, " ")
}

func (c Config) merken(w http.ResponseWriter, r *http.Request) {
	h := c.Haushalt
	answer := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	if h.Notes == nil {
		answer(http.StatusNotFound, map[string]string{"error": "Notizen sind auf dieser Box nicht eingerichtet."})
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		answer(http.StatusBadRequest, map[string]string{"error": "Das kam nicht lesbar an."})
		return
	}
	link, hint := sharedLink(in.Text)
	if link == "" {
		answer(http.StatusBadRequest, map[string]string{"error": "Im Geteilten ist kein Link. Teile eine Webseite aus dem Browser."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), merkenWait)
	defer cancel()
	got, err := merken.Merken(ctx, h.Fetcher, h.Judge, h.Notes, link, hint)
	if err != nil {
		code := http.StatusBadRequest
		switch {
		case errors.Is(err, merken.ErrNoAnswer): // with its reason (merken.NoAnswer)
			code = http.StatusBadGateway
		case errors.Is(err, context.DeadlineExceeded):
			code, err = http.StatusGatewayTimeout, errors.New("Haus hat länger als "+merkenWait.String()+" gebraucht und aufgegeben. Später noch einmal teilen.")
		case errors.Is(err, merken.ErrNoAddress), errors.Is(err, merken.ErrPrivate):
		default:
			log.Printf("haus: napi merken: %v", err)
			code, err = http.StatusInternalServerError, errors.New("Das ließ sich gerade nicht merken. Später noch einmal teilen.")
		}
		answer(code, map[string]string{"error": err.Error()})
		return
	}
	note := ""
	if pub := strings.TrimRight(h.PublicURL, "/"); pub != "" {
		note = pub + cards.NotePage(got.Name)
	}
	log.Printf("haus: napi merken: %s (%s)", got.Name, got.Art)
	answer(http.StatusOK, map[string]any{"titel": got.Titel, "art": got.Art, "kurz": got.Kurz, "schon": got.Schon, "teilweise": got.Teilweise, "notiz": note})
}
