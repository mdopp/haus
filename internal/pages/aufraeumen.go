package pages

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
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

	"github.com/mdopp/haus/internal/merken"
)

// Musik aufräumen (owner 2026-10-26: "sortiere du sie mal anhand der
// Metadaten" and "halte mal fest ob die Alben wirklich gut sind … Bitte
// aufräumen"): a plan, written beside Haus's data by whoever looked at the
// music (musik-aufraeumen.json), proposes for each song beets could not
// place (music/_unklar) to delete it, to put it back into the inbox under a
// better name, or to look at it; and for each album its doubles, its live
// songs among the studio ones and what came from a video site. Nothing
// happens on its own: an admin presses each step, deleting asks first.
// Only what the plan names can be touched, and only inside the music.
const MusikAufraeumenPath = "/musik/aufraeumen"

// musikPlanFile and musikDoneFile live in Haus's data folder.
const (
	musikPlanFile = "musik-aufraeumen.json"
	musikDoneFile = "musik-aufraeumen-erledigt.json"
)

// musikPlan is the proposal.
type musikPlan struct {
	Stand  string         `json:"stand"`
	Unklar []planUnklar   `json:"unklar"`
	Alben  []planAlbum    `json:"alben"`
	byPath map[string]int // unklar by path
}

// planUnklar is one song (or a folder, Ordner) under _unklar: its group
// (1 delete, 2 back into the inbox, 3 look at it), why, the name it goes
// back as ("Interpret - Titel"), and the wish it stands for.
type planUnklar struct {
	Pfad   string `json:"pfad"`
	Gruppe int    `json:"gruppe"`
	Grund  string `json:"grund"`
	Name   string `json:"name,omitempty"`
	Wunsch string `json:"wunsch,omitempty"`
	Ordner bool   `json:"ordner,omitempty"`
}

// planAlbum is an album folder (Interpret/Album) and what is wrong in it.
type planAlbum struct {
	Album   string       `json:"album"`
	Titel   int          `json:"titel"`
	Befunde []planBefund `json:"befunde"`
}

// planBefund is one file of an album: doppelt (Behalten is the copy that
// stays), live, or netz (a download of a video).
type planBefund struct {
	Datei    string `json:"datei"`
	Art      string `json:"art"`
	Behalten string `json:"behalten,omitempty"`
}

// musikErledigt is what was done with an entry, so the page leaves it out.
type musikErledigt struct {
	Was string    `json:"was"`
	Wer string    `json:"wer"`
	Am  time.Time `json:"am"`
}

var musikDoneMu sync.Mutex

func loadMusikPlan(cfg StartConfig) (*musikPlan, error) {
	b, err := os.ReadFile(path.Join(cfg.DataDir, musikPlanFile))
	if err != nil {
		return nil, err
	}
	var p musikPlan
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	p.byPath = map[string]int{}
	for i, u := range p.Unklar {
		p.byPath[u.Pfad] = i
	}
	return &p, nil
}

func loadMusikDone(cfg StartConfig) map[string]musikErledigt {
	out := map[string]musikErledigt{}
	if b, err := os.ReadFile(path.Join(cfg.DataDir, musikDoneFile)); err == nil {
		json.Unmarshal(b, &out)
	}
	return out
}

func markMusikDone(cfg StartConfig, keys []string, was, wer string) {
	musikDoneMu.Lock()
	defer musikDoneMu.Unlock()
	all := loadMusikDone(cfg)
	for _, k := range keys {
		all[k] = musikErledigt{Was: was, Wer: wer, Am: time.Now().UTC()}
	}
	b, _ := json.MarshalIndent(all, "", " ")
	tmp := path.Join(cfg.DataDir, "."+musikDoneFile+".neu")
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, path.Join(cfg.DataDir, musikDoneFile))
	}
}

// musicPath is a path inside the music as the plan writes it: relative,
// no part empty, hidden or leaving.
func musicPath(p string) bool {
	if p == "" || len(p) > 1024 || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." || strings.HasPrefix(s, ".") {
			return false
		}
	}
	return true
}

// MusikAufraeumen is the page and its steps.
func MusikAufraeumen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != MusikAufraeumenPath || cfg.MusicDir == "" || cfg.DataDir == "" {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		if !isAdmin(r) {
			missing{http.StatusForbidden, "Die Musik aufräumen dürfen nur Admins.", "Mehr", MusikPath, "Musik hinzufügen"}.write(w, r, cfg)
			return
		}
		if r.Method == http.MethodPost {
			musikAufraeumenTun(w, r, cfg)
			return
		}
		if !showOnly(w, r, "Musik aufräumen") {
			return
		}
		writeAufraeumen(w, r, cfg, "", false)
	}
}

// open is whether an entry still waits: not done, and still there.
func (p *musikPlan) open(root *os.Root, done map[string]musikErledigt, key, file string) bool {
	if _, ok := done[key]; ok {
		return false
	}
	_, err := root.Lstat(file)
	return err == nil
}

func writeAufraeumen(w http.ResponseWriter, r *http.Request, cfg StartConfig, said string, failed bool) {
	b := familyHead(r, cfg, "Musik aufräumen", "Musik hinzufügen")
	b.WriteString(musikStyle + aufraeumenStyle + `<h1>Musik aufräumen</h1>`)
	if said != "" {
		cls := "gut"
		if failed {
			cls = "schlecht"
		}
		b.WriteString("<div class=" + cls + " role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p></div>")
	}
	p, err := loadMusikPlan(cfg)
	root, rerr := os.OpenRoot(cfg.MusicDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		b.WriteString(`<p class=k>Noch kein Vorschlag zum Aufräumen. Claude legt ihn an, wenn du darum bittest.</p>`)
	case err != nil:
		log.Printf("haus: musik aufräumen: the plan: %v", err)
		b.WriteString(`<p class=k>Der Vorschlag zum Aufräumen ist nicht lesbar. Bitte Claude, ihn neu anzulegen.</p>`)
	case rerr != nil:
		b.WriteString(`<p class=k>Haus sieht die Musiksammlung gerade nicht. Laib muss sie Haus freigeben; bis dahin geht hier nichts.</p>`)
	default:
		defer root.Close()
		done := loadMusikDone(cfg)
		b.WriteString(`<p class=k>Vorschlag vom ` + html.EscapeString(p.Stand) + `. Nichts passiert von selbst: Du drückst jeden Schritt, Löschen fragt vorher. Was erledigt ist, verschwindet hier.</p>`)
		b.WriteString(unklarKarten(p, root, done))
		b.WriteString(albenKarte(p, root, done))
	}
	b.WriteString(`<p><a href="` + MusikPath + `">Zurück zu Musik hinzufügen</a></p>`)
	writePage(w, http.StatusOK, b)
}

var unklarGruppen = []struct {
	n           int
	titel, satz string
}{
	{1, "Vorschlag: löschen", "Spaß-Remixe, Schnipsel und Namenloses aus Tauschbörsen."},
	{2, "Zurück in den Musik-Eingang", "Interpret und Titel stimmen, nur hat beets sie so nicht erkannt. Sie gehen unter dem richtigen Namen zurück in den Musik-Eingang und werden dann einsortiert."},
	{3, "Prüfen oder ersetzen", "Falsch beschriftet, abgeschnitten oder ohne Daten. Auf die Wunschliste setzen, behalten oder löschen."},
}

func unklarKarten(p *musikPlan, root *os.Root, done map[string]musikErledigt) string {
	var b strings.Builder
	for _, g := range unklarGruppen {
		var items []planUnklar
		for _, u := range p.Unklar {
			if u.Gruppe == g.n && p.open(root, done, u.Pfad, u.Pfad) {
				items = append(items, u)
			}
		}
		if len(items) == 0 {
			continue
		}
		id := "u" + strconv.Itoa(g.n)
		b.WriteString(`<section class=karte aria-labelledby=` + id + `><h2 id=` + id + `>` + html.EscapeString(g.titel) + ` · ` + strconv.Itoa(len(items)) + `</h2><p class=k>` + html.EscapeString(g.satz) + `</p>`)
		switch g.n {
		case 1:
			b.WriteString(aufraeumenForm("gruppe-loeschen", "gruppe", "1", "Alle "+strconv.Itoa(len(items))+" löschen", "Löscht …", "Alle "+strconv.Itoa(len(items))+" Titel dieser Gruppe endgültig löschen?", "weg"))
		case 2:
			b.WriteString(aufraeumenForm("gruppe-eingang", "gruppe", "2", "Alle "+strconv.Itoa(len(items))+" zurück in den Eingang", "Legt zurück …", "", ""))
		}
		b.WriteString(`<ul class=musik-liste>`)
		for _, u := range items {
			b.WriteString(`<li><span class=name>` + html.EscapeString(u.Pfad) + `</span><span class=k>` + html.EscapeString(u.Grund))
			if g.n == 2 && u.Name != "" {
				b.WriteString(` → „` + html.EscapeString(u.Name) + `“`)
			}
			if g.n == 3 && u.Wunsch != "" {
				b.WriteString(` · eigentlich „` + html.EscapeString(u.Wunsch) + `“`)
			}
			b.WriteString(`</span><div class=musik-tun>`)
			if g.n == 2 {
				b.WriteString(aufraeumenForm("eingang", "pfad", u.Pfad, "Zurück in den Eingang", "Legt zurück …", "", ""))
			}
			if g.n == 3 && u.Wunsch != "" {
				b.WriteString(aufraeumenForm("wunsch", "pfad", u.Pfad, "Auf die Wunschliste", "Merkt …", "", ""))
			}
			if g.n != 2 {
				b.WriteString(aufraeumenForm("behalten", "pfad", u.Pfad, "Behalten", "Merkt …", "", ""))
			}
			b.WriteString(aufraeumenForm("loeschen", "pfad", u.Pfad, "Löschen", "Löscht …", "„"+path.Base(u.Pfad)+"“ endgültig löschen?", "weg"))
			b.WriteString(`</div></li>`)
		}
		b.WriteString(`</ul></section>`)
	}
	return b.String()
}

var befundArten = []struct{ art, mehrzahl, knopf, laeuft, frage, klasse string }{
	{"doppelt", "doppelt", "Doppelte löschen", "Löscht …", "Die doppelten Titel dieses Albums endgültig löschen? Die bessere Fassung bleibt.", "weg"},
	{"netz", "aus dem Netz", "Netz-Downloads löschen", "Löscht …", "Die Downloads aus Videoseiten in diesem Album endgültig löschen?", "weg"},
	{"live", "live", "Live-Titel neu einsortieren", "Legt zurück …", "", ""},
}

func albenKarte(p *musikPlan, root *os.Root, done map[string]musikErledigt) string {
	var b strings.Builder
	n, dups := 0, 0
	var rows strings.Builder
	for _, a := range p.Alben {
		if _, ok := done["album:"+a.Album]; ok {
			continue
		}
		count := map[string][]string{}
		for _, f := range a.Befunde {
			if p.open(root, done, a.Album+"/"+f.Datei, a.Album+"/"+f.Datei) {
				count[f.Art] = append(count[f.Art], f.Datei)
			}
		}
		if len(count) == 0 {
			continue
		}
		n++
		dups += len(count["doppelt"])
		var sum []string
		for _, k := range befundArten {
			if c := len(count[k.art]); c > 0 {
				sum = append(sum, strconv.Itoa(c)+" "+k.mehrzahl)
			}
		}
		rows.WriteString(`<li><details><summary><span class=name>` + html.EscapeString(a.Album) + `</span><span class=k>` + strconv.Itoa(a.Titel) + ` Titel · ` + strings.Join(sum, ", ") + `</span></summary><ul class=befunde>`)
		for _, k := range befundArten {
			for _, f := range count[k.art] {
				rows.WriteString(`<li>` + html.EscapeString(k.mehrzahl) + `: ` + html.EscapeString(f))
				for _, x := range a.Befunde {
					if x.Datei == f && x.Behalten != "" {
						rows.WriteString(` <span class=k>(bleibt: ` + html.EscapeString(x.Behalten) + `)</span>`)
					}
				}
				rows.WriteString(`</li>`)
			}
		}
		rows.WriteString(`</ul><div class=musik-tun>`)
		for _, k := range befundArten {
			if len(count[k.art]) > 0 {
				rows.WriteString(aufraeumenForm("album-"+k.art, "album", a.Album, k.knopf, k.laeuft, k.frage, k.klasse))
			}
		}
		rows.WriteString(aufraeumenForm("album-passt", "album", a.Album, "Passt so", "Merkt …", "", ""))
		rows.WriteString(`</div></details></li>`)
	}
	if n == 0 {
		return ""
	}
	b.WriteString(`<section class=karte aria-labelledby=m-a><h2 id=m-a>Alben · ` + strconv.Itoa(n) + `</h2><p class=k>Alben mit doppelten Titeln, mit Live-Titeln zwischen den Studio-Titeln oder mit Downloads aus Videoseiten. Live-Titel gehen zurück in den Musik-Eingang; beets ordnet sie dort dem Live-Album zu, auf das sie gehören. Ob ein Titel gar nicht auf das Album gehört, sehen die Dateinamen nicht. Das findet erst beets beim nächsten Abgleich.</p>`)
	if dups > 0 {
		b.WriteString(aufraeumenForm("alle-doppelten", "", "", "Alle "+strconv.Itoa(dups)+" doppelten Titel löschen", "Löscht …", "Alle "+strconv.Itoa(dups)+" doppelten Titel in allen Alben endgültig löschen? Die bessere Fassung bleibt jeweils.", "weg"))
	}
	b.WriteString(`<ul class="musik-liste alben">` + rows.String() + `</ul></section>`)
	return b.String()
}

func aufraeumenForm(tun, key, val, knopf, laeuft, frage, klasse string) string {
	f := `<form method=post action=` + MusikAufraeumenPath
	if frage != "" {
		f += ` data-frage="` + html.EscapeString(frage) + `"`
	}
	f += `><input type=hidden name=tun value=` + tun + `>`
	if key != "" {
		f += `<input type=hidden name=` + key + ` value="` + html.EscapeString(val) + `">`
	}
	f += `<button`
	if klasse != "" {
		f += ` class=` + klasse
	}
	return f + ` data-laeuft="` + html.EscapeString(laeuft) + `">` + html.EscapeString(knopf) + `</button></form>`
}

// musikAufraeumenTun does one step and shows the page again with what it did.
func musikAufraeumenTun(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	if !fromHaus(r) {
		http.Error(w, "Das geht nur von der Seite selbst.", http.StatusForbidden)
		return
	}
	r.ParseForm()
	p, err := loadMusikPlan(cfg)
	if err != nil {
		http.Error(w, "Es gibt keinen lesbaren Vorschlag zum Aufräumen.", http.StatusConflict)
		return
	}
	root, err := os.OpenRoot(cfg.MusicDir)
	if err != nil {
		http.Error(w, "Haus sieht die Musiksammlung gerade nicht.", http.StatusServiceUnavailable)
		return
	}
	defer root.Close()
	who := r.Header.Get("Remote-User")
	done := loadMusikDone(cfg)
	tun := r.PostForm.Get("tun")
	var said string
	var fail error
	switch tun {
	case "loeschen", "eingang", "wunsch", "behalten":
		pf := r.PostForm.Get("pfad")
		i, ok := p.byPath[pf]
		if !ok || !musicPath(pf) {
			http.Error(w, "Diesen Titel kennt der Vorschlag nicht.", http.StatusBadRequest)
			return
		}
		u := p.Unklar[i]
		if !p.open(root, done, u.Pfad, u.Pfad) {
			http.Error(w, "Das ist schon erledigt oder nicht mehr da.", http.StatusConflict)
			return
		}
		switch tun {
		case "loeschen":
			if fail = root.RemoveAll(u.Pfad); fail == nil {
				markMusikDone(cfg, []string{u.Pfad}, "gelöscht", who)
				log.Printf("haus: musik aufräumen: %s deleted %s", who, u.Pfad)
				said = "„" + path.Base(u.Pfad) + "“ ist gelöscht."
			}
		case "eingang":
			if u.Gruppe != 2 {
				http.Error(w, "Dieser Titel geht nicht so zurück.", http.StatusBadRequest)
				return
			}
			var to string
			if to, fail = toInbox(root, cfg.MusicInbox, u, who); fail == nil {
				markMusikDone(cfg, []string{u.Pfad}, "eingang", who)
				said = "„" + to + "“ liegt im Musik-Eingang. Die Box sortiert es in etwa einer Minute ein."
			}
		case "wunsch":
			said, fail = wishFor(r.Context(), cfg, u, who)
			if fail == nil {
				markMusikDone(cfg, []string{u.Pfad}, "wunsch", who)
			}
		case "behalten":
			markMusikDone(cfg, []string{u.Pfad}, "behalten", who)
			said = "„" + path.Base(u.Pfad) + "“ bleibt, wo es ist."
		}
	case "gruppe-loeschen", "gruppe-eingang":
		g := map[string]int{"gruppe-loeschen": 1, "gruppe-eingang": 2}[tun]
		if r.PostForm.Get("gruppe") != strconv.Itoa(g) {
			http.Error(w, "Diese Gruppe geht so nicht.", http.StatusBadRequest)
			return
		}
		n, failed := 0, 0
		for _, u := range p.Unklar {
			if u.Gruppe != g || !musicPath(u.Pfad) || !p.open(root, done, u.Pfad, u.Pfad) {
				continue
			}
			var err error
			if g == 1 {
				err = root.RemoveAll(u.Pfad)
			} else {
				_, err = toInbox(root, cfg.MusicInbox, u, who)
			}
			if err != nil {
				log.Printf("haus: musik aufräumen: %s: %v", u.Pfad, err)
				failed++
				continue
			}
			markMusikDone(cfg, []string{u.Pfad}, map[int]string{1: "gelöscht", 2: "eingang"}[g], who)
			n++
		}
		log.Printf("haus: musik aufräumen: %s, group %d: %d done, %d failed", who, g, n, failed)
		said = map[int]string{1: count(n, "Titel gelöscht.", "Titel gelöscht."), 2: count(n, "Titel liegt wieder im Musik-Eingang; die Box sortiert ihn ein.", "Titel liegen wieder im Musik-Eingang; die Box sortiert sie nach und nach ein.")}[g]
		if failed > 0 {
			said += " " + count(failed, "Titel ging nicht, er steht noch hier.", "Titel gingen nicht, sie stehen noch hier.")
		}
	case "album-doppelt", "album-netz", "album-live", "album-passt", "alle-doppelten":
		var alben []planAlbum
		for _, a := range p.Alben {
			if tun == "alle-doppelten" || a.Album == r.PostForm.Get("album") {
				alben = append(alben, a)
			}
		}
		if len(alben) == 0 || (tun != "alle-doppelten" && !musicPath(alben[0].Album)) {
			http.Error(w, "Dieses Album kennt der Vorschlag nicht.", http.StatusBadRequest)
			return
		}
		if tun == "album-passt" {
			markMusikDone(cfg, []string{"album:" + alben[0].Album}, "passt", who)
			said = "„" + alben[0].Album + "“ bleibt, wie es ist."
			break
		}
		art := strings.TrimPrefix(strings.TrimPrefix(tun, "album-"), "alle-")
		if art == "doppelten" {
			art = "doppelt"
		}
		n, failed := 0, 0
		for _, a := range alben {
			if !musicPath(a.Album) {
				continue
			}
			if _, ok := done["album:"+a.Album]; ok {
				continue
			}
			kept := map[string]bool{} // a copy that stays is never deleted, whatever else the plan says
			for _, f := range a.Befunde {
				kept[f.Behalten] = true
			}
			for _, f := range a.Befunde {
				file := a.Album + "/" + f.Datei
				if f.Art != art || kept[f.Datei] || !musicPath(file) || strings.Contains(f.Datei, "/") || !p.open(root, done, file, file) {
					continue
				}
				if art == "doppelt" { // the copy that stays must be there, or nothing goes
					if _, err := root.Lstat(a.Album + "/" + f.Behalten); f.Behalten == "" || err != nil {
						continue
					}
				}
				var err error
				if art == "live" {
					_, err = toInbox(root, cfg.MusicInbox, planUnklar{Pfad: file}, who)
				} else {
					err = root.Remove(file)
				}
				if err != nil {
					log.Printf("haus: musik aufräumen: %s: %v", file, err)
					failed++
					continue
				}
				markMusikDone(cfg, []string{file}, art, who)
				n++
			}
		}
		log.Printf("haus: musik aufräumen: %s, %s: %d done, %d failed", who, tun, n, failed)
		if art == "live" {
			said = count(n, "Live-Titel liegt im Musik-Eingang; die Box sortiert ihn ein.", "Live-Titel liegen im Musik-Eingang; die Box sortiert sie ein.")
		} else {
			said = count(n, "Titel gelöscht.", "Titel gelöscht.")
		}
		if failed > 0 {
			said += " " + count(failed, "Titel ging nicht, er steht noch hier.", "Titel gingen nicht, sie stehen noch hier.")
		}
	default:
		http.Error(w, "Diesen Schritt gibt es nicht.", http.StatusBadRequest)
		return
	}
	if fail != nil {
		log.Printf("haus: musik aufräumen: %s %s: %v", tun, r.PostForm.Get("pfad"), fail)
		writeAufraeumen(w, r, cfg, "Das ging nicht: "+plainReason(fail)+" Versuch es noch einmal; geht es wieder nicht, sag Claude Bescheid.", true)
		return
	}
	writeAufraeumen(w, r, cfg, said, false)
}

// plainReason is an error as one German sentence, without paths.
func plainReason(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "Haus darf dort nicht schreiben."
	case errors.Is(err, fs.ErrNotExist):
		return "Die Datei ist nicht mehr da."
	case errors.Is(err, errNoWishes):
		return "Die Musik-Wunschliste ist nicht eingerichtet."
	}
	return "Der Speicher hat nicht mitgespielt."
}

var errNoWishes = errors.New("no wish list")

// wishFor puts what the song should be on the music wish list.
func wishFor(ctx context.Context, cfg StartConfig, u planUnklar, who string) (string, error) {
	if cfg.Wuensche == nil {
		return "", errNoWishes
	}
	artist, title, ok := strings.Cut(u.Wunsch, " – ")
	if !ok {
		artist, title, _ = strings.Cut(u.Wunsch, " - ")
	}
	g, err := cfg.Wuensche.Wuenschen(ctx, merken.Lied{Kuenstler: strings.TrimSpace(artist), Titel: strings.TrimSpace(title)}, who)
	if err != nil {
		return "", err
	}
	return g.Satz, nil
}

// toInbox moves a song (or a folder) from the music into the inbox under
// its plan name, by the inbox's contract: copied as a dot name, renamed
// when complete, the uploader beside it; then the original goes. Its name
// in the inbox.
func toInbox(music *os.Root, inbox string, u planUnklar, who string) (string, error) {
	in, err := os.OpenRoot(inbox)
	if err != nil {
		return "", err
	}
	defer in.Close()
	fi, err := music.Lstat(u.Pfad)
	if err != nil {
		return "", err
	}
	base := path.Base(u.Pfad)
	if u.Name != "" {
		base = safeName(u.Name)
		if !fi.IsDir() {
			base += strings.ToLower(path.Ext(u.Pfad))
		}
	}
	to := base
	for n := 2; ; n++ {
		if _, err := in.Lstat(to); errors.Is(err, fs.ErrNotExist) {
			break
		}
		ext := path.Ext(base)
		if fi.IsDir() {
			ext = ""
		}
		to = strings.TrimSuffix(base, ext) + " (" + strconv.Itoa(n) + ")" + ext
	}
	part := "._" + to + ".part"
	in.RemoveAll(part)
	if fi.IsDir() {
		err = copyTree(music, u.Pfad, in, part)
	} else {
		err = copyFile(music, u.Pfad, in, part)
	}
	if err != nil {
		in.RemoveAll(part)
		return "", err
	}
	if id := uploader(who); id != "" {
		writeRootFile(in, "._"+to+".von", id+"\n")
	}
	if err := in.Rename(part, to); err != nil {
		in.RemoveAll(part)
		return "", err
	}
	if err := music.RemoveAll(u.Pfad); err != nil {
		log.Printf("haus: musik aufräumen: %s is in the inbox, but stays in the music too: %v", u.Pfad, err)
	}
	log.Printf("haus: musik aufräumen: %s put %s into the inbox as %s", who, u.Pfad, to)
	return to, nil
}

// safeName is a plan name as a file name: no slash, no leading dot.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '-'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.TrimLeft(s, ".")
	if len(s) > 200 {
		s = s[:200]
	}
	if s == "" {
		s = "Titel"
	}
	return s
}

func copyFile(from *os.Root, src string, to *os.Root, dst string) error {
	r, err := from.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := to.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

func copyTree(from *os.Root, src string, to *os.Root, dst string) error {
	if err := to.Mkdir(dst, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(from.FS(), src)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		s, d := src+"/"+e.Name(), dst+"/"+e.Name()
		switch {
		case e.IsDir():
			err = copyTree(from, s, to, d)
		case e.Type().IsRegular():
			err = copyFile(from, s, to, d)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func writeRootFile(root *os.Root, name, content string) {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	f.WriteString(content)
	f.Close()
}

const aufraeumenStyle = `<style>.schlecht{display:flex;gap:10px;align-items:flex-start;padding:12px 14px;border-radius:14px;background:var(--danger-soft);color:var(--danger);margin:12px 0}.schlecht p{margin:0}.alben summary{cursor:pointer;list-style:none}.alben summary::-webkit-details-marker{display:none}.befunde{margin:8px 0 0;padding-left:18px;font-size:.92em}.befunde li{margin:2px 0;overflow-wrap:anywhere}</style>`
