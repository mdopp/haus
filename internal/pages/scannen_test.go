package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/laib"
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
	_, cfg := dokumente(t)
	cfg.Laib, cfg.SambaHost = f.client(t), "kraftwerk"
	cfg.Loc, cfg.Now = berlin(t), func() time.Time { return startNow(t) }
	rec := getDokumente(cfg, "/dokumente", map[string]string{"X-Forwarded-For": "192.168.1.23", "Remote-Groups": groups})
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
	mustNotContain(t, body, "Stapel scannen", "sane_start", "device I/O", "http-equiv=refresh")
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
		mustNotContain(t, body, "Stapel scannen", "action=/scannen")
		mustContain(t, body, `\\kraftwerk\data\scan-eingang`, "nur zu Hause im WLAN")
	}
	body := scanPage(t, &scanLaib{status: map[string]any{"zustand": "scannt", "seite": 12, "scanner": true}}, "family")
	mustContain(t, body, "<b>Scannt</b>", "Seite 12", "<meta http-equiv=refresh content=5>")
	mustNotContain(t, body, "Stapel scannen", "action=/scannen")
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
		`value=pruefen><button>Erneut prüfen</button>`, `value=probe><button>Probeseite</button>`, `value=suchen><button>Scanner suchen</button>`,
		"Epson PID 0899 flatbed scanner", `<input type=hidden name=geraet value="epson2:net:192.168.178.87"><button aria-label="Epson PID 0899 flatbed scanner einrichten">Diesen einrichten</button>`,
		`name=geraet value="airscan:e0:Brother MFC"`, "2 Scanner gefunden", "Adresse von Hand",
		`\\kraftwerk\data`, "scan-eingang", "href=/netzlaufwerk>Netzlaufwerk</a>")
	mustNotContain(t, body, "http-equiv=refresh")
	mustNotContain(t, scanPage(t, f, "family"), "Scanner einrichten", "Scanner suchen", "Probeseite", "pruefen")

	// no scanner yet, a search running, a check that failed: open, the page follows, no probe without a device
	f.status = map[string]any{"zustand": "bereit", "scanner": false,
		"pruefung": map[string]any{"geraet": "", "ok": false, "at": "2026-10-22T20:00:00Z", "detail": "kein Scanner eingerichtet (SCANNER ist leer)"},
		"suche":    map[string]any{"zustand": "läuft", "at": "2026-10-22T20:01:00Z", "funde": []any{}}}
	body = scanPage(t, f, "family,admins")
	mustContain(t, body, `<details id=scanner class=einrichten open>`, "Sucht im Netz nach Scannern", "<meta http-equiv=refresh content=5>",
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
	for _, do := range []string{"suchen", "pruefen", "probe"} {
		if rec := post(t, h, "/scannen", url.Values{"tun": {do}}, "anna", admin); rec.Header().Get("Location") != "/dokumente#scanner" {
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
