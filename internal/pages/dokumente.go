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

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/paperless"
)

// DokumentePath is the Dokumente page (haus#68); a document's own page is
// below it (cards.DocumentPages, Dokument).
const DokumentePath = "/dokumente"

// What the Dokumente page says (haus#68), in the family's words.
const (
	saysDocsLead     = "Briefe, Rechnungen, Verträge: alles, was aufgehoben werden soll, kommt ins Archiv des Hauses."
	saysMailHow      = "Die Anhänge werden abgelegt, der Betreff wird der Titel."
	saysUploadHow    = "Fotos und PDFs, bis 25 MB. Ohne Titel heißt es wie die Datei."
	saysFiled        = "Liegt in Paperless und wird gerade gelesen (Texterkennung). Das dauert ein paar Minuten."
	saysFiledMany    = " Dokumente liegen in Paperless und werden gerade gelesen (Texterkennung). Das dauert ein paar Minuten."
	saysSeeFiled     = "Im Paperless ansehen"
	saysSeeNewest    = "Die neuesten in Paperless ansehen"
	saysPaperlessLAN = "Paperless öffnet sich nur zu Hause im WLAN; gerade bist du unterwegs. Ablegen geht hier auch von unterwegs."
	saysOnlyAtHome   = "Nur zu Hause im WLAN."
)

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
// in by mail (a mailto to HOMES_DOCUMENTS_MAIL) or right here (a file or
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
			modulAus(w, r, cfg, "dokumente", DokumentePath)
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
			if !fromHomes(r) {
				http.Error(w, "Das geht nur von Homes' eigener Seite.", http.StatusForbidden)
				return
			}
			placed, code, why := cfg.Ablage.Store(w, r)
			if code != 0 {
				log.Printf("homes: dokumente: upload by %s refused: %d %s", r.Header.Get("Remote-User"), code, why)
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
// Homes' /dokumente/abgelegt/<mark> (the document once Paperless has read
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

const dokStyle = `<style>.lead{color:var(--muted)}.ablegen{margin:18px 0 0}.ablegen .vorne{margin:0;cursor:pointer;background:var(--accent-soft);color:var(--accent)}.ablegen .vorne input{position:absolute;width:1px;height:1px;opacity:0}.ablegen .danach{display:none}.ablegen:has(input[type=file]:valid) .danach{display:block}
.ablegen label[for]{display:block;font-size:.85rem;color:var(--muted);margin:12px 0 6px}.ablegen input[type=text]{width:100%}.ablegen button{margin:12px 0 0}.ablegen p.k{font-size:.85rem}
.weg{margin:22px 0 0;padding:14px;background:var(--card);border-radius:14px}.weg h2{margin:0;font-size:1rem}
.weg p.k{font-size:.85rem;margin:8px 0 0}.knopf{display:inline-flex;align-items:center;margin:12px 0 0;min-height:44px;padding:0 18px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-weight:600;text-decoration:none}
.weg label{display:block;font-size:.85rem;color:var(--muted);margin:12px 0 6px}.weg input[type=text]{width:100%;font:inherit;color:var(--ink);background:var(--panel);border:1px solid var(--edge);border-radius:12px;padding:0 14px;min-height:44px}
.weg input[type=file]{width:100%;font:inherit;color:var(--muted)}.weg button{margin:14px 0 0;width:100%}@media (min-width:36rem){.weg button{width:auto}}
.adresse{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.9rem;color:var(--ink);overflow-wrap:anywhere}</style>`

func writeDokumente(w http.ResponseWriter, r *http.Request, cfg StartConfig, code int, done, see, problem string) {
	scan := readScan(r, cfg)
	b := familyHead(r, cfg, "Dokumente", "Dokumente")
	if scan.moving() {
		b.WriteString(refreshEvery(scan.refresh())) // the scan moves on by itself; the page follows in place
	}
	b.WriteString(dokStyle + dokFindenStyle + scanStyle + gekoppeltStyle + "<h1>Dokumente</h1><p class=lead>" + html.EscapeString(saysDocsLead) + "</p>")
	if done != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(done) + " " + see + "</p></div>")
	}
	finden, zuletzt := dokFinden(r, cfg)        // one wait for both
	b.WriteString(gesagt("", problem) + finden) // owner 2026-11-06: the way into one's documents first
	if p := cfg.paperless(); p.Set() {          // owner 2026-11-04: the archive itself leads, its way in big; LAN-only, said in one line, away from home too
		b.WriteString(vorne(p.href(newestPath), "In Paperless öffnen"))
		if cfg.PaperlessLANOnly {
			b.WriteString(zustand(map[bool]string{false: "gelb"}[atHome(r)], map[bool]string{true: saysOnlyAtHome, false: saysPaperlessLAN}[atHome(r)]))
		}
	}
	scanning := scanSection(r, cfg, scan)
	if scan.active() { // a stack running or waiting comes first
		b.WriteString(scanning)
		scanning = ""
	}
	if cfg.Ablage != nil { // what the page is for, first and big (the Mehr audit, owner 2026-11-04); the title once a file is chosen
		b.WriteString(`<form class=ablegen method=post action=` + DokumentePath + ` enctype=multipart/form-data><label class=vorne>Foto machen oder Datei ablegen` +
			`<input type=file name=file accept="image/jpeg,image/png,image/gif,image/webp,application/pdf" multiple required onchange="this.form.querySelector('.gewaehlt').textContent=[...this.files].map(f=>f.name).join(', ')"></label>` +
			`<p class="k gewaehlt" role=status></p><div class=danach><label for=titel>Titel (wenn du magst)</label><input id=titel type=text name=filename maxlength=80 autocomplete=off placeholder="z. B. Stromrechnung Oktober">` +
			`<button type=submit data-laeuft="Lädt hoch …">Ablegen</button></div><p class=k>` + html.EscapeString(saysUploadHow) + `</p></form>`)
	}
	if cfg.DocumentsMail != "" {
		addr := html.EscapeString(cfg.DocumentsMail)
		b.WriteString(`<p class=k>Oder per Mail an <a href="mailto:` + addr + `">` + addr + `</a>. ` + html.EscapeString(saysMailHow) + `</p>`)
	}
	b.WriteString(zuletzt.HTML() + scanning)
	writePage(w, code, b)
}

// zuletztAbgelegt is the five documents the archive got last, each to its
// page where Paperless opens; no rows when Paperless is silent.
func zuletztAbgelegt(r *http.Request, cfg StartConfig, docs []paperless.Doc) cards.Card {
	c, opens := cards.Card{Title: "Zuletzt abgelegt"}, cfg.paperlessOpens(r)
	now, loc := cfg.clock()
	for _, d := range docs {
		detail := strings.Trim(d.Correspondent+" · "+map[bool]string{false: dayWord(d.Created, now, loc)}[d.Created.IsZero()], " ·")
		c.AddLink(d.Title, detail, map[bool]string{true: cards.DocumentPages + strconv.Itoa(d.ID)}[opens]) // away from a LAN-only Paperless: no link
	}
	return c
}
