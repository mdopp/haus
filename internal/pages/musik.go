package pages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/discogs"
	"github.com/mdopp/homes/internal/einkauf"
	"github.com/mdopp/homes/internal/musik"
	"github.com/mdopp/homes/internal/papierkorb"
	"github.com/mdopp/homes/internal/texte"
)

// Musik is managing the household's music (owner 2026-11-04: „Musik verwalten“, not what is
// playing; „nur Dateien darstellen, aber nichts Komplexes“): how music gets in — shared from
// the phone to Homes (POST /napi/musik), or laid into the inbox on the network drive — and
// one list of the files and what laib's importer (beets) made of them; then the records.
const MusikPath = "/musik"

// Musik is the page and its POSTs (tun=nochmal, loeschen, zurueckholen).
func Musik(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case cfg.MusicInbox == "" || r.URL.Path != MusikPath:
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
		case r.Method == http.MethodPost:
			musikNochmal(w, r, cfg)
		case showOnly(w, r, "Musik"):
			writeMusik(w, r, cfg, "", "")
		}
	}
}

// writeMusik is the page, with what was just done and its Rückgängig (haus#174): Hinzufügen
// with the files under it, Platten, and one's Discogs quiet at the end.
func writeMusik(w http.ResponseWriter, r *http.Request, cfg StartConfig, said, undo string) {
	ctx, cancel := context.WithTimeout(r.Context(), backendWait)
	defer cancel()
	musikKorbLeeren(cfg)
	b := familyHead(r, cfg, texte.Titel("musik"), "Musik")
	b.WriteString(musikStyle + keyHintStyle + "<h1>" + html.EscapeString(texte.Titel("musik")) + "</h1>")
	if said != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p>")
		if undo != "" {
			b.WriteString(musikUndoForm(undo))
		}
		b.WriteString("</div>")
	}
	b.WriteString(musikDazu(cfg, beetsLesen(ctx, cfg)) + musikPlatten(ctx, r, cfg))
	writePage(w, http.StatusOK, b)
}

// beetsStand is what laib's status of beets says of the uploads sorted in (laib
// templates/beets README: letzte, the last 20, newest first) and, read here, what is still
// in the inbox and what beets kept back.
type beetsStand struct {
	Letzte []struct {
		At      time.Time `json:"at"`
		Eingang string    `json:"eingang"`
		Titel   int       `json:"titel"`
		Alben   int       `json:"alben"`
	} `json:"letzte"`
	wartet []os.FileInfo // the uploads at the inbox's top or in .in-arbeit/, not taken yet
	kept   []liegen      // what beets kept back
}

// beetsLesen is the files' state: beets' own status from laib (nothing when laib does not
// say), the inbox's entries the importer has not finished, and what it kept back.
func beetsLesen(ctx context.Context, cfg StartConfig) beetsStand {
	var st beetsStand
	if cfg.Laib != nil {
		if _, own, err := cfg.Laib.ServiceStatus(ctx, "beets"); err != nil {
			log.Printf("homes: musik: laib status beets: %v", err)
		} else if len(own) > 0 {
			json.Unmarshal(own, &st)
		}
	}
	st.wartet, st.kept = musikWartet(cfg.MusicInbox), zurueck(cfg.MusicInbox)
	return st
}

// musikWartet are the uploads at the inbox's top or in .in-arbeit/, not taken yet.
func musikWartet(inbox string) (out []os.FileInfo) {
	for _, dir := range []string{"", ".in-arbeit/"} {
		es, _ := os.ReadDir(inbox + "/" + dir)
		for _, e := range es {
			if fi, err := e.Info(); err == nil && !strings.HasPrefix(e.Name(), ".") && e.Name() != keptBack {
				out = append(out, fi)
			}
		}
	}
	return out
}

// musikDazu is Hinzufügen: how music gets in, from the phone and, with the network drive,
// from a computer at home; under it one list of the files: being sorted in (the page looks
// again every ten seconds meanwhile, and says when it takes long), not sorted in with beets'
// reason and „Noch einmal einsortieren“ and „Löschen“ on its row, and the last week's done.
func musikDazu(cfg StartConfig, st beetsStand) string {
	var b strings.Builder
	b.WriteString(`<section class="karte musik-dazu" aria-labelledby=m-h><h2 id=m-h>` + html.EscapeString(texte.T(texte.MusikDazu)) +
		`</h2><div class=wie><p>` + html.EscapeString(texte.T(texte.MusikHandy)) + `</p>`)
	if cfg.Samba != nil {
		b.WriteString(`<p>` + strings.Replace(html.EscapeString(texte.T(texte.MusikComputer)), "Netzlaufwerk", `<a href=`+netzlaufwerkPath+`>Netzlaufwerk</a>`, 1) + `</p>`)
	}
	b.WriteString(`</div><ul class=musik-liste>`)
	row := func(punkt, name, state, tun string) {
		b.WriteString(`<li><span class="punkt ` + punkt + `"></span><div><span class=name>` + html.EscapeString(name) + `</span><span class=k>` + html.EscapeString(state) + `</span>` + tun + `</div></li>`)
	}
	now, loc := cfg.clock()
	for _, fi := range st.wartet {
		state := texte.T(texte.MusikWirdEinsortiert)
		if now.Sub(fi.ModTime()) > 10*time.Minute {
			state += " " + texte.T(texte.MusikDauert)
		}
		row("gelb", fi.Name(), state, "")
	}
	for _, k := range st.kept {
		hidden := `<input type=hidden name=lauf value="` + html.EscapeString(k.run) + `"><input type=hidden name=name value="` + html.EscapeString(k.name) + `">`
		row("rot", k.name, strings.TrimSuffix(texte.T(texte.MusikNicht)+": "+k.grund, ": "), `<div class=musik-tun><form method=post action=`+MusikPath+
			`><input type=hidden name=tun value=nochmal>`+hidden+`<button data-laeuft="Legt zurück …">Noch einmal einsortieren</button></form><form method=post action=`+MusikPath+
			` data-frage="„`+html.EscapeString(k.name)+`“ endgültig löschen?"><input type=hidden name=tun value=loeschen>`+hidden+`<button class=weg data-laeuft="Löscht …">Löschen</button></form></div>`)
	}
	for i, u := range st.Letzte {
		if i == 5 || now.Sub(u.At) > 7*24*time.Hour {
			break
		}
		if u.Titel > 0 {
			what := count(u.Titel, "Titel", "Titel")
			if u.Alben > 0 {
				what = count(u.Alben, "Album", "Alben") + ", " + what
			}
			row("", u.Eingang, fmt.Sprintf(texte.T(texte.MusikFertig), what)+" · "+whenWords(u.At, now, loc), "")
		}
	}
	b.WriteString(`</ul>`)
	if len(st.wartet) > 0 {
		b.WriteString(`<span hidden data-auffrischen=10></span>`)
	}
	b.WriteString(`</section>`)
	return b.String()
}

// musikNochmal puts an upload beets kept back into the inbox again, under
// its own name (beside one there already), for who asked: the importer
// takes it in about a minute; or (tun=loeschen) deletes it. A run left
// with only its GRUND.txt goes.
func musikNochmal(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	if !fromHomes(r) {
		http.Error(w, "Das geht nur von Homes' eigener Seite.", http.StatusForbidden)
		return
	}
	r.ParseForm()
	run, name, tun := r.PostForm.Get("lauf"), r.PostForm.Get("name"), r.PostForm.Get("tun")
	if tun == "zurueckholen" { // haus#174
		musikZurueck(w, r, cfg, r.PostForm.Get("id"))
		return
	}
	if (tun != "nochmal" && tun != "loeschen") || !plainName(run) || !plainName(name) || name == "GRUND.txt" {
		http.Error(w, "Diese Datei gibt es hier nicht.", http.StatusBadRequest)
		return
	}
	root, err := os.OpenRoot(cfg.MusicInbox)
	if err != nil {
		http.Error(w, "Der Musik-Eingang ist gerade nicht erreichbar.", http.StatusServiceUnavailable)
		return
	}
	defer root.Close()
	from := keptBack + "/" + run + "/" + name
	if _, err := root.Lstat(from); err != nil {
		http.Error(w, "Diese Datei liegt nicht mehr unter „Nicht übernommen“.", http.StatusConflict)
		return
	}
	if tun == "loeschen" { // the owner, 2026-10-26: what can be put back can be deleted
		var undo string
		if cfg.Papierkorb != nil { // haus#174: into the inbox's hidden Papierkorb for 30 days; the importer never touches a dot folder
			e, err := cfg.Papierkorb.Rein("musik", name, r.Header.Get("Remote-User"), musikImKorb{Lauf: run, Name: name})
			if err == nil {
				root.MkdirAll(musikKorb+"/"+e.ID, 0o700)
				err = root.Rename(from, musikKorb+"/"+e.ID+"/"+name)
				if err != nil {
					cfg.Papierkorb.Raus(e.ID)
				}
			}
			if err != nil {
				log.Printf("homes: musik: deleting %s: %v", from, err)
				http.Error(w, "Die Datei ließ sich gerade nicht löschen; sie bleibt. Versuch es gleich noch einmal.", http.StatusInternalServerError)
				return
			}
			undo = e.ID
		} else if err := root.RemoveAll(from); err != nil {
			log.Printf("homes: musik: deleting %s: %v", from, err)
			http.Error(w, "Die Datei ließ sich nicht löschen.", http.StatusInternalServerError)
			return
		}
		tidyRun(root, run)
		log.Printf("homes: musik: %s deleted %s", r.Header.Get("Remote-User"), from)
		writeMusik(w, r, cfg, "„"+name+"“ ist gelöscht.", undo)
		return
	}
	to := name
	for i := 2; ; i++ {
		if _, err := root.Lstat(to); errors.Is(err, fs.ErrNotExist) {
			break
		}
		ext := path.Ext(name)
		to = strings.TrimSuffix(name, ext) + " (" + strconv.Itoa(i) + ")" + ext
		if i > 99 {
			http.Error(w, "Diese Datei gibt es im Eingang schon zu oft.", http.StatusConflict)
			return
		}
	}
	if who := musik.Uploader(r.Header.Get("Remote-User")); who != "" {
		if f, err := root.OpenFile("._"+to+".von", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o664); err == nil {
			f.WriteString(who + "\n")
			f.Close()
		}
	}
	if err := root.Rename(from, to); err != nil {
		log.Printf("homes: musik: again %s: %v", from, err)
		http.Error(w, "Die Datei ließ sich nicht zurücklegen.", http.StatusInternalServerError)
		return
	}
	tidyRun(root, run)
	log.Printf("homes: musik: %s put %s back into the inbox", r.Header.Get("Remote-User"), name)
	writeMusik(w, r, cfg, "„"+name+"“ liegt wieder im Musik-Eingang. Die Box sortiert es in etwa einer Minute ein.", "")
}

// plainName is one path part, no folder, nothing hidden or leaving.
func plainName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00") && !strings.HasPrefix(s, ".") && len(s) <= 255
}

// keptBack is the importer's folder for what it did not take (laib
// templates/beets: _nicht-importiert/<Datum Uhrzeit>/, GRUND.txt beside).
const keptBack = "_nicht-importiert"

// liegen is one upload beets kept back: where, its name, why.
type liegen struct {
	run, name, grund string
}

// zurueck are the uploads beets kept back, the newest run first, at most
// 20, each with the reason GRUND.txt names.
func zurueck(inbox string) []liegen {
	root, err := os.OpenRoot(inbox)
	if err != nil {
		return nil
	}
	defer root.Close()
	runs, err := fs.ReadDir(root.FS(), keptBack)
	if err != nil {
		return nil
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Name() > runs[j].Name() })
	var out []liegen
	for _, run := range runs {
		if !run.IsDir() || strings.HasPrefix(run.Name(), ".") {
			continue
		}
		reasons := map[string]string{}
		if b, err := fs.ReadFile(root.FS(), keptBack+"/"+run.Name()+"/GRUND.txt"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				n, g, ok := strings.Cut(l, ": ")
				if !ok {
					continue
				}
				n, g = strings.TrimSpace(n), strings.TrimSpace(g)
				if top, rest, sub := strings.Cut(n, "/"); sub { // a file inside an upload's folder: the folder's reason names it
					g = rest + ": " + g
					if reasons[top] != "" {
						g = reasons[top] + "; " + g
					}
					n = top
				}
				reasons[n] = g
			}
		}
		items, _ := fs.ReadDir(root.FS(), keptBack+"/"+run.Name())
		for _, it := range items {
			if it.Name() == "GRUND.txt" || strings.HasPrefix(it.Name(), ".") {
				continue
			}
			out = append(out, liegen{run: run.Name(), name: it.Name(), grund: reasons[it.Name()]})
			if len(out) == 20 {
				return out
			}
		}
	}
	return out
}

// tidyRun removes a run of kept-back uploads left with only its GRUND.txt.
func tidyRun(root *os.Root, run string) {
	if rest, err := fs.ReadDir(root.FS(), keptBack+"/"+run); err == nil && (len(rest) == 0 || (len(rest) == 1 && rest[0].Name() == "GRUND.txt")) {
		root.Remove(keptBack + "/" + run + "/GRUND.txt")
		root.Remove(keptBack + "/" + run)
	}
}

// musikPlatten is Platten (owner 2026-10-26: Platte erfassen and Discogs belong under Musik;
// 2026-11-04: by whether one's Discogs is connected). Connected: Platte erfassen, and one's
// Discogs quiet at the page's end; not: only the quiet way to connect it and what it brings.
// The music wish list with its open wishes either way: it is Homes' own, Discogs only adds the
// wantlist. Without the household's Discogs app, for an admin the way to its keys.
func musikPlatten(ctx context.Context, r *http.Request, cfg StartConfig) string {
	var b strings.Builder
	k, connected := discogs.Konto{}, false
	if cfg.Discogs != nil {
		k, connected = cfg.Discogs.Konto(r.Header.Get("Remote-User"))
	}
	if connected {
		b.WriteString(`<p class=wege><a class=knopf href=` + PlattePath + `>` + html.EscapeString(texte.T(texte.MusikPlatteErfassen)) + `</a></p>`)
	}
	if cfg.Wuensche != nil {
		wish := texte.T(texte.MusikWunschliste)
		if items, err := cfg.Wuensche.Listen.Andere(einkauf.MusikListe).Lesen(ctx); err == nil {
			open := 0
			for _, it := range items {
				open += map[bool]int{false: 1}[it.Done]
			}
			wish += " · " + strconv.Itoa(open)
		}
		b.WriteString(`<p class=stand><a href="` + EinkaufPath + `?liste=` + einkauf.MusikListe.ID() + `">` + html.EscapeString(wish) + `</a></p>`)
	}
	switch {
	case cfg.Discogs == nil && isAdmin(r):
		b.WriteString(keyHint(r, cfg, "Discogs", saysDiscogsEnables, publicBase(r, cfg)+MusikPath, discogsKeys))
	case cfg.Discogs != nil && !connected:
		b.WriteString(`<p class=ruhig><span class="punkt grau"></span><span><a href=` + DiscogsPath + `>` + html.EscapeString(texte.T(texte.MusikDiscogsVerbinden)) +
			`</a> – ` + html.EscapeString(texte.T(texte.MusikDiscogsBringt)) + `</span></p>`)
	}
	if b.Len() == 0 {
		return ""
	}
	out := `<section class="karte musik-platten" aria-labelledby=m-p><h2 id=m-p>` + html.EscapeString(texte.T(texte.MusikPlatten)) + `</h2>` + b.String() + `</section>`
	if connected {
		out += `<p class=ruhig><span class=punkt></span><a href=` + DiscogsPath + `>` + html.EscapeString(fmt.Sprintf(texte.T(texte.MusikDiscogsVerbunden), k.Name)) + `</a></p>`
	}
	return out
}

// musikStyle: Start's tiles' look (homes-android#245) — no outlines, the card's colour, round
// corners — for the how and the files; a file's state in a dot and a quiet line.
const musikStyle = `<style>.musik-dazu .wie{padding:14px 16px;border-radius:20px;background:var(--card)}.musik-dazu .wie p{margin:0}.musik-dazu .wie p+p{margin-top:8px}.musik-dazu .wie a{color:var(--accent)}
.karte ul.musik-liste{display:grid;gap:8px;margin:10px 0 0;background:none;border:0;border-radius:0}.karte .musik-liste li{display:flex;gap:10px;padding:12px 14px;border:0;border-radius:20px;background:var(--card)}
.musik-liste li>div{min-width:0}.musik-liste .name{display:block;font-weight:600;overflow-wrap:anywhere}.punkt.rot{background:var(--danger)}.musik-liste .punkt:not(.gelb):not(.rot){background:var(--ok)}
.musik-tun{display:flex;flex-wrap:wrap;gap:8px;margin:10px 0 0}.musik-tun form{margin:0}.musik-tun .weg{background:var(--danger-soft);color:var(--danger)}
.musik-platten .stand{margin:12px 2px 0}.musik-platten .stand a{color:var(--accent)}.musik-platten .ruhig{margin:12px 2px 0}.musik-platten .ruhig a{color:var(--accent)}</style>`

// musikKorb is the inbox's hidden Papierkorb (haus#174): a dot folder, which
// the importer never touches; each deleted upload in <id>/ for 30 days.
const musikKorb = ".papierkorb"

// musikImKorb is where a deleted kept-back upload came from.
type musikImKorb struct {
	Lauf string `json:"lauf"`
	Name string `json:"name"`
}

func musikUndoForm(id string) string {
	return `<form method=post action=` + MusikPath + `><input type=hidden name=tun value=zurueckholen><input type=hidden name=id value=` + id +
		`><button data-laeuft="Holt zurück …">Rückgängig</button></form>`
}

// musikZurueck puts a deleted upload back among the files not sorted in, where it was.
func musikZurueck(w http.ResponseWriter, r *http.Request, cfg StartConfig, id string) {
	var e papierkorb.Eintrag
	var d musikImKorb
	err := papierkorb.ErrWeg
	if cfg.Papierkorb != nil {
		if e, err = cfg.Papierkorb.Holen("musik", id); err == nil {
			err = json.Unmarshal(e.Daten, &d)
		}
	}
	if err != nil || !plainName(d.Lauf) || !plainName(d.Name) {
		http.Error(w, papierkorb.ErrWeg.Error(), http.StatusConflict)
		return
	}
	root, err := os.OpenRoot(cfg.MusicInbox)
	if err != nil {
		http.Error(w, "Der Musik-Eingang ist gerade nicht erreichbar.", http.StatusServiceUnavailable)
		return
	}
	defer root.Close()
	root.MkdirAll(keptBack+"/"+d.Lauf, 0o755)
	if err := root.Rename(musikKorb+"/"+e.ID+"/"+d.Name, keptBack+"/"+d.Lauf+"/"+d.Name); err != nil {
		log.Printf("homes: musik: zurück %s: %v", d.Name, err)
		http.Error(w, "„"+d.Name+"“ ließ sich gerade nicht zurückholen. Versuch es gleich noch einmal.", http.StatusConflict)
		return
	}
	root.Remove(musikKorb + "/" + e.ID)
	cfg.Papierkorb.Raus(e.ID)
	log.Printf("homes: musik: %s put %s back", r.Header.Get("Remote-User"), d.Name)
	writeMusik(w, r, cfg, "„"+d.Name+"“ ist wieder da.", "")
}

// musikKorbLeeren lets go of what was deleted more than 30 days ago: the inbox's Papierkorb
// keeps a file only while its entry does (haus#174).
func musikKorbLeeren(cfg StartConfig) {
	root, err := os.OpenRoot(cfg.MusicInbox)
	if cfg.Papierkorb == nil || err != nil {
		return
	}
	defer root.Close()
	keep := map[string]bool{}
	for _, e := range cfg.Papierkorb.Liste("musik") {
		keep[e.ID] = true
	}
	ds, _ := fs.ReadDir(root.FS(), musikKorb)
	for _, d := range ds {
		if !keep[d.Name()] {
			root.RemoveAll(musikKorb + "/" + d.Name())
		}
	}
}
