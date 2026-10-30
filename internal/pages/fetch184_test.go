package pages

import (
	"bytes"
	"context"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// haus#184: a page that waits long for its backends says so in Haus's log,
// with the page and how long, so its time can be read on the box (laib
// logs haus) — the whole page sits behind the login and cannot be timed
// from outside. A quick page says nothing.
func TestASlowPageSaysHowLongItWaited(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	old := slowFetch
	slowFetch = 30 * time.Millisecond
	t.Cleanup(func() { slowFetch = old })
	r := httptest.NewRequest("GET", "/kalender", nil)
	fetch(r, func(context.Context) {})
	if logs.Len() != 0 {
		t.Fatalf("a quick page logged %q", logs.String())
	}
	fetch(r, func(context.Context) {}, func(context.Context) { time.Sleep(50 * time.Millisecond) })
	if got := logs.String(); !strings.Contains(got, "haus: /kalender waited 0.0") || !strings.Contains(got, "s for its backends") {
		t.Fatalf("logged %q", got)
	}
}
