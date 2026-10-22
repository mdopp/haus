package pages

import (
	"html"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// DokumentePath is the Dokumente page (haus#68); a document's own page is
// below it (cards.DocumentPages, Dokument).
const DokumentePath = "/dokumente"

// What the Dokumente page says (haus#68), in the family's words.
const (
	saysDocsLead     = "Briefe, Rechnungen, Verträge: alles, was aufgehoben werden soll, kommt in Paperless, das Archiv des Hauses."
	saysMailHow      = "Häng das PDF oder Foto an und schick die Mail ab. Abgelegt werden die Anhänge, der Betreff wird der Titel. Paperless holt die Mails alle zehn Minuten ab."
	saysUploadHow    = "Fotos (JPG, PNG) und PDFs, bis 25 MB. Ohne Titel bekommt das Dokument den Namen der Datei."
	saysFiled        = "Liegt in Paperless und wird gerade gelesen (Texterkennung). Das dauert ein paar Minuten."
	saysFiledMany    = " Dokumente liegen in Paperless und werden gerade gelesen (Texterkennung). Das dauert ein paar Minuten."
	saysSeeFiled     = "Im Paperless ansehen"
	saysSeeNewest    = "Die neuesten in Paperless ansehen"
	saysPaperlessLAN = "Paperless öffnet sich nur zu Hause im WLAN. Ablegen geht hier auch von unterwegs."
	saysOnlyAtHome   = "Nur zu Hause im WLAN."
)

// docsWhatHappens is what happens to a document once it is filed (haus#68,
// haus#13): who reads it and where it is found again.
func docsWhatHappens() string {
	return "Paperless liest den Text jedes Dokuments (Texterkennung) und legt es ins Archiv. Danach findest du es hier unter Notizen über die Suche, und " +
		House.Display + " findet es, wenn du fragst, etwa „Wann läuft die Autoversicherung aus?“."
}

// what a refused upload says: napi's reason in the family's words
var uploadSays = map[string]string{
	"unsupported type":     "Das geht nicht: nur Fotos (JPG, PNG) und PDFs.",
	"empty file":           "Die Datei ist leer.",
	"file too large":       "Die Datei ist zu groß, höchstens 25 MB.",
	"request too large":    "Zusammen zu groß, höchstens 64 MB auf einmal.",
	"too many files":       "Höchstens 20 Dateien auf einmal.",
	"no file part":         "Bitte zuerst eine Datei wählen.",
	"busy":                 "Gerade wird schon abgelegt. Gleich noch einmal versuchen.",
	"insufficient_storage": "Auf der Box ist gerade kein Platz. Sag einem Admin Bescheid.",
	"too_slow":             "Das Hochladen hat zu lange gedauert. Noch einmal versuchen, am besten im WLAN.",
}

const saysUploadFailed = "Das Ablegen hat nicht geklappt. Später noch einmal versuchen."

// Dokumente is the family's way into the archive (haus#68): put a document
// in by mail (a mailto to HAUS_DOCUMENTS_MAIL) or right here (a file or
// the camera, into Paperless's inbox like the app's, haus#10), open
// Paperless itself ("Weiter in", haus#64), and in a line what happens to
// a document. Each part only when its config is set; none set, the page
// says so (laib#418).
func Dokumente(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DokumentePath {
			Elsewhere(cfg)(w, r)
			return
		}
		if !cfg.hasDocs() {
			notSetDocuments.write(w, r, cfg, "Dokumente")
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			q := r.URL.Query()
			done, problem, see := "", "", ""
			n, err := strconv.Atoi(q.Get("abgelegt"))
			if err == nil && n == 1 {
				done = saysFiled
			} else if err == nil && n > 1 {
				done = strconv.Itoa(n) + saysFiledMany
			}
			if done != "" {
				see = cfg.seeFiled(r, n, q.Get("datei"))
			}
			if why := q.Get("fehler"); why != "" {
				problem = uploadSays[why]
				if problem == "" {
					problem = saysUploadFailed
				}
			}
			writeDokumente(w, r, cfg, http.StatusOK, done, see, problem)
		case http.MethodPost:
			if cfg.Ablage == nil {
				http.Error(w, "Ablegen geht hier nicht.", http.StatusNotFound)
				return
			}
			if !fromHaus(r) {
				http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
				return
			}
			placed, code, why := cfg.Ablage.Store(w, r)
			if code != 0 {
				log.Printf("haus: dokumente: upload by %s refused: %d %s", r.Header.Get("Remote-User"), code, why)
				http.Redirect(w, r, DokumentePath+"?fehler="+url.QueryEscape(why), http.StatusSeeOther)
				return
			}
			to := DokumentePath + "?abgelegt=" + strconv.Itoa(len(placed))
			if mark := fileMark(placed); mark != "" {
				to += "&datei=" + mark // the name's random part only: the title stays out of the URL
			}
			http.Redirect(w, r, to, http.StatusSeeOther)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, "Nur Anzeigen und das Formular.", http.StatusMethodNotAllowed)
		}
	}
}

var notSetDocuments = notSet{"Die Dokumente sind hier noch nicht eingerichtet.", "Für Admins: Haus kennt weder die Adresse für Mails (HAUS_DOCUMENTS_MAIL) noch Paperless' Einwurf-Ordner (HAUS_UPLOAD_DIR) oder Adresse (HAUS_PAPERLESS_PUBLIC_URL)."}

// hasDocs says the Dokumente page has at least one way in.
func (cfg StartConfig) hasDocs() bool {
	return cfg.DocumentsMail != "" || cfg.Ablage != nil || cfg.PaperlessPublicURL != ""
}

// MailAddress is an address from the config as a mailto takes it: one
// plain address (name@host), no display name, nothing a mailto would read
// as more (?, &, %); anything else is "" (no button rather than a wrong one).
func MailAddress(raw string) string {
	a, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || a.Name != "" || strings.ContainsAny(a.Address, "?&%#/ \"<>") || strings.Count(a.Address, "@") != 1 {
		return ""
	}
	return a.Address
}

// paperless is Paperless as Dokumente hands over to it.
func (cfg StartConfig) paperless() App { return App{"Paperless", cfg.PaperlessPublicURL} }

// newestPath is Paperless's document list, the newest added first: what
// a family member looks for there (its list's sort and reverse
// parameters).
const newestPath = "/documents?sort=added&reverse=1"

// FiledPages is where a filed file's link goes: /dokumente/abgelegt/<mark>
// asks Paperless which document it became (Dokument, haus#68).
const FiledPages = "/dokumente/abgelegt/"

// markRE is the random part napi puts in a placed file's name,
// <stem>-<mark>.<ext>.
var markRE = regexp.MustCompile(`^[0-9a-f]{8}$`)
var placedRE = regexp.MustCompile(`-([0-9a-f]{8})\.[a-z]+$`)

// fileMark is the mark of the one file just placed; "" for several or a
// name without one.
func fileMark(placed []string) string {
	if len(placed) != 1 {
		return ""
	}
	if m := placedRE.FindStringSubmatch(placed[0]); m != nil {
		return m[1]
	}
	return ""
}

// paperlessOpens says the person can open Paperless from where they are.
func (cfg StartConfig) paperlessOpens(r *http.Request) bool {
	return cfg.paperless().Set() && (!cfg.PaperlessLANOnly || atHome(r))
}

// seeFiled is the link under "Liegt in Paperless": one file with its mark,
// Haus's /dokumente/abgelegt/<mark> (the document once Paperless has read
// it); otherwise Paperless's newest. "" where Paperless does not open.
func (cfg StartConfig) seeFiled(r *http.Request, n int, mark string) string {
	if !cfg.paperlessOpens(r) {
		return ""
	}
	if n == 1 && cfg.Docs != nil && markRE.MatchString(mark) {
		return `<a href="` + FiledPages + mark + `">` + html.EscapeString(saysSeeFiled) + `</a>`
	}
	return `<a href="` + html.EscapeString(cfg.paperless().href(newestPath)) + `">` + html.EscapeString(saysSeeNewest) + `</a>`
}

// atHome says the request comes from the house's own network, as Caddy
// saw it (X-Forwarded-For; only Caddy reaches the pages, the proxy secret
// says so): a private, loopback or link-local address. No address at all
// counts as at home: then the link is shown, with the hint.
func atHome(r *http.Request) bool {
	first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ",")
	first = strings.TrimSpace(first)
	if first == "" {
		return true
	}
	ip := net.ParseIP(first)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

const dokStyle = `<style>.lead{color:var(--muted)}.weg{margin:22px 0 0;padding:14px;background:var(--card);border:1px solid var(--edge);border-radius:14px}.weg h2{margin:0;font-size:1rem}
.weg p.k{font-size:.85rem;margin:8px 0 0}.knopf{display:inline-flex;align-items:center;margin:12px 0 0;min-height:44px;padding:0 18px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-weight:600;text-decoration:none}
.weg label{display:block;font-size:.85rem;color:var(--muted);margin:12px 0 6px}.weg input[type=text]{width:100%;font:inherit;color:var(--ink);background:var(--panel);border:1px solid var(--edge);border-radius:12px;padding:0 14px;min-height:44px}
.weg input[type=file]{width:100%;font:inherit;color:var(--muted)}.weg button{margin:14px 0 0;width:100%}@media (min-width:36rem){.weg button{width:auto}}
.adresse{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.9rem;color:var(--ink);overflow-wrap:anywhere}
.gut{display:flex;gap:8px;align-items:flex-start;margin:16px 0 0;padding:12px 14px;border:1px solid var(--ok);border-radius:14px}.gut p{margin:0}</style>`

func writeDokumente(w http.ResponseWriter, r *http.Request, cfg StartConfig, code int, done, see, problem string) {
	scan := readScan(r, cfg)
	b := familyHead(r, cfg, "Dokumente", "Dokumente")
	if scan.moving() {
		b.WriteString("<meta http-equiv=refresh content=5>") // the scan moves on by itself; the page follows
	}
	b.WriteString(dokStyle + scanStyle)
	b.WriteString("<h1>Dokumente</h1><p class=lead>" + html.EscapeString(saysDocsLead) + "</p>")
	if done != "" {
		if see != "" {
			see = " " + see
		}
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(done) + see + "</p></div>")
	}
	if problem != "" {
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(problem) + "</p></div>")
	}
	scanning := scanSection(r, cfg, scan)
	if scan.active() { // a stack running or waiting comes first
		b.WriteString(scanning)
		scanning = ""
	}
	if cfg.DocumentsMail != "" {
		addr := html.EscapeString(cfg.DocumentsMail)
		b.WriteString(`<section class=weg><h2>Per E-Mail ablegen</h2>` +
			`<a class=knopf href="mailto:` + addr + `">Per E-Mail ablegen</a>` +
			`<p class=k>An <span class=adresse>` + addr + `</span>. ` + html.EscapeString(saysMailHow) + `</p></section>`)
	}
	if cfg.Ablage != nil {
		b.WriteString(`<section class=weg><h2>Dokument ablegen</h2>` +
			`<form method=post action=` + DokumentePath + ` enctype=multipart/form-data>` +
			`<label for=datei>Datei wählen oder Foto machen</label>` +
			`<input id=datei type=file name=file accept="image/jpeg,image/png,image/gif,image/webp,application/pdf" multiple required>` +
			`<label for=titel>Titel (wenn du magst)</label>` +
			`<input id=titel type=text name=filename maxlength=80 autocomplete=off placeholder="z. B. Stromrechnung Oktober">` +
			`<button type=submit>Ablegen</button></form>` +
			`<p class=k>` + html.EscapeString(saysUploadHow) + `</p></section>`)
	}
	b.WriteString(scanning)
	if p := cfg.paperless(); p.Set() {
		if !cfg.paperlessOpens(r) {
			b.WriteString(`<p class=weiter-in><span class=k>` + html.EscapeString(saysPaperlessLAN) + `</span></p>`)
		} else {
			what := "Die neuesten zuerst; alle Dokumente suchen, lesen und ordnen."
			if cfg.PaperlessLANOnly {
				what += " " + saysOnlyAtHome
			}
			b.WriteString(p.Weiter(newestPath, what))
		}
	}
	b.WriteString(`<p class=k>` + html.EscapeString(docsWhatHappens()) + `</p>`)
	writePage(w, code, b)
}
