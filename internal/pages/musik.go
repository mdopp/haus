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
	"sync"
	"time"

	"github.com/mdopp/homes/internal/einkauf"
	"github.com/mdopp/homes/internal/jellyfin"
	"github.com/mdopp/homes/internal/musik"
	"github.com/mdopp/homes/internal/papierkorb"
	"github.com/mdopp/homes/internal/texte"
)

// Musik is managing the household's music (owner 2026-11-04: „Musik verwalten“, not what is
// playing): how music gets in — shared from the phone to Homes (POST /napi/musik), or laid into
// the inbox on the network drive — and what laib's importer (beets) made of it, the albums
// last added (Jellyfin), what needs someone, and the records (Discogs, the wish list).
const (
	MusikPath     = "/musik"
	MusikBildPath = "/musik/bild" // an album's cover from Jellyfin, ?id=
)

// Musik is the page, its POSTs (tun=nochmal, loeschen, zurueckholen) and the covers.
func Musik(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case cfg.MusicInbox == "" || r.URL.Path != MusikPath && r.URL.Path != MusikBildPath:
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
		case r.URL.Path == MusikBildPath:
			zeigeBild(w, r, cfg.Musikbib != nil, func(ctx context.Context) ([]byte, string, error) {
				return cfg.Musikbib.Bild(ctx, r.URL.Query().Get("id"))
			})
		case r.Method == http.MethodPost:
			musikNochmal(w, r, cfg)
		case showOnly(w, r, "Musik"):
			writeMusik(w, r, cfg, "", "")
		}
	}
}

// writeMusik is the page, with what was just done and its Rückgängig (haus#174): Hinzufügen
// and the import's state under it, Zuletzt hinzugefügt, Braucht Aufmerksamkeit and Gelöscht
// while there is any, Platten, and Discogs quiet at the end. The backends are asked at once.
func writeMusik(w http.ResponseWriter, r *http.Request, cfg StartConfig, said, undo string) {
	ctx, cancel := context.WithTimeout(r.Context(), backendWait)
	defer cancel()
	var (
		st      beetsStand
		alben   []jellyfin.Album
		anzahl  int
		bibErr  error
		platten string
		wg      sync.WaitGroup
	)
	wg.Go(func() { st = beetsLesen(ctx, cfg) })
	if cfg.Musikbib != nil {
		wg.Go(func() { alben, anzahl, bibErr = cfg.Musikbib.Neueste(ctx, 6) })
	}
	wg.Go(func() { platten = musikPlatten(ctx, r, cfg) })
	wg.Wait()
	kept := zurueck(cfg.MusicInbox)
	b := familyHead(r, cfg, texte.Titel("musik"), "Musik")
	b.WriteString(musikStyle + gekoppeltStyle + keyHintStyle + "<h1>" + html.EscapeString(texte.Titel("musik")) + "</h1>")
	if said != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p>")
		if undo != "" {
			b.WriteString(musikUndoForm(undo))
		}
		b.WriteString("</div>")
	}
	b.WriteString(musikDazu(cfg, st, len(kept) > 0) + musikNeu(cfg, alben, anzahl, bibErr) + nichtUebernommen(kept) + musikGeloescht(cfg) + platten)
	writePage(w, http.StatusOK, b)
}

// beetsStand is what laib's status of beets says of the uploads (laib templates/beets
// README: letzte, the last 20, newest first) and, read here, what is still in the inbox.
type beetsStand struct {
	Letzte []struct {
		At      time.Time `json:"at"`
		Eingang string    `json:"eingang"`
		Titel   int       `json:"titel"`
		Alben   int       `json:"alben"`
		Nicht   []struct {
			Grund string `json:"grund"`
		} `json:"nicht_importiert"`
	} `json:"letzte"`
	wartet []string  // the uploads at the inbox's top or in .in-arbeit/, not taken yet
	seit   time.Time // when the oldest of them came
}

// beetsLesen is the uploads' state: beets' own status from laib (nothing when laib does not
// say) and the inbox's entries the importer has not finished.
func beetsLesen(ctx context.Context, cfg StartConfig) beetsStand {
	var st beetsStand
	if cfg.Laib != nil {
		if _, own, err := cfg.Laib.ServiceStatus(ctx, "beets"); err != nil {
			log.Printf("homes: musik: laib status beets: %v", err)
		} else if len(own) > 0 {
			json.Unmarshal(own, &st)
		}
	}
	for _, dir := range []string{"", ".in-arbeit/"} {
		es, _ := os.ReadDir(cfg.MusicInbox + "/" + dir)
		for _, e := range es {
			fi, err := e.Info()
			if err != nil || strings.HasPrefix(e.Name(), ".") || e.Name() == keptBack {
				continue
			}
			st.wartet = append(st.wartet, e.Name())
			if st.seit.IsZero() || fi.ModTime().Before(st.seit) {
				st.seit = fi.ModTime()
			}
		}
	}
	return st
}

// musikDazu is Hinzufügen: how music gets in, from the phone and, with the network drive, from
// a computer at home; under it the state, a line each: what is being sorted in (the page looks
// again every ten seconds meanwhile, and says so when it takes long), and the last week's
// uploads, at most three — done, or not sorted in with why and where to act.
func musikDazu(cfg StartConfig, st beetsStand, kept bool) string {
	var b strings.Builder
	b.WriteString(`<section class="karte musik-dazu" aria-labelledby=m-h><h2 id=m-h>` + html.EscapeString(texte.T(texte.MusikDazu)) +
		`</h2><div class=wie><p>` + html.EscapeString(texte.T(texte.MusikHandy)) + `</p>`)
	if cfg.Samba != nil {
		b.WriteString(`<p>` + strings.Replace(html.EscapeString(texte.T(texte.MusikComputer)), "Netzlaufwerk", `<a href=`+netzlaufwerkPath+`>Netzlaufwerk</a>`, 1) + `</p>`)
	}
	b.WriteString(`</div>`)
	now, loc := cfg.clock()
	if len(st.wartet) > 0 {
		line := fmt.Sprintf(texte.T(texte.MusikWirdEinsortiert), namenListe(st.wartet))
		if now.Sub(st.seit) > 10*time.Minute {
			line += " " + texte.T(texte.MusikDauert)
		}
		b.WriteString(zustand("gelb", line) + `<span hidden data-auffrischen=10></span>`)
	}
	for i, u := range st.Letzte {
		if i == 3 || now.Sub(u.At) > 7*24*time.Hour {
			break
		}
		when := " · " + whenWords(u.At, now, loc)
		if u.Titel > 0 {
			what := count(u.Titel, "Titel", "Titel")
			if u.Alben > 0 {
				what = count(u.Alben, "Album", "Alben") + ", " + what
			}
			b.WriteString(zustand("", fmt.Sprintf(texte.T(texte.MusikFertig), u.Eingang, what)+when))
		}
		if len(u.Nicht) > 0 {
			why := u.Nicht[0].Grund
			if len(u.Nicht) > 1 {
				why = count(len(u.Nicht), "Datei", "Dateien")
			}
			line := fmt.Sprintf(texte.T(texte.MusikNicht), u.Eingang, why) + when
			if kept {
				line += ". " + texte.T(texte.MusikNichtUnten)
			}
			b.WriteString(zustand("rot", line))
		}
	}
	b.WriteString(`</section>`)
	return b.String()
}

// namenListe is up to three names in quotes, „und N weitere“ after.
func namenListe(names []string) string {
	sort.Strings(names)
	out := "„" + strings.Join(names[:min(3, len(names))], "“, „") + "“"
	if len(names) > 3 {
		out += " und " + strconv.Itoa(len(names)-3) + " weitere"
	}
	return out
}

// musikNeu is Zuletzt hinzugefügt: the newest albums as tiles, each its cover (or the music's
// picture), name and artist, a link into Jellyfin where its address is known, and how many
// albums the music holds. None without Jellyfin or while the music is empty; Jellyfin silent, a line.
func musikNeu(cfg StartConfig, alben []jellyfin.Album, anzahl int, err error) string {
	if err != nil {
		log.Printf("homes: musik: jellyfin: %v", err)
	} else if len(alben) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<section class="karte musik-neu" aria-labelledby=m-z><h2 id=m-z>` + html.EscapeString(texte.T(texte.MusikZuletzt)) + `</h2><p class=hinweis>`)
	if err != nil {
		return b.String() + html.EscapeString(texte.T(texte.MusikJellyfinStumm)) + `</p></section>`
	}
	b.WriteString(html.EscapeString(fmt.Sprintf(texte.T(texte.MusikInDerMusik), count(anzahl, "Album", "Alben"))) + `</p><ul class=alben>`)
	web := ""
	if m, ok := cfg.Module.Eins("filme"); ok && m.An() && m.Einrichtung != nil {
		web = m.Einrichtung.Web
	}
	for _, a := range alben {
		inner := `<span class=bild aria-hidden=true><svg viewBox="0 0 24 24">` + iconMusik + `</svg></span>`
		if a.Bild {
			inner = `<img class=bild src="` + MusikBildPath + `?id=` + html.EscapeString(a.ID) + `" alt="" loading=lazy>`
		}
		inner += `<span class=name>` + html.EscapeString(a.Name) + `</span><span class=wer>` + html.EscapeString(a.Kuenstler) + `</span>`
		if web != "" {
			inner = `<a href="` + html.EscapeString(web+"/web/#/details?id="+a.ID) + `">` + inner + `</a>`
		}
		b.WriteString(`<li>` + inner + `</li>`)
	}
	return b.String() + `</ul></section>`
}

// zeigeBild answers a cover a page shows (a Discogs record's, a Jellyfin album's): get's bytes,
// kept a day by the browser; none, or not there, 404.
func zeigeBild(w http.ResponseWriter, r *http.Request, ok bool, get func(context.Context) ([]byte, string, error)) {
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !showOnly(w, r, "Das Bild") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	b, ct, err := get(ctx)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(b)
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

// nichtUebernommen is Braucht Aufmerksamkeit: what beets kept back, each with „Noch einmal
// einsortieren“ and „Löschen“; none when there is nothing.
func nichtUebernommen(kept []liegen) string {
	if len(kept) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<section class="karte musik-achtung" aria-labelledby=m-n><h2 id=m-n>` + html.EscapeString(texte.T(texte.MusikAchtung)) + `</h2><p class=hinweis>` +
		html.EscapeString(texte.T(texte.MusikAchtungWie)) + `</p><ul class=musik-liste>`)
	for _, k := range kept {
		b.WriteString("<li><span class=name>" + html.EscapeString(k.name) + "</span>")
		if k.grund != "" {
			b.WriteString("<span class=k>" + html.EscapeString(k.grund) + "</span>")
		}
		hidden := `<input type=hidden name=lauf value="` + html.EscapeString(k.run) + `"><input type=hidden name=name value="` + html.EscapeString(k.name) + `">`
		b.WriteString(`<div class=musik-tun><form method=post action=` + MusikPath + `><input type=hidden name=tun value=nochmal>` + hidden +
			`<button data-laeuft="Legt zurück …">Noch einmal einsortieren</button></form>` +
			`<form method=post action=` + MusikPath + ` data-frage="„` + html.EscapeString(k.name) + `“ endgültig löschen?"><input type=hidden name=tun value=loeschen>` + hidden +
			`<button class=weg data-laeuft="Löscht …">Löschen</button></form></div></li>`)
	}
	b.WriteString("</ul></section>")
	return b.String()
}

// tidyRun removes a run of kept-back uploads left with only its GRUND.txt.
func tidyRun(root *os.Root, run string) {
	if rest, err := fs.ReadDir(root.FS(), keptBack+"/"+run); err == nil && (len(rest) == 0 || (len(rest) == 1 && rest[0].Name() == "GRUND.txt")) {
		root.Remove(keptBack + "/" + run + "/GRUND.txt")
		root.Remove(keptBack + "/" + run)
	}
}

// musikPlatten is Platten (owner 2026-10-26: Platte erfassen and Discogs belong under Musik):
// Platte erfassen while one's Discogs is connected, how many records one has, the music wish
// list with its open wishes; one's Discogs quiet at the page's end. Without the household's
// Discogs app, what is missing and, for an admin, the way to its keys.
func musikPlatten(ctx context.Context, r *http.Request, cfg StartConfig) string {
	if cfg.Wuensche == nil && cfg.Discogs == nil {
		return ""
	}
	var state []string
	b := `<section class="karte musik-platten" aria-labelledby=m-p><h2 id=m-p>` + html.EscapeString(texte.T(texte.MusikPlatten)) + `</h2>`
	if rg, ok := cfg.Discogs.For(r.Header.Get("Remote-User")); ok {
		b += `<p class=wege><a class=knopf href=` + PlattePath + `>` + html.EscapeString(texte.T(texte.MusikPlatteErfassen)) + `</a></p>`
		if n, err := rg.Anzahl(ctx); err == nil {
			state = append(state, html.EscapeString(fmt.Sprintf(texte.T(texte.MusikInSammlung), count(n, "Platte", "Platten"))))
		}
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
		state = append(state, `<a href="`+EinkaufPath+`?liste=`+einkauf.MusikListe.ID()+`">`+html.EscapeString(wish)+`</a>`)
	}
	b += `<p class=stand>` + strings.Join(state, ` · `) + `</p>`
	if cfg.Discogs == nil {
		return b + keyHint(r, cfg, "Discogs", saysDiscogsEnables, publicBase(r, cfg)+MusikPath, discogsKeys) + `</section>`
	}
	if k, ok := cfg.Discogs.Konto(r.Header.Get("Remote-User")); ok {
		return b + `</section><p class=ruhig><span class=punkt></span><a href=` + DiscogsPath + `>` + html.EscapeString(fmt.Sprintf(texte.T(texte.MusikDiscogsVerbunden), k.Name)) + `</a></p>`
	}
	return b + `</section><p class=ruhig><span class="punkt grau"></span><a href=` + DiscogsPath + `>` + html.EscapeString(texte.T(texte.MusikDiscogsVerbinden)) + `</a></p>`
}

// musikStyle: Start's tiles' look (homes-android#245): no outlines, the card's colour, round
// corners — for the how, the albums and what needs someone; the state's lines as #268's.
const musikStyle = `<style>.musik-dazu .wie{padding:14px 16px;border-radius:20px;background:var(--card)}.musik-dazu .wie p{margin:0}.musik-dazu .wie p+p{margin-top:8px}.musik-dazu .wie a{color:var(--accent)}
.musik-dazu .zustand{margin:10px 2px 0;align-items:flex-start;overflow-wrap:anywhere}.punkt.rot{background:var(--danger)}.karte .hinweis{margin:-2px 2px 10px}
.karte ul.alben,.karte ul.musik-liste{display:grid;grid-template-columns:repeat(auto-fill,minmax(104px,1fr));gap:8px;background:none;border:0;border-radius:0}.karte ul.musik-liste{grid-template-columns:1fr}
.karte .alben li,.karte .musik-liste li{padding:8px;border:0;border-radius:20px;background:var(--card)}.alben .bild{display:grid;place-items:center;width:100%;aspect-ratio:1;border-radius:12px;object-fit:cover;background:var(--accent-soft);color:var(--accent)}
.alben .bild svg{width:40%;fill:none;stroke:currentColor;stroke-width:1.6}.alben .name,.alben .wer{display:block;margin-top:8px;font-size:.9rem;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.alben .wer{margin-top:0;font-weight:400;font-size:.78rem;color:var(--muted)}
.karte .musik-liste li{padding:12px 14px}.musik-liste .name{display:block;font-weight:600}.musik-tun{display:flex;flex-wrap:wrap;gap:8px;margin:10px 0 0}.musik-tun form{margin:0}.musik-tun .weg{background:var(--danger-soft);color:var(--danger)}
.musik-platten .stand{margin:12px 2px 0;color:var(--muted)}.musik-platten .stand a{color:var(--accent)}</style>`

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

// musikZurueck puts a deleted upload back under Braucht Aufmerksamkeit, where it was.
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
	writeMusik(w, r, cfg, "„"+d.Name+"“ ist wieder da, unter „"+texte.T(texte.MusikAchtung)+"“.", "")
}

// musikGeloescht is the uploads deleted in the last 30 days, each with
// Rückgängig; what is older leaves the inbox's Papierkorb.
func musikGeloescht(cfg StartConfig) string {
	if cfg.Papierkorb == nil {
		return ""
	}
	es := cfg.Papierkorb.Liste("musik")
	if root, err := os.OpenRoot(cfg.MusicInbox); err == nil { // what has no entry any more goes
		keep := map[string]bool{}
		for _, e := range es {
			keep[e.ID] = true
		}
		if ds, err := fs.ReadDir(root.FS(), musikKorb); err == nil {
			for _, d := range ds {
				if !keep[d.Name()] {
					root.RemoveAll(musikKorb + "/" + d.Name())
				}
			}
		}
		root.Close()
	}
	if len(es) == 0 {
		return ""
	}
	_, loc := cfg.clock()
	var b strings.Builder
	b.WriteString(`<section class=karte aria-labelledby=m-g><h2 id=m-g>Gelöscht</h2><p class=k>Noch 30 Tage zurückzuholen.</p><ul class=musik-liste>`)
	for _, e := range es {
		b.WriteString(`<li><span class=name>` + html.EscapeString(e.Name) + `</span><span class=k>bis ` + e.Bis().In(loc).Format("02.01.") + `</span><div class=musik-tun>` + musikUndoForm(e.ID) + `</div></li>`)
	}
	b.WriteString(`</ul></section>`)
	return b.String()
}
