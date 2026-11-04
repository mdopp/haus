package laib

import (
	"context"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/modul"
)

// Wunsch is one module's latest wish as laib_wish's stand answers it
// (laib#1038, homes ADR 0001 §10): a Box-Aufgabe for the admins, and
// what became of it — angefordert, wartet (a proposal in the Inbox),
// abgelehnt, eingerichtet (approved), umgesetzt (Homes ticked it off).
type Wunsch struct {
	Modul     string `json:"modul"`
	Stand     string `json:"stand"`
	Aufgabe   int    `json:"aufgabe"`
	Seit      string `json:"seit"`
	Von       string `json:"von,omitempty"`
	Vorschlag int    `json:"vorschlag,omitempty"`
	Grund     string `json:"grund,omitempty"`
}

// Gewuenscht is laib_wish's answer to a module's wish: the Box-Aufgabe,
// and whether it is new (false: one was open already, nothing filed).
type Gewuenscht struct {
	Aufgabe int    `json:"aufgabe"`
	Neu     bool   `json:"neu"`
	Message string `json:"message"`
}

// wuenscheKept is the last stand Laib answered, kept wuenscheFresh.
type wuenscheKept struct {
	mu   sync.Mutex
	je   map[string]Wunsch
	at   time.Time
	frag string
}

// wuenscheFresh is how long a stand is kept: a page shows it at once,
// and a proposal decided in the Inbox shows within a minute.
const wuenscheFresh = time.Minute

// NimmtWuensche: does this Laib take a wish for a module (laib#1038)?
// An error: Laib did not say.
func (c *Client) NimmtWuensche(ctx context.Context) (bool, error) {
	return c.Takes(ctx, "wish", "modul")
}

// Wuensche is the stand of each module's latest wish, by module; module
// is "<name>=<backend> …", the modules asked for. A stand younger than a
// minute for the same question is answered without asking; when Laib
// fails, the last one comes back with the error, so a page shows the
// last known state rather than none.
func (c *Client) Wuensche(ctx context.Context, module string) (map[string]Wunsch, error) {
	k := &c.wuensche
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.je != nil && k.frag == module && time.Since(k.at) < wuenscheFresh {
		return k.je, nil
	}
	text, err := c.CallTyped(ctx, "wish", map[string]any{"stand": module})
	var list []Wunsch
	if err == nil {
		err = decodeJSON("wish", text, &list)
	}
	if err != nil {
		if k.frag == module {
			return k.je, err
		}
		return nil, err
	}
	je := map[string]Wunsch{}
	for _, w := range list {
		je[w.Modul] = w
	}
	k.je, k.at, k.frag = je, time.Now(), module
	return je, nil
}

// PeekWuensche is the kept stand when it is fresh, without asking Laib.
func (c *Client) PeekWuensche(module string) (map[string]Wunsch, bool) {
	k := &c.wuensche
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.je, k.je != nil && k.frag == module && time.Since(k.at) < wuenscheFresh
}

// Wuenschen files a wish for a module for a person (von, the login):
// titel is the module's title. The kept stand is forgotten.
func (c *Client) Wuenschen(ctx context.Context, modul, titel, von string) (Gewuenscht, error) {
	defer c.ForgetWuensche()
	var out Gewuenscht
	text, err := c.CallTyped(ctx, "wish", map[string]any{"modul": modul, "von": von, "text": titel})
	if err == nil {
		err = decodeJSON("wish", text, &out)
	}
	return out, err
}

// ForgetWuensche drops the kept stand: the next page asks Laib again.
func (c *Client) ForgetWuensche() {
	c.wuensche.mu.Lock()
	c.wuensche.at = time.Time{}
	c.wuensche.mu.Unlock()
}

// ModulWuensche is je as the module list reads it; geht: Laib takes a wish.
func ModulWuensche(je map[string]Wunsch, geht bool) modul.Wuensche {
	w := modul.Wuensche{Je: map[string]modul.Wunsch{}, Geht: geht}
	for name, x := range je {
		w.Je[name] = modul.Wunsch{Stand: x.Stand, Seit: x.Seit}
	}
	return w
}
