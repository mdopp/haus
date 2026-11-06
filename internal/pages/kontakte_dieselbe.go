package pages

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/gesichter"
	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/texte"
)

// DieselbePath is „Dieselbe Person?“ (owner 2026-11-06): pairs of persons
// whose faces Immich holds close, ten a page, each with its two faces,
// names and photo counts, and Ja (merge in Immich), Nein (never asked
// again) and Überspringen. Only the Immich key's owner, as Meine
// Kontakte; the pairs come from the hourly run (gesichter.Vorschlagen),
// never from Immich while the page loads.
const DieselbePath = KontaktePath + "/dieselbe"

const (
	dieselbeJeSeite = 10
	dieselbeWait    = 30 * time.Second // a merge in Immich before the page says so
)

const dieselbeStyle = `<style>.paar{margin:18px 0 0;padding:14px;border-radius:16px;background:var(--card)}
.paar .zwei{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin:0}.paar figure{margin:0;text-align:center;min-width:0}
.paar img,.paar .ohne{display:block;width:96px;height:96px;margin:0 auto 8px;border-radius:50%;object-fit:cover;background:var(--track)}
.paar figcaption{overflow-wrap:anywhere}.paar figcaption b{display:block}.paar figcaption i,.paar figcaption span{display:block;color:var(--muted);font-size:.85rem}
.paar form{display:flex;flex-wrap:wrap;gap:8px;margin:14px 0 0}.paar form button{flex:1 1 auto}.paar form button.leise{flex:0 0 auto}
.blaettern{display:flex;justify-content:space-between;align-items:center;gap:8px;margin:20px 0 0}</style>`

// dieselbeLink is the quiet line to the page, while it has pairs.
func dieselbeLink(cfg StartConfig) string {
	n := len(cfg.Gesichter.Paare()) + len(cfg.Gesichter.Fragen())
	if n == 0 {
		return ""
	}
	return `<p class=k><a href=` + DieselbePath + `>` + html.EscapeString(texte.T(texte.Dieselbe)) + `</a> · ` +
		html.EscapeString(fmt.Sprintf(texte.T(texte.DieselbeKurz), texte.Anzahl(n, "Vorschlag", "Vorschläge"))) + `</p>`
}

// Dieselbe is the page, and its answers (POST a, b, antwort ja|nein|spaeter;
// about a contact's picture kontakt, b, antwort ja|nein|keinfoto).
func Dieselbe(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && !showOnly(w, r, texte.T(texte.Dieselbe)) {
			return
		}
		if !privatEigen(r, cfg) {
			http.Redirect(w, r, KontaktePath, http.StatusSeeOther)
			return
		}
		said, problem := "", ""
		if r.Method == http.MethodPost {
			if !fromHomes(r) {
				http.Error(w, "Das geht nur von Homes' eigener Seite.", http.StatusForbidden)
				return
			}
			r.ParseForm()
			antwort, kontakt := r.PostForm.Get("antwort"), r.PostForm.Get("kontakt")
			erlaubt := []string{"ja", "nein", "spaeter"}
			if kontakt != "" {
				erlaubt[2] = "keinfoto"
			}
			if !slices.Contains(erlaubt, antwort) {
				http.Error(w, "Ja, Nein oder Überspringen? Die Antwort fehlt.", http.StatusBadRequest)
				return
			}
			if kontakt != "" {
				said, problem = istAntworten(r.Context(), cfg, kontakt, r.PostForm.Get("b"), antwort)
			} else {
				said, problem = dieselbeAntworten(r.Context(), cfg, r.PostForm.Get("a"), r.PostForm.Get("b"), antwort)
			}
		}
		seite, _ := strconv.Atoi(r.FormValue("seite"))
		writePage(w, http.StatusOK, dieselbeSeite(r, cfg, seite, said, problem))
	}
}

// dieselbeAntworten passes the answer on and says how it ended.
func dieselbeAntworten(ctx context.Context, cfg StartConfig, a, b, antwort string) (said, problem string) {
	ctx, cancel := context.WithTimeout(ctx, dieselbeWait)
	defer cancel()
	name, err := cfg.Gesichter.Antworten(ctx, a, b, antwort)
	var fehlt immich.FehltRecht
	switch {
	case errors.Is(err, gesichter.ErrKeinPaar):
		return "", texte.T(texte.DieselbeWeg)
	case errors.As(err, &fehlt):
		return "", "" // the page's line says which permission
	case errors.Is(err, gesichter.ErrNichtZusammen):
		return "", texte.T(texte.DieselbeNichtGemacht)
	case err != nil:
		log.Printf("homes: dieselbe person: %v", err)
		return "", texte.T(texte.DieselbeStumm)
	case antwort == "ja":
		log.Printf("homes: dieselbe person: merged into %q", name)
		return fmt.Sprintf(texte.T(texte.DieselbeZusammen), name), ""
	case antwort == "nein":
		return texte.T(texte.DieselbeGemerkt), ""
	}
	return texte.T(texte.DieselbeHinten), ""
}

// dieselbeSeite is the page: its line, the outcome, the pairs of seite.
func dieselbeSeite(r *http.Request, cfg StartConfig, seite int, said, problem string) *strings.Builder {
	t := func(id texte.ID) string { return html.EscapeString(texte.T(id)) }
	b := familyHead(r, cfg, texte.T(texte.Dieselbe), "Kontakte")
	b.WriteString(dieselbeStyle + "<h1>" + t(texte.Dieselbe) + "</h1><p class=k>" + t(texte.DieselbeHinweis) + "</p>")
	if recht := cfg.Gesichter.Fehlt(); recht != "" {
		problem = fmt.Sprintf(texte.T(texte.MeineKontakteRecht), recht)
	}
	b.WriteString(gesagt(said, problem))
	paare := cfg.Gesichter.Paare()
	seiten := max(1, (len(paare)+dieselbeJeSeite-1)/dieselbeJeSeite)
	seite = min(max(seite, 1), seiten)
	b.WriteString(istFragen(cfg, seite))
	if len(paare) == 0 {
		b.WriteString("<p class=k>" + t(texte.DieselbeLeer) + "</p>")
	}
	for _, p := range paare[(seite-1)*dieselbeJeSeite : min(len(paare), seite*dieselbeJeSeite)] {
		b.WriteString(`<section class=paar><div class=zwei>` + dieselbeGesicht(p.A) + dieselbeGesicht(p.B) + `</div><form method=post action=` + DieselbePath +
			`><input type=hidden name=a value="` + html.EscapeString(p.A.ID) + `"><input type=hidden name=b value="` + html.EscapeString(p.B.ID) +
			`"><input type=hidden name=seite value=` + strconv.Itoa(seite) + `><button name=antwort value=ja data-laeuft="` + t(texte.DieselbeJaLaeuft) + `">` + t(texte.DieselbeJa) +
			`</button><button name=antwort value=nein data-laeuft="` + t(texte.DieselbeNeinLaeuft) + `">` + t(texte.DieselbeNein) +
			`</button><button name=antwort value=spaeter class=leise data-laeuft="` + t(texte.DieselbeSpaeterLauft) + `">` + t(texte.DieselbeSpaeter) + `</button></form></section>`)
	}
	if seiten > 1 {
		link := func(n int, id texte.ID) string {
			if n < 1 || n > seiten {
				return "<span></span>"
			}
			return `<a href="` + DieselbePath + `?seite=` + strconv.Itoa(n) + `">` + t(id) + `</a>`
		}
		b.WriteString(`<nav class=blaettern>` + link(seite-1, texte.DieselbeZurueck) + `<span class=k>` + html.EscapeString(fmt.Sprintf(texte.T(texte.DieselbeSeiten), seite, seiten)) + `</span>` + link(seite+1, texte.DieselbeWeiter) + `</nav>`)
	}
	return b
}

// dieselbeGesicht is one person of a pair: the face, the name or „ohne
// Namen“, the photos when counted.
func dieselbeGesicht(s gesichter.Seite) string {
	bild := `<span class=ohne aria-hidden=true></span>`
	if s.Bild != nil {
		bild = `<img src="data:image/jpeg;base64,` + base64.StdEncoding.EncodeToString(s.Bild) + `" alt="" width=96 height=96>`
	}
	name := "<i>" + html.EscapeString(texte.T(texte.DieselbeOhneName)) + "</i>"
	if s.Name != "" {
		name = "<b>" + html.EscapeString(s.Name) + "</b>"
	}
	if s.Fotos >= 0 {
		name += "<span>" + html.EscapeString(texte.Anzahl(s.Fotos, "Foto", "Fotos")) + "</span>"
	}
	return "<figure>" + bild + "<figcaption>" + name + "</figcaption></figure>"
}
