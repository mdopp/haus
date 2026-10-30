package pages

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/paperless/paperlessfake"
)

// a Laib with laib#568's verb scan: status prints the service's status
// file, every other action is an order; or a box without the service, or
// a Laib that does not give haus the verb. It keeps the orders.
type scanLaib struct {
	mu     sync.Mutex
	status map[string]any
	orders []map[string]any
	unset  bool   // "Der Dienst scan ist nicht eingerichtet."
	old    bool   // no laib_scan for haus
	refuse string // an order's refusal, a tool error
}

func (f *scanLaib) client(t *testing.T) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text, isErr := "[]", false
		switch {
		case req.Params.Name != "laib_scan":
		case f.old:
			text, isErr = "haus may not scan: policy.yml limits it to status, inbox", true
		case f.unset && req.Params.Args["action"] != "einrichten":
			text, isErr = "laib: Der Dienst scan ist nicht eingerichtet.", true
		case req.Params.Args["action"] == "status":
			b, _ := json.Marshal(f.status)
			text = string(b)
		default:
			f.mu.Lock()
			f.orders = append(f.orders, req.Params.Args)
			f.mu.Unlock()
			if f.refuse != "" {
				text, isErr = f.refuse, true
				break
			}
			text = `{"message":"Auftrag angenommen; was der Scanner tut, steht in laib scan status."}`
			if req.Params.Args["action"] == "einrichten" {
				text = `{"proposal":42,"class":"human","message":"Vorschlag #42 liegt im Eingang: Den Scanner einrichten."}`
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

// scanPage is the Dokumente page with a scan service as f says, seen from
// home (192.168.1.23) by anna in groups.
func scanPage(t *testing.T, f *scanLaib, groups string) string {
	t.Helper()
	return scanPageAt(t, f, groups, "/dokumente")
}

// scanPageAt is scanPage at target, the redirect after an order.
func scanPageAt(t *testing.T, f *scanLaib, groups, target string) string {
	t.Helper()
	_, cfg := dokumente(t)
	cfg.Laib, cfg.SambaHost = f.client(t), "kraftwerk"
	cfg.Loc, cfg.Now = berlin(t), func() time.Time { return startNow(t) }
	rec := getDokumente(cfg, target, map[string]string{"X-Forwarded-For": "192.168.1.23", "Remote-Groups": groups})
	if rec.Code != 200 {
		t.Fatalf("%d\n%s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// haus#76: after a jam (wartet-auf-rest) Scannen comes first on the page,
// says from which sheet the rest goes in, and offers Weiter, Abschließen
// and Verwerfen — each a POST to /scannen; no new stack while one waits,
// and the scanner's own reason stays in the log.
func TestScannenAfterAJamSaysWhereToGoOn(t *testing.T) {
	body := scanPage(t, &scanLaib{status: map[string]any{"zustand": "wartet-auf-rest", "blatt": 6, "grund": "sane_start: Error during device I/O", "scanner": true}}, "family")
	mustContain(t, body, "Nach Blatt 6 abgebrochen", "Den Rest ab Blatt 6 einlegen",
		`<form method=post action=/scannen><input type=hidden name=tun value=weiter><button data-laeuft="Scannt weiter …">Weiter</button></form>`,
		`value=abschliessen><button>Abschließen</button>`, `value=verwerfen><button>Verwerfen</button>`)
	mustNotContain(t, body, "Stapel scannen", "sane_start", "device I/O", "<div hidden data-auffrischen")
	before(t, body, "<h2>Scannen</h2>", "Per E-Mail ablegen")
}

// haus#76: „Stapel scannen“ only with a SANE scanner (scanner: true) and
// nothing running; the inbox folder is always there. At rest Scannen
// follows the other ways in.
func TestScannenOffersAStackOnlyWithAScanner(t *testing.T) {
	for _, z := range []string{"bereit", "fertig", "fehler"} {
		body := scanPage(t, &scanLaib{status: map[string]any{"zustand": z, "scanner": true}}, "family")
		mustContain(t, body, `data-laeuft="Startet …">Stapel scannen</button>`)
		before(t, body, "Dokument ablegen", "<h2>Scannen</h2>")
		body = scanPage(t, &scanLaib{status: map[string]any{"zustand": z, "scanner": false}}, "family")
		mustNotContain(t, body, "Stapel scannen", "action=/scannen>")
		mustContain(t, body, `\\kraftwerk\data\scan-eingang`, "nur zu Hause im WLAN")
	}
	body := scanPage(t, &scanLaib{status: map[string]any{"zustand": "scannt", "seite": 12, "scanner": true}}, "family")
	mustContain(t, body, "<b>Scannt</b>", "Seite 12", "data-auffrischen=5>", saysScanBusy) // owner 2026-10-27: where the button went
	mustNotContain(t, body, "value=start>", "action=/scannen>")
	body = scanPage(t, &scanLaib{status: map[string]any{"zustand": "trennt", "detail": "trennt 47 Seiten", "scanner": true}}, "family")
	mustContain(t, body, "Das Hausmodell liest 47 Seiten", saysScanBusy)
}

// haus#76: the last stacks with what came of them — how many documents,
// each by its title, leading to it in Paperless from home — and a dropped
// stack says that nothing was filed.
func TestScannenShowsTheLastStacks(t *testing.T) {
	f := &scanLaib{status: map[string]any{"zustand": "fertig", "scanner": true, "letzte": []map[string]any{
		{"stapel": "2026-10-22T10-12-40", "ende": "fertig", "quelle": "scanner", "at": "2026-10-22T08:15:00Z", "seiten": 38, "leer": 9,
			"dokumente": []map[string]any{{"titel": "2023-05-01 Mosel - Rechnung", "datei": "2023-05-01 Mosel - Rechnung.pdf"}, {"titel": "2021-03-02 Schornsteinfeger - KÜO", "datei": "x.pdf"}}},
		{"stapel": "2026-10-21T18-00-00", "ende": "abgebrochen", "quelle": "ordner", "at": "2026-10-21T16:00:00Z", "seiten": 4},
	}}}
	body := scanPage(t, f, "family")
	mustContain(t, body, "Letzte Stapel", "<b>2 Dokumente</b>", "38 Seiten, 9 leer entfernt", "vom Scanner",
		`<a href="https://paperless.box.test/documents?title_content=2023-05-01+Mosel+-+Rechnung">2023-05-01 Mosel - Rechnung</a>`,
		"2021-03-02 Schornsteinfeger - KÜO", "Verworfen, nichts abgelegt", "aus dem Ordner")
	before(t, body, "2 Dokumente", "Verworfen, nichts abgelegt")

	mustContain(t, scanPage(t, &scanLaib{status: map[string]any{"zustand": "bereit"}}, "family"), saysNoStacks)
}

// haus#76, haus#77: a box without the scan service shows the family no
// Scannen at all, an admin „Scannen einrichten“ (a proposal, laib#571)
// and a device by hand; a Laib that does not give haus the verb tells only
// an admin what to do; a scan service not answering is one plain line.
func TestScannenWithoutTheService(t *testing.T) {
	body := scanPage(t, &scanLaib{unset: true}, "family")
	mustNotContain(t, body, "Scannen", "scan-eingang", "Scannen einrichten", "name=geraet")
	mustContain(t, body, "Dokument ablegen")
	body = scanPage(t, &scanLaib{unset: true}, "family,admins")
	mustContain(t, body, `<section class="weg scannen" id=scanner><h2>Scannen</h2>`, saysScanUnset,
		`value=einrichten><button>Scannen einrichten</button>`, `name=geraet`, "Adresse von Hand")
	mustNotContain(t, body, "Stapel scannen", "Scanner suchen")
	mustNotContain(t, scanPage(t, &scanLaib{old: true}, "family"), "Scannen", "policy.yml")
	mustContain(t, scanPage(t, &scanLaib{old: true}, "family,admins"), "<h2>Scannen</h2>", saysScanOld, "v0.20.0")

	_, cfg := dokumente(t)
	cfg.Laib = &laib.Client{URL: "http://127.0.0.1:1", Token: "t"}
	body = getDokumente(cfg, "/dokumente", nil).Body.String()
	mustContain(t, body, "<h2>Scannen</h2>", saysScanSilent)
	mustNotLeak(t, body)
}

// haus#76: a button is one order to Laib (laib scan <aktion>), for the
// person who pressed it, then back to the page; only from Haus's own
// page, only the page's four orders.
func TestScannenPassesTheOrderOn(t *testing.T) {
	f := &scanLaib{status: map[string]any{"zustand": "wartet-auf-rest", "blatt": 3}}
	_, cfg := dokumente(t)
	cfg.Laib = f.client(t)
	h := Scannen(cfg)
	rec := post(t, h, "/scannen", url.Values{"tun": {"weiter"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/dokumente?tut=weiter&seit=") { // haus#195
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.orders) != 1 || f.orders[0]["action"] != "weiter" || f.orders[0]["person"] != "anna" {
		t.Fatalf("orders %v", f.orders)
	}
	// a Remote-User the service would not take: the box's default person
	post(t, h, "/scannen", url.Values{"tun": {"start"}}, "Anna.M", nil)
	if _, named := f.orders[1]["person"]; named || f.orders[1]["action"] != "start" {
		t.Fatalf("order %v", f.orders[1])
	}
	for _, bad := range []url.Values{{"tun": {"redo"}}, {"tun": {""}}, {"tun": {"light.turn_off"}}} {
		if rec := post(t, h, "/scannen", bad, "anna", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%v → %d", bad, rec.Code)
		}
	}
	if rec := post(t, h, "/scannen", url.Values{"tun": {"start"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
	if rec := post(t, h, "/scannen", url.Values{"tun": {"start"}}, "", nil); rec.Code != http.StatusForbidden {
		t.Errorf("no person → %d", rec.Code)
	}
	if len(f.orders) != 2 {
		t.Fatalf("refused posts reached Laib: %v", f.orders)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/scannen", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET → %d", rec.Code)
	}

	// Laib refuses: the page says it did not work, in the family's words
	f.refuse = "laib: open /var/lib/laib/scan/auftrag: permission denied"
	rec = post(t, h, "/scannen", url.Values{"tun": {"verwerfen"}}, "anna", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("%d", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body, saysScanNotDone, "Nach Blatt 3 abgebrochen")
	mustNotLeak(t, body, "permission denied", "/var/lib")
	if strings.Count(body, "<h1>") != 1 {
		t.Errorf("one page:\n%s", body)
	}
}

// haus#77: an admin sets the scanner up under Scannen: the last check
// („Scanner antwortet“, or why not) with Erneut prüfen and Probeseite,
// Scanner suchen and what it found, each with Diesen einrichten, a device
// by hand, and what goes into a folder scanner's own menu. The family
// sees none of it.
func TestScannerEinrichtenShowsTheSetup(t *testing.T) {
	f := &scanLaib{status: map[string]any{"zustand": "bereit", "scanner": true,
		"pruefung": map[string]any{"geraet": "epson2:net:192.168.178.87", "ok": true, "at": "2026-10-22T20:00:00Z", "detail": "Scanner antwortet"},
		"suche": map[string]any{"zustand": "fertig", "at": "2026-10-22T19:59:00Z", "detail": "2 Scanner gefunden", "funde": []map[string]any{
			{"geraet": "epson2:net:192.168.178.87", "name": "Epson PID 0899 flatbed scanner"},
			{"geraet": "airscan:e0:Brother MFC", "name": "Brother MFC"}}}}}
	body := scanPage(t, f, "family,admins")
	mustContain(t, body, `<details id=scanner class=einrichten><summary>Scanner einrichten</summary>`, // a scanner is set: closed
		"Scanner antwortet", "epson2:net:192.168.178.87",
		`value=pruefen><button data-laeuft="Prüft den Scanner …">Erneut prüfen</button>`, `value=probe><button data-laeuft="Liest eine Probeseite …">Probeseite</button>`,
		`value=suchen><button data-laeuft="Sucht nach Scannern …">Scanner suchen</button>`,
		"Epson PID 0899 flatbed scanner", `<input type=hidden name=geraet value="epson2:net:192.168.178.87"><button aria-label="Epson PID 0899 flatbed scanner einrichten">Diesen einrichten</button>`,
		`name=geraet value="airscan:e0:Brother MFC"`, "2 Scanner gefunden", "Adresse von Hand",
		`\\kraftwerk\data`, "scan-eingang", "href=/netzlaufwerk>Netzlaufwerk</a>")
	mustNotContain(t, body, "<div hidden data-auffrischen")
	mustNotContain(t, scanPage(t, f, "family"), "Scanner einrichten", "Scanner suchen", "Probeseite", "pruefen")

	// no scanner yet, a search running, a check that failed: open, the page follows, no probe without a device
	f.status = map[string]any{"zustand": "bereit", "scanner": false,
		"pruefung": map[string]any{"geraet": "", "ok": false, "at": "2026-10-22T20:00:00Z", "detail": "kein Scanner eingerichtet (SCANNER ist leer)"},
		"suche":    map[string]any{"zustand": "läuft", "at": "2026-10-22T20:01:00Z", "funde": []any{}}}
	body = scanPage(t, f, "family,admins")
	mustContain(t, body, `<details id=scanner class=einrichten open>`, "Sucht nach Scannern", "data-auffrischen=5>",
		`<div class="achtung selbst"><p>kein Scanner eingerichtet`)
	mustNotContain(t, body, "Scanner suchen</button>", "Probeseite", "Erneut prüfen")
}

// haus#77: Einrichten is one proposal through Laib (laib_scan einrichten),
// admins only, a device only as laib#571 takes it; back with a fixed
// sentence (no device in the URL) and the way to the inbox. Suchen,
// Prüfen and Probeseite are orders, admins only.
func TestScannerEinrichtenProposes(t *testing.T) {
	f := &scanLaib{status: map[string]any{"zustand": "bereit"}}
	_, cfg := dokumente(t)
	cfg.Laib, cfg.LaibURL = f.client(t), "https://laib.box.test"
	h := Scannen(cfg)
	admin := map[string]string{"Remote-Groups": "admins"}
	rec := post(t, h, "/scannen", url.Values{"tun": {"einrichten"}, "geraet": {"epson2:net:192.168.178.87"}}, "anna", admin)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/dokumente?gesagt=vorgeschlagen&vorschlag=42#scanner" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.orders) != 1 || f.orders[0]["action"] != "einrichten" || f.orders[0]["geraet"] != "epson2:net:192.168.178.87" {
		t.Fatalf("calls %v", f.orders)
	}
	for _, bad := range []string{`epson2:net:1.2.3.4"; rm`, "epson2:net:$HOME", "nur-text", "epson2:net:1.2.3.4\nX"} {
		rec := post(t, h, "/scannen", url.Values{"tun": {"einrichten"}, "geraet": {bad}}, "anna", admin)
		if rec.Header().Get("Location") != "/dokumente?gesagt=geraet#scanner" {
			t.Errorf("%q → %q", bad, rec.Header().Get("Location"))
		}
	}
	// the family: no proposal, no setup orders
	if rec := post(t, h, "/scannen", url.Values{"tun": {"einrichten"}}, "ben", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/start" {
		t.Errorf("family einrichten → %d %q", rec.Code, rec.Header().Get("Location"))
	}
	for _, do := range []string{"suchen", "pruefen", "probe"} {
		if rec := post(t, h, "/scannen", url.Values{"tun": {do}}, "ben", nil); rec.Code != http.StatusForbidden {
			t.Errorf("family %s → %d", do, rec.Code)
		}
	}
	if len(f.orders) != 1 {
		t.Fatalf("refused posts reached Laib: %v", f.orders)
	}
	cfg.Now = func() time.Time { return time.Unix(1792757939, 0) }
	h = Scannen(cfg)
	for _, do := range []string{"suchen", "pruefen", "probe"} {
		if rec := post(t, h, "/scannen", url.Values{"tun": {do}}, "anna", admin); rec.Header().Get("Location") != "/dokumente?tut="+do+"&seit=1792757939#scanner" {
			t.Errorf("admin %s → %d %q", do, rec.Code, rec.Header().Get("Location"))
		}
	}
	if len(f.orders) != 4 || f.orders[1]["action"] != "suchen" {
		t.Fatalf("orders %v", f.orders)
	}

	// the page after: the sentence and the inbox with the proposal's number
	cfg.Loc, cfg.Now = berlin(t), func() time.Time { return startNow(t) }
	page := getDokumente(cfg, "/dokumente?gesagt=vorgeschlagen&vorschlag=42", admin).Body.String()
	mustContain(t, page, scanSaid["vorgeschlagen"], `<a href="https://laib.box.test/inbox">Zur Inbox (Vorschlag #42)</a>`, `<details id=scanner class=einrichten open>`)
	page = getDokumente(cfg, "/dokumente?gesagt=geraet", admin).Body.String()
	mustContain(t, page, scanSaid["geraet"])

	// a box without the service: the same proposal adds it, no device needed
	f.unset = true
	if rec := post(t, h, "/scannen", url.Values{"tun": {"einrichten"}}, "anna", admin); rec.Header().Get("Location") != "/dokumente?gesagt=vorgeschlagen&vorschlag=42#scanner" {
		t.Errorf("einrichten without the service → %q", rec.Header().Get("Location"))
	}
	if _, has := f.orders[len(f.orders)-1]["geraet"]; has {
		t.Errorf("an empty device was sent: %v", f.orders[len(f.orders)-1])
	}
}

// haus#80: after Scanner suchen the page follows the order where it was
// pressed — „Sucht …“ and a quick look again while the result is older
// than the order, then what was found or, found nothing, the plain
// sentence and the field by hand, ready; no answer in time says so. The
// same for Prüfen and Probeseite, with SANE's own words only folded away.
// the check's sentence as laib#589 writes it
const saysOff = "Der Scanner unter 192.168.178.87 antwortet nicht – ist er an?"

func TestScannerEinrichtenFollowsTheOrder(t *testing.T) {
	now := startNow(t)
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }
	after := func(do string, ago time.Duration) string {
		return "/dokumente?tut=" + do + "&seit=" + strconv.FormatInt(now.Add(-ago).Unix(), 10)
	}
	admins := "family,admins"
	old := map[string]any{"zustand": "fertig", "at": at(-time.Hour), "funde": []any{}, "detail": "kein Scanner gefunden"}
	f := &scanLaib{status: map[string]any{"zustand": "bereit", "scanner": false, "suche": old}}

	// idle: the last search's result, no following
	body := scanPage(t, f, admins)
	mustContain(t, body, saysNotFound, "Scanner suchen</button>", `<script>`)
	mustNotContain(t, body, "<div hidden data-auffrischen", "role=alert><p>"+saysNotFound, "autofocus")

	// pressed, the service has not written yet: Sucht …, no button, look again soon
	body = scanPageAt(t, f, admins, after("suchen", 2*time.Second))
	mustContain(t, body, `<details id=scanner class=einrichten open>`, `<p class=laeuft role=status>Sucht nach Scannern …</p>`, "data-auffrischen=2>")
	mustNotContain(t, body, "Scanner suchen</button>", saysNotFound) // the old result is not this one's

	// the service runs it
	f.status["suche"] = map[string]any{"zustand": "läuft", "at": at(-time.Second), "funde": []any{}}
	body = scanPageAt(t, f, admins, after("suchen", 3*time.Second))
	mustContain(t, body, "Sucht nach Scannern …", "data-auffrischen=2>")
	if strings.Count(body, "Sucht nach Scannern …</p>") != 1 {
		t.Errorf("one Sucht:\n%s", body)
	}

	// done, nothing found: the sentence at the button and the field, ready
	f.status["suche"] = map[string]any{"zustand": "fertig", "at": at(0), "funde": []any{}, "detail": "kein Scanner gefunden: das Gerät an, im selben Netz?"}
	body = scanPageAt(t, f, admins, after("suchen", 4*time.Second))
	mustContain(t, body, `<div class="achtung selbst" role=alert><p>`+saysNotFound, `<input id=geraet autofocus `, "Scanner suchen</button>")
	mustNotContain(t, body, "<div hidden data-auffrischen", "Sucht nach Scannern …</p>", "das Gerät an, im selben Netz?")

	// done with finds: each to choose
	f.status["suche"] = map[string]any{"zustand": "fertig", "at": at(0), "funde": []map[string]any{{"geraet": "epson2:net:192.168.178.87", "name": "Epson WF-3540"}}}
	body = scanPageAt(t, f, admins, after("suchen", 4*time.Second))
	mustContain(t, body, "Epson WF-3540", "Diesen einrichten")
	mustNotContain(t, body, saysNotFound, "autofocus", "<div hidden data-auffrischen")

	// failed: Laib's sentence why, not „nothing found“
	f.status["suche"] = map[string]any{"zustand": "fehler", "at": at(0), "funde": []any{}, "detail": "Netzsuche abgebrochen"}
	body = scanPageAt(t, f, admins, after("suchen", 4*time.Second))
	mustContain(t, body, `role=alert><p>Netzsuche abgebrochen</p>`)
	mustNotContain(t, body, saysNotFound)

	// no result in a minute: said, no endless looking
	f.status["suche"] = old
	body = scanPageAt(t, f, admins, after("suchen", 61*time.Second))
	mustContain(t, body, `<div class=achtung role=alert><p>Laib hat den Auftrag angenommen, aber nach einer Minute kein Ergebnis gemeldet.`, "Scanner suchen</button>")
	mustNotContain(t, body, "<div hidden data-auffrischen", "Sucht nach Scannern …</p>")

	// a link too old, or made up: no following
	for _, target := range []string{after("suchen", time.Hour), "/dokumente?tut=suchen&seit=x", "/dokumente?tut=redo&seit=" + strconv.FormatInt(now.Unix(), 10)} {
		mustNotContain(t, scanPageAt(t, f, admins, target), "<div hidden data-auffrischen", "Laib hat den Auftrag", "Sucht nach Scannern …</p>")
	}

	// Prüfen: waits for a check as new as the order, then shows it at the button
	f.status = map[string]any{"zustand": "bereit", "scanner": true,
		"pruefung": map[string]any{"geraet": "epson2:net:192.168.178.87", "ok": true, "at": at(-time.Hour), "detail": "Scanner antwortet"}}
	body = scanPageAt(t, f, admins, after("pruefen", time.Second))
	mustContain(t, body, "Prüft den Scanner …</p>", "data-auffrischen=2>")
	mustNotContain(t, body, "Scanner antwortet", "Erneut prüfen</button>")
	f.status["pruefung"] = map[string]any{"geraet": "epson2:net:192.168.178.87", "ok": false, "at": at(0),
		"detail": "Der Scanner unter 192.168.178.87 antwortet nicht – ist er an?", "einzelheiten": "open of device epson2:net:192.168.178.87 failed: Invalid argument"}
	body = scanPageAt(t, f, admins, after("pruefen", 3*time.Second))
	mustContain(t, body, `<div class="achtung selbst" role=status><p>`+saysOff, "<summary>Einzelheiten</summary>", "Erneut prüfen</button>")
	mustNotContain(t, body, "<div hidden data-auffrischen", "Prüft den Scanner …</p>")
	if i, j := strings.Index(body, saysOff), strings.Index(body, "Invalid argument"); i < 0 || j < i || !strings.Contains(body[i:j], "<details") {
		t.Errorf("SANE's words not folded away:\n%s", body)
	}
	f.status["pruefung"].(map[string]any)["at"] = at(-time.Hour)
	body = scanPageAt(t, f, admins, after("probe", 3*time.Minute))
	mustContain(t, body, saysOff, "nach 2 Minuten kein Ergebnis")
	mustNotContain(t, body, "role=status><p>"+saysOff) // not the probe's answer
}

// haus#88 (laib#589): Laib's sentence stands as it is; the raw error
// behind it — of a check, a search, a stack — is folded away under
// „Einzelheiten“ for an admin, and the family never sees it. A search
// says how many neighbours it asked.
func TestScannenShowsEinzelheitenToAdmins(t *testing.T) {
	f := &scanLaib{status: map[string]any{"zustand": "bereit", "scanner": true,
		"pruefung": map[string]any{"geraet": "epson2:net:192.168.178.87", "ok": false, "at": "2026-10-22T20:00:00Z",
			"detail": "Der Scanner antwortet nicht.", "einzelheiten": "TCP 192.168.178.87:1865: timed out"},
		"suche": map[string]any{"zustand": "fehler", "at": "2026-10-22T20:00:00Z", "funde": []any{}, "gefragt": 7,
			"detail": "Die Suche lief nicht durch – bitte noch einmal versuchen.", "einzelheiten": "scanimage: not found"},
		"letzte": []map[string]any{{"stapel": "s1", "ende": "fehler", "quelle": "scanner", "at": "2026-10-22T19:00:00Z",
			"meldung": "Scan fehlgeschlagen.", "einzelheiten": "sane_start: Error during device I/O"}}}}
	body := scanPage(t, f, "family,admins")
	mustContain(t, body, `<div class="achtung selbst"><p>Der Scanner antwortet nicht.`,
		"<summary>Einzelheiten</summary><p class=adresse>TCP 192.168.178.87:1865: timed out</p>",
		"Die Suche lief nicht durch – bitte noch einmal versuchen.", "Im Heimnetz wurden 7 Geräte direkt gefragt.",
		"<p class=adresse>scanimage: not found</p>", "<p class=adresse>sane_start: Error during device I/O</p>")
	mustNotContain(t, scanPage(t, f, "family"), "Einzelheiten", "timed out", "Error during device I/O", "scanimage")
}

// owner 2026-10-27: „es stehen da noch Scans die es gar nicht mehr in
// paperless gibt“ — a stack filed again shows once, its newest run, and
// what was deleted in Paperless is not listed
func TestScannenListsWhatPaperlessStillHas(t *testing.T) {
	fake := paperlessfake.New("pl")
	defer fake.Close()
	fake.Add(paperlessfake.Doc{ID: 1, Title: "Rechnung 2023", OriginalFileName: "2023-06-15 Mosel - Rechnung 2023.pdf"})
	doc := func(d string) laib.StackDoc { return laib.StackDoc{Titel: strings.TrimSuffix(d, ".pdf"), Datei: d} }
	all := []laib.Stack{
		{Stapel: "A", Ende: "fertig", Dokumente: []laib.StackDoc{doc("2023-06-15 Mosel - Rechnung 2023.pdf"), doc("2023-06-22 Mosel - Brief.pdf")}},
		{Stapel: "A", Ende: "fertig", Dokumente: []laib.StackDoc{doc("Mosel - Dokument.pdf")}},
		{Stapel: "B", Ende: "fertig", Dokumente: []laib.StackDoc{doc("2022-10-07 Bohlen - Lieferschein.pdf")}},
		{Stapel: "C", Ende: "abgebrochen"},
	}
	cfg := StartConfig{Docs: &paperless.Client{URL: fake.URL, Token: "pl"}}
	got := shownStacks(httptest.NewRequest("GET", "/dokumente", nil), cfg, all)
	if len(got) != 3 || got[0].Stapel != "A" || len(got[0].docs) != 1 || got[0].docs[0].title != "2023-06-15 Mosel - Rechnung 2023" ||
		!got[1].gone || got[1].line() != "Nichts mehr in Paperless" || got[2].Stapel != "C" || got[2].gone {
		t.Fatalf("shown: %+v", got)
	}
	// Paperless not answering: as the service says, each stack once
	cfg.Docs = &paperless.Client{URL: "http://127.0.0.1:1", Token: "pl"}
	if got := shownStacks(httptest.NewRequest("GET", "/dokumente", nil), cfg, all); len(got) != 3 || len(got[0].docs) != 2 || got[1].gone {
		t.Fatalf("Paperless away: %+v", got)
	}
}

// haus#197: a stack's documents come from Paperless by the field „Stapel“
// (Dokumente writes the stack id, mdopp/dokumente#15): renamed, split or
// merged there, they are listed as they are now, each a link to the
// document, and the whole stack opens as Paperless's own list; a stack
// Paperless lists nothing for yet keeps Dokumente's list, by id where
// Dokumente knows it. Each stack is one folded line, the newest open.
func TestScannenListsAStackFromPaperless(t *testing.T) {
	fake := paperlessfake.New("pl")
	defer fake.Close()
	fake.Field(1, "Anbieter", "string")
	fake.Field(7, "Stapel", "string")
	fake.Add(paperlessfake.Doc{ID: 11, Title: "Stromrechnung Teil 1", Fields: map[int]any{7: "2026-10-29T17-07-46"}},
		paperlessfake.Doc{ID: 12, Title: "Stromrechnung Teil 2", Fields: map[int]any{7: "2026-10-29T17-07-46", 1: "Strom AG"}},
		paperlessfake.Doc{ID: 13, Title: "Anderer Stapel", Fields: map[int]any{7: "2026-10-28T09-00-00"}},
		paperlessfake.Doc{ID: 20, Title: "Brief", OriginalFileName: "Brief.pdf"}) // filed before the field
	f := &scanLaib{status: map[string]any{"zustand": "fertig", "scanner": true, "letzte": []map[string]any{
		{"stapel": "2026-10-29T17-07-46", "ende": "fertig", "quelle": "scanner", "at": "2026-10-29T16:07:46Z", "seiten": 6, "fuer": "person", "person": "anna",
			"dokumente": []map[string]any{{"titel": "2026-10-01 Strom AG - Rechnung", "datei": "2026-10-01 Strom AG - Rechnung.pdf", "id": 10}}},
		{"stapel": "2026-10-29T10-00-00", "ende": "fertig", "quelle": "ordner", "at": "2026-10-29T08:00:00Z", "person": "ben",
			"dokumente": []map[string]any{{"titel": "Brief", "datei": "Brief.pdf", "id": 20}}},
	}}}
	_, cfg := dokumente(t)
	cfg.Laib, cfg.Loc, cfg.Now = f.client(t), berlin(t), func() time.Time { return startNow(t) }
	cfg.Docs = &paperless.Client{URL: fake.URL, Token: "pl"}
	rec := getDokumente(cfg, "/dokumente", map[string]string{"X-Forwarded-For": "192.168.1.23", "Remote-Groups": "family"})
	body := rec.Body.String()
	mustContain(t, body, `<details data-stapel="2026-10-29T17-07-46" open><summary><b>2 Dokumente</b>`, "nur für Anna", "6 Seiten",
		`<a href="https://paperless.box.test/documents/11/details">Stromrechnung Teil 1</a>`,
		`<a href="https://paperless.box.test/documents/12/details">Stromrechnung Teil 2</a>`,
		`<a href="https://paperless.box.test/documents?custom_field_query=%5B%22AND%22%2C%5B%5B7%2C%22exact%22%2C%222026-10-29T17-07-46%22%5D%5D%5D">Den ganzen Stapel in Paperless öffnen</a>`,
		`<details data-stapel="2026-10-29T10-00-00"><summary><b>1 Dokument</b>`, "aus dem Ordner · Ben",
		`<a href="https://paperless.box.test/documents/20/details">Brief</a>`, "localStorage")
	mustNotContain(t, body, "Anderer Stapel", "2026-10-01 Strom AG - Rechnung", "title_content=Brief", "nur für Ben")
	before(t, body, "Stromrechnung Teil 1", "Stromrechnung Teil 2")
}

// mdopp/dokumente#16: the scan writes a refused order with its reason into
// the status at once; the page shows it where „Startet …“ was, the button
// free again, without waiting 30 seconds. A taken one goes on as before.
func TestScannenShowsARefusedStartInPlace(t *testing.T) {
	now := startNow(t)
	seit := "/dokumente?tut=start&seit=" + strconv.FormatInt(now.Add(-time.Second).Unix(), 10)
	grund := "„Nur für mich“ geht gerade nicht: Paperless antwortet nicht. Nichts gescannt; versuche es in ein paar Minuten noch einmal."
	f := &scanLaib{status: map[string]any{"zustand": "fertig", "at": now.UTC().Format(time.RFC3339), "scanner": true,
		"auftrag": map[string]any{"aktion": "start", "at": now.UTC().Format(time.RFC3339), "angenommen": false, "grund": grund}}}
	body := scanPageAt(t, f, "family", seit)
	mustContain(t, body, `<div class=achtung role=alert><p>`+html.EscapeString(grund)+`</p></div>`, `data-laeuft="Startet …">Stapel scannen</button>`)
	mustNotContain(t, body, "Startet …</p>", "nach 30 Sekunden", "<div hidden data-auffrischen")

	// an older refusal is not this order's: still Startet …
	f.status["auftrag"] = map[string]any{"aktion": "start", "at": now.Add(-time.Hour).UTC().Format(time.RFC3339), "angenommen": false, "grund": grund}
	body = scanPageAt(t, f, "family", seit)
	mustContain(t, body, `<p class="zustand laeuft" role=status>Startet …</p>`)
	mustNotContain(t, body, html.EscapeString(grund))

	// taken: Startet … until the stack's own state
	f.status["auftrag"] = map[string]any{"aktion": "start", "at": now.UTC().Format(time.RFC3339), "angenommen": true}
	mustContain(t, scanPageAt(t, f, "family", seit), `<p class="zustand laeuft" role=status>Startet …</p>`)
}

// laib#831: a stack the model could not split waits and is done again by
// the service itself; Haus says so, with the service's sentence
func TestScannenSaysAStackIsDoneAgain(t *testing.T) {
	st := map[string]any{"zustand": "bereit", "scanner": true, "letzte": []any{map[string]any{
		"stapel": "2026-10-27T19-32-41", "ende": "nachholen", "quelle": "scanner", "seiten": 36,
		"meldung": "Scan wartet: Das Trennen wird nachgeholt, sobald das Hausmodell wieder antwortet. Bis dahin liegt nichts davon in Paperless."}}}
	body := scanPage(t, &scanLaib{status: st}, "family")
	mustContain(t, body, "<b>Wird nachgeholt</b>", "Das Trennen wird nachgeholt, sobald das Hausmodell wieder antwortet.", `data-laeuft="Startet …">Stapel scannen</button>`)
	mustNotContain(t, body, "Kein Dokument", "Scan wartet:")
}

// laib ADR 0028: Dokumente reads contracts and invoices when Paperless takes
// new documents; Scannen says what its last run did
func TestScannenSaysWhatTheEvaluationDid(t *testing.T) {
	st := map[string]any{"zustand": "bereit", "scanner": true, "auswertung": map[string]any{
		"zustand": "fertig", "at": "2026-10-14T07:02:00Z", "anlass": "neue Dokumente",
		"detail": "Nach neuen Dokumenten ausgewertet. Gelesen: 2 Verträge, 1 Rechnung. 1 Dokument zum Prüfen markiert."}}
	body := scanPage(t, &scanLaib{status: st}, "family")
	mustContain(t, body, "<h3>Verträge und Rechnungen</h3><p>Nach neuen Dokumenten ausgewertet. Gelesen: 2 Verträge, 1 Rechnung. 1 Dokument zum Prüfen markiert.", "heute, 09:02")
	st["auswertung"] = map[string]any{"zustand": "fehler", "at": "2026-10-14T07:02:00Z", "detail": "Die Auswertung brach ab; der nächste Lauf versucht es wieder.", "einzelheiten": "vertraege: Traceback"}
	body = scanPage(t, &scanLaib{status: st}, "family")
	mustContain(t, body, "<p class=fehler>Die Auswertung brach ab")
	mustNotContain(t, body, "Traceback")
	mustContain(t, scanPage(t, &scanLaib{status: st}, "admins"), "vertraege: Traceback")
	delete(st, "auswertung") // an older Laib, or no run yet
	mustNotContain(t, scanPage(t, &scanLaib{status: st}, "family"), "Verträge und Rechnungen")
}

// owner 2026-10-28: a stack is the household's by default, or only the
// person's; the page says honestly that admins still see it; the order
// carries the choice only for „nur für mich“, and only on start.
func TestScannenFuerWen(t *testing.T) {
	body := scanPage(t, &scanLaib{status: map[string]any{"zustand": "bereit", "scanner": true}}, "family")
	mustContain(t, body, `<input type=radio name=fuer value=haushalt checked> Für den Haushalt`,
		`<input type=radio name=fuer value=mich> Nur für mich`, html.EscapeString(saysScanFuer), `data-laeuft="Startet …">Stapel scannen</button>`)
	f := &scanLaib{status: map[string]any{"zustand": "bereit", "scanner": true}}
	_, cfg := dokumente(t)
	cfg.Laib = f.client(t)
	h := Scannen(cfg)
	post(t, h, "/scannen", url.Values{"tun": {"start"}, "fuer": {"haushalt"}}, "anna", nil)
	post(t, h, "/scannen", url.Values{"tun": {"start"}, "fuer": {"mich"}}, "anna", nil)
	post(t, h, "/scannen", url.Values{"tun": {"weiter"}, "fuer": {"mich"}}, "anna", nil)
	post(t, h, "/scannen", url.Values{"tun": {"start"}, "fuer": {"mich"}}, "Anna.M", nil)
	want := []any{nil, "person", nil, nil}
	for i, o := range f.orders {
		if o["fuer"] != want[i] {
			t.Errorf("order %d: %v", i, o)
		}
	}
	if len(f.orders) != 4 || f.orders[1]["person"] != "anna" {
		t.Fatalf("orders %v", f.orders)
	}
}

// haus#195: after „Stapel scannen“ the page follows the order until the
// service writes that the stack runs: Startet …, no free button and no
// second order, looking again soon; then the stack's own state; a reason
// written by the service (fehler) where the button was; no start in 30 s
// said honestly, with the button back. Weiter the same.
func TestScannenFollowsAStackOrder(t *testing.T) {
	now := startNow(t)
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }
	after := func(do string, ago time.Duration) string {
		return "/dokumente?tut=" + do + "&seit=" + strconv.FormatInt(now.Add(-ago).Unix(), 10)
	}

	// the order goes on, the page is told when
	f := &scanLaib{status: map[string]any{"zustand": "fertig", "at": at(-time.Hour), "scanner": true}}
	_, cfg := dokumente(t)
	cfg.Laib, cfg.Loc, cfg.Now = f.client(t), berlin(t), func() time.Time { return now }
	for _, do := range []string{"start", "weiter"} {
		rec := post(t, Scannen(cfg), "/scannen", url.Values{"tun": {do}}, "anna", nil)
		if want := "/dokumente?tut=" + do + "&seit=" + strconv.FormatInt(now.Unix(), 10); rec.Header().Get("Location") != want {
			t.Errorf("%s → %q, want %q", do, rec.Header().Get("Location"), want)
		}
	}

	// a second after the order, the status still the last stack's: Startet …, nothing to press
	for _, groups := range []string{"family", "family,admins"} {
		body := scanPageAt(t, f, groups, after("start", time.Second))
		mustContain(t, body, `<p class="zustand laeuft" role=status>Startet …</p>`, "data-auffrischen=2>")
		mustNotContain(t, body, "value=start>", "Stapel scannen</button>", "<b>Fertig</b>", "Erneut prüfen</button>", "Probeseite</button>")
	}

	// the service writes the stack's state: the page shows it
	f.status = map[string]any{"zustand": "scannt", "seite": 1, "at": at(0), "scanner": true}
	body := scanPageAt(t, f, "family", after("start", 2*time.Second))
	mustContain(t, body, "<b>Scannt</b>", saysScanBusy)
	mustNotContain(t, body, "Startet …</p>", "value=start>")

	// the service writes why it did not start: the reason where the button was, the button back
	f.status = map[string]any{"zustand": "fehler", "detail": "Scan fehlgeschlagen: der Scanner lässt sich nicht starten.", "at": at(0), "scanner": true}
	body = scanPageAt(t, f, "family", after("start", 2*time.Second))
	mustContain(t, body, "Der letzte Stapel ging schief", "der Scanner lässt sich nicht starten", `data-laeuft="Startet …">Stapel scannen</button>`)
	mustNotContain(t, body, "Startet …</p>", "<div hidden data-auffrischen")

	// nothing in 30 seconds: said, the button back, no endless looking
	f.status = map[string]any{"zustand": "fertig", "at": at(-time.Hour), "scanner": true}
	body = scanPageAt(t, f, "family", after("start", 31*time.Second))
	mustContain(t, body, `<div class=achtung role=alert><p>Der Scanner hat nach 30 Sekunden nicht angefangen.`, `data-laeuft="Startet …">Stapel scannen</button>`)
	mustNotContain(t, body, "Startet …</p>", "<div hidden data-auffrischen")

	// Weiter after a jam: Scannt weiter … until the service scans again
	f.status = map[string]any{"zustand": "wartet-auf-rest", "blatt": 6, "at": at(-time.Minute), "scanner": true}
	body = scanPageAt(t, f, "family", after("weiter", time.Second))
	mustContain(t, body, `<p class="zustand laeuft" role=status>Scannt weiter …</p>`, "data-auffrischen=2>")
	mustNotContain(t, body, "Weiter</button>", "Abschließen</button>", "Verwerfen</button>", "Nach Blatt 6 abgebrochen")
	body = scanPage(t, f, "family")
	mustContain(t, body, `data-laeuft="Scannt weiter …">Weiter</button>`)
}

// mdopp/dokumente#14: the scanner is free while earlier stacks are split.
// „Stapel scannen“ comes with einzug frei, whatever the worker does; below
// it what is split now, at which page, and how many wait. The scanner busy
// says when the button comes back; a jam offers Weiter as before.
func TestScannenOffersAStackWhileTheQueueRuns(t *testing.T) {
	queue := []map[string]any{
		{"stapel": "2026-10-14T09-55-10", "person": "ben", "fuer": "haushalt", "quelle": "scanner", "seiten": 4, "seit": "2026-10-14T07:55:40Z"},
		{"stapel": "2026-10-14T09-58-00", "person": "anna", "fuer": "person", "quelle": "scanner", "seit": "2026-10-14T07:58:30Z"},
	}
	// idle: nothing in work, no queue lines
	body := scanPage(t, &scanLaib{status: map[string]any{"zustand": "fertig", "einzug": "frei", "scanner": true, "warten": []any{}}}, "family")
	mustContain(t, body, "<b>Fertig</b>", `data-laeuft="Startet …">Stapel scannen</button>`)
	mustNotContain(t, body, "Wird getrennt", "Danach:", `class="laeuft reihe"`)

	// running: the worker splits 09:52 at page 12 of 40, two wait; the scanner is free
	f := &scanLaib{status: map[string]any{"zustand": "trennt", "einzug": "frei", "scanner": true, "stapel": "2026-10-14T09-52-00", "detail": "liest Seite 12 von 40",
		"trennt": map[string]any{"stapel": "2026-10-14T09-52-00", "person": "anna", "fuer": "haushalt", "schritt": "trennt", "detail": "liest Seite 12 von 40", "seite": 12, "seiten": 40},
		"warten": queue}}
	body = scanPage(t, f, "family")
	mustContain(t, body, "<b>Bereit</b>", "Der Scanner ist frei für den nächsten Stapel.", `data-laeuft="Startet …">Stapel scannen</button>`,
		`<p class="laeuft reihe">Wird getrennt: Stapel von 09:52, Seite 12 von 40 <span class=k>Anna</span></p>`,
		`<p class="k reihe">Danach: 2 Stapel</p>`, "data-auffrischen=5>")
	mustNotContain(t, body, saysScanBusy, "Trennt die Dokumente")
	before(t, body, "Stapel scannen</button>", "Wird getrennt")

	// the model is held, then filing; a stack from yesterday says its day
	f.status["trennt"] = map[string]any{"stapel": "2026-10-13T21-00-00", "person": "anna", "fuer": "person", "schritt": "trennt", "detail": "wartet auf das Modell: Joshua spricht bis 10:05"}
	mustContain(t, scanPage(t, f, "family"), "Wird getrennt: Stapel vom 13. Oktober 2026, 21:00, wartet auf das Hausmodell <span class=k>nur für Anna</span>")
	f.status["trennt"] = map[string]any{"stapel": "2026-10-14T09-52-00", "schritt": "legt-ab", "detail": "legt 3 Dokumente ab"}
	body = scanPage(t, f, "family")
	mustContain(t, body, `<p class="laeuft reihe">Wird abgelegt: Stapel von 09:52</p>`, "Danach: 2 Stapel")
	mustNotContain(t, body, "Joshua")

	// a queue nobody picked up yet: the first is next, one after it
	delete(f.status, "trennt")
	body = scanPage(t, f, "family")
	mustContain(t, body, `Wird gleich getrennt: Stapel von 09:55 <span class=k>Ben</span></p>`, "Danach: 1 Stapel", "Stapel scannen</button>")

	// the scanner runs: no button, when it comes back, the queue below
	f.status["zustand"], f.status["einzug"], f.status["seite"] = "scannt", "scannt", 3
	body = scanPage(t, f, "family")
	mustContain(t, body, "<b>Scannt</b>", "Seite 3", saysScanScanning, "Wird gleich getrennt: Stapel von 09:55")
	mustNotContain(t, body, "value=start>", saysScanBusy)

	// a jam at the scanner while the worker splits: Weiter, no new stack
	f.status["zustand"], f.status["einzug"], f.status["blatt"] = "wartet-auf-rest", "wartet-auf-rest", 6
	body = scanPage(t, f, "family")
	mustContain(t, body, "Nach Blatt 6 abgebrochen", "value=weiter>", "Wird gleich getrennt")
	mustNotContain(t, body, "value=start>")
}

// haus#195 with the queue (mdopp/dokumente#14): the worker writes while
// the scanner rests, so a newer `at` is no start; the start is taken when
// a stack newer than the order is at the scanner, queued or ended.
func TestScannenFollowsAStartWhileTheQueueRuns(t *testing.T) {
	now := startNow(t)
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }
	after := "/dokumente?tut=start&seit=" + strconv.FormatInt(now.Add(-2*time.Second).Unix(), 10)
	late := "/dokumente?tut=start&seit=" + strconv.FormatInt(now.Add(-31*time.Second).Unix(), 10)
	working := func() map[string]any {
		return map[string]any{"zustand": "trennt", "einzug": "frei", "scanner": true, "at": at(0),
			"trennt": map[string]any{"stapel": "2026-10-14T09-52-00", "schritt": "trennt", "seite": 13, "seiten": 40},
			"letzte": []any{map[string]any{"stapel": "2026-10-14T09-40-00", "ende": "fertig", "at": at(0)}}}
	}

	// running: the worker wrote just now, the scanner has not begun: Startet …
	f := &scanLaib{status: working()}
	body := scanPageAt(t, f, "family", after)
	mustContain(t, body, `<p class="zustand laeuft" role=status>Startet …</p>`, "data-auffrischen=2>", "Wird getrennt: Stapel von 09:52, Seite 13 von 40")
	mustNotContain(t, body, "Stapel scannen</button>")

	// done: the scanner scans a stack begun after the order
	f.status["zustand"], f.status["einzug"], f.status["stapel"], f.status["seite"] = "scannt", "scannt", "2026-10-14T09-59-59", 1
	body = scanPageAt(t, f, "family", after)
	mustContain(t, body, "<b>Scannt</b>", saysScanScanning)
	mustNotContain(t, body, "Startet …</p>", "value=start>")
	// … but not one begun before it
	f.status["stapel"] = "2026-10-14T09-50-00"
	mustContain(t, scanPageAt(t, f, "family", after), "Startet …</p>")

	// done: a short stack already scanned and queued
	f.status = working()
	f.status["warten"] = []any{map[string]any{"stapel": "2026-10-14T09-59-59", "person": "anna", "seit": at(0)}}
	body = scanPageAt(t, f, "family", after)
	mustContain(t, body, "Stapel scannen</button>", "Danach: 1 Stapel")
	mustNotContain(t, body, "Startet …</p>")

	// failed: the new stack ended at once, the button back
	f.status = working()
	f.status["letzte"] = []any{map[string]any{"stapel": "2026-10-14T09-59-59", "ende": "fehler", "at": at(0), "meldung": "Scan fehlgeschlagen."},
		map[string]any{"stapel": "2026-10-14T09-40-00", "ende": "fertig", "at": at(0)}}
	body = scanPageAt(t, f, "family", after)
	mustContain(t, body, "<b>Ging schief</b>", "Stapel scannen</button>")
	mustNotContain(t, body, "Startet …</p>", "nach 30 Sekunden")

	// timed out: 30 seconds and only the worker wrote: said, the button back
	f.status = working()
	body = scanPageAt(t, f, "family", late)
	mustContain(t, body, "Der Scanner hat nach 30 Sekunden nicht angefangen.", "Stapel scannen</button>", "Wird getrennt")
	mustNotContain(t, body, "Startet …</p>")

	// Weiter: taken once the scanner no longer waits for the rest, not by the worker's writing
	f.status = working()
	f.status["zustand"], f.status["einzug"], f.status["blatt"] = "wartet-auf-rest", "wartet-auf-rest", 6
	weiter := strings.Replace(after, "tut=start", "tut=weiter", 1)
	mustContain(t, scanPageAt(t, f, "family", weiter), "Scannt weiter …</p>")
	f.status["zustand"], f.status["einzug"] = "scannt", "scannt"
	body = scanPageAt(t, f, "family", weiter)
	mustContain(t, body, "<b>Scannt</b>")
	mustNotContain(t, body, "Scannt weiter …</p>")
}

// mdopp/dokumente#12: a document whose split is doubtful carries its
// reason; the page says it beside the document and on the folded line,
// from Dokumente's list and onto Paperless's documents by id.
func TestScannenSaysWhatToCheck(t *testing.T) {
	why := "das Modell hat für 2 Seiten nicht geantwortet"
	st := map[string]any{"zustand": "fertig", "einzug": "frei", "scanner": true, "letzte": []any{
		map[string]any{"stapel": "2026-10-14T09-07-00", "ende": "fertig", "quelle": "scanner", "seiten": 9, "dokumente": []any{
			map[string]any{"titel": "Arztbrief", "datei": "Arztbrief.pdf"},
			map[string]any{"titel": "Befund", "datei": "Befund.pdf", "pruefen": why}}},
		map[string]any{"stapel": "2026-10-13T09-07-00", "ende": "fertig", "dokumente": []any{map[string]any{"titel": "Brief", "datei": "Brief.pdf"}}}}}
	body := scanPage(t, &scanLaib{status: st}, "family")
	mustContain(t, body, `<b>2 Dokumente</b>`, `<span class=zweifel>1 bitte prüfen</span></summary>`,
		`">Befund</a> <span class=zweifel>Bitte prüfen: `+why+`</span></li>`, `">Arztbrief</a></li>`)
	if strings.Count(body, "bitte prüfen") != 1 {
		t.Errorf("only the stack with a doubt is marked:\n%s", body)
	}

	// from Paperless by the field „Stapel“: the doubt follows the id
	fake := paperlessfake.New("pl")
	defer fake.Close()
	fake.Field(7, "Stapel", "string")
	fake.Add(paperlessfake.Doc{ID: 31, Title: "Arztbrief Dr. B", Fields: map[int]any{7: "2026-10-14T09-07-00"}},
		paperlessfake.Doc{ID: 32, Title: "Befund (umbenannt)", Fields: map[int]any{7: "2026-10-14T09-07-00"}})
	all := []laib.Stack{{Stapel: "2026-10-14T09-07-00", Ende: "fertig", Dokumente: []laib.StackDoc{
		{Titel: "Arztbrief", Datei: "a.pdf", ID: 31}, {Titel: "Befund", Datei: "b.pdf", ID: 32, Pruefen: why}}}}
	cfg := StartConfig{Docs: &paperless.Client{URL: fake.URL, Token: "pl"}}
	got := shownStacks(httptest.NewRequest("GET", "/dokumente", nil), cfg, all)
	if len(got) != 1 || len(got[0].docs) != 2 || got[0].doubtful() != 1 {
		t.Fatalf("shown: %+v", got)
	}
	for _, d := range got[0].docs {
		if (d.pruefen != "") != (d.title == "Befund (umbenannt)") {
			t.Errorf("%+v", d)
		}
	}
}
