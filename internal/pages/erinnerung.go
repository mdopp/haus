package pages

import (
	"context"
	"html"
	"log"
	"net/http"
	"strings"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
)

// Erinnerung is one reminder's own page (haus#28), the page a reminders
// widget row opens: its title, whether it is open, due, done or cancelled.
// Ticking it off stays the widget's (haus#1: the pages show). Radicale
// not answering is Kalender's plain line (haus#33).
func Erinnerung(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Die Erinnerung") {
			return
		}
		if cfg.Calendar == nil {
			toStart(w, r)
			return
		}
		uid := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, cards.ReminderPages))
		var todos []caldav.Todo
		var err error
		if uid != "" {
			fetch(r, func(ctx context.Context) { todos, err = cfg.Calendar.Todos(ctx) })
		}
		if err != nil {
			log.Printf("haus: erinnerung page: %q: %v", uid, err)
			missing{http.StatusOK, saysNoCalendar, "Kalender", "/kalender", "Kalender"}.write(w, r, cfg)
			return
		}
		for _, t := range todos {
			if uid == "" || strings.TrimSpace(t.UID) != uid {
				continue
			}
			now, loc := cfg.clock()
			title := summary(t.Summary)
			b := familyHead(r, cfg, title, "Kalender")
			b.WriteString("<h1>" + html.EscapeString(title) + "</h1>")
			c := cards.Card{Title: "Erinnerung"}
			switch {
			case strings.EqualFold(t.Status, "CANCELLED"):
				c.Add("abgesagt", "")
			case !t.Open():
				done := ""
				if !t.Completed.IsZero() {
					done = dayWord(t.Completed, now, loc) + " " + t.Completed.In(loc).Format("15:04")
				}
				c.Add("erledigt", done)
			default:
				due := dueWords(t, now, loc)
				if due == "" {
					due = "ohne Termin"
				}
				c.Add("offen", due)
			}
			b.WriteString(c.HTML())
			b.WriteString("<p><a href=/kalender>Kalender</a></p>")
			writePage(w, http.StatusOK, b)
			return
		}
		missing{http.StatusNotFound, "Diese Erinnerung gibt es nicht.", "Kalender", "/kalender", "Kalender"}.write(w, r, cfg)
	}
}
