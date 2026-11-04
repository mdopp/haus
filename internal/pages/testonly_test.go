package pages

import (
	"net/http"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/werkstatt"
)

// Only the tests call these: production code calls what they wrap. They
// were in the package itself, where `deadcode` counted them as
// production code nobody runs (scripts/ratchet.py, test-only).

// Box is the admins' page: readiness, how many inbox cards wait, what is
// not green, the morning report. The full service list, the inbox's cards
// and the journal are Laib's own page's (haus#157, owner 2026-10-27);
// Box links there. laibURL is that page.
func Box(c *laib.Client, laibURL string) http.HandlerFunc { return BoxWith(c, laibURL, nil) }

// BoxWith is Box with the Werkstatt's tickets as a card (haus#15); nil, no card.
func BoxWith(c *laib.Client, laibURL string, tickets *werkstatt.Tickets) http.HandlerFunc {
	return BoxPage(BoxConfig{Laib: c, LaibURL: laibURL, Tickets: tickets})
}
