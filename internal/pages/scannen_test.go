package pages

import (
	"encoding/json"
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
		`<form method=post action=/scannen><input type=hidden name=tun value=weiter><button>Weiter</button></form>`,
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
		mustContain(t, body, `value=start><button>Stapel scannen</button>`)
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
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/dokumente" {
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
		`value=suchen><button data-laeuft="Sucht im Netz nach Scannern …">Scanner suchen</button>`,
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
	mustContain(t, body, `<details id=scanner class=einrichten open>`, "Sucht im Netz nach Scannern", "data-auffrischen=5>",
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
	mustContain(t, body, `<details id=scanner class=einrichten open>`, `<p class=laeuft role=status>Sucht im Netz nach Scannern …</p>`, "data-auffrischen=2>")
	mustNotContain(t, body, "Scanner suchen</button>", saysNotFound) // the old result is not this one's

	// the service runs it
	f.status["suche"] = map[string]any{"zustand": "läuft", "at": at(-time.Second), "funde": []any{}}
	body = scanPageAt(t, f, admins, after("suchen", 3*time.Second))
	mustContain(t, body, "Sucht im Netz nach Scannern …", "data-auffrischen=2>")
	if strings.Count(body, "Sucht im Netz nach Scannern …</p>") != 1 {
		t.Errorf("one Sucht:\n%s", body)
	}

	// done, nothing found: the sentence at the button and the field, ready
	f.status["suche"] = map[string]any{"zustand": "fertig", "at": at(0), "funde": []any{}, "detail": "kein Scanner gefunden: das Gerät an, im selben Netz?"}
	body = scanPageAt(t, f, admins, after("suchen", 4*time.Second))
	mustContain(t, body, `<div class="achtung selbst" role=alert><p>`+saysNotFound, `<input id=geraet autofocus `, "Scanner suchen</button>")
	mustNotContain(t, body, "<div hidden data-auffrischen", "Sucht im Netz nach Scannern …</p>", "das Gerät an, im selben Netz?")

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
	mustNotContain(t, body, "<div hidden data-auffrischen", "Sucht im Netz nach Scannern …</p>")

	// a link too old, or made up: no following
	for _, target := range []string{after("suchen", time.Hour), "/dokumente?tut=suchen&seit=x", "/dokumente?tut=start&seit=" + strconv.FormatInt(now.Unix(), 10)} {
		mustNotContain(t, scanPageAt(t, f, admins, target), "<div hidden data-auffrischen", "Laib hat den Auftrag", "Sucht im Netz nach Scannern …</p>")
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
	if len(got) != 2 || got[0].Stapel != "A" || len(got[0].Dokumente) != 1 || got[0].Dokumente[0].Titel != "2023-06-15 Mosel - Rechnung 2023" || got[1].Stapel != "C" {
		t.Fatalf("shown: %+v", got)
	}
	// Paperless not answering: as the service says, each stack once
	cfg.Docs = &paperless.Client{URL: "http://127.0.0.1:1", Token: "pl"}
	if got := shownStacks(httptest.NewRequest("GET", "/dokumente", nil), cfg, all); len(got) != 3 || len(got[0].Dokumente) != 2 {
		t.Fatalf("Paperless away: %+v", got)
	}
}

// laib#831: a stack the model could not split waits and is done again by
// the service itself; Haus says so, with the service's sentence
func TestScannenSaysAStackIsDoneAgain(t *testing.T) {
	st := map[string]any{"zustand": "bereit", "scanner": true, "letzte": []any{map[string]any{
		"stapel": "2026-10-27T19-32-41", "ende": "nachholen", "quelle": "scanner", "seiten": 36,
		"meldung": "Scan wartet: Das Trennen wird nachgeholt, sobald das Hausmodell wieder antwortet. Bis dahin liegt nichts davon in Paperless."}}}
	body := scanPage(t, &scanLaib{status: st}, "family")
	mustContain(t, body, "<b>Wird nachgeholt</b>", "Das Trennen wird nachgeholt, sobald das Hausmodell wieder antwortet.", `value=start><button>Stapel scannen</button>`)
	mustNotContain(t, body, "Kein Dokument", "Scan wartet:")
}
