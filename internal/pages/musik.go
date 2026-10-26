package pages

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/mdopp/haus/internal/einkauf"
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
	"time"
)

// Musik hinzufügen (haus#107; owner 2026-10-24: "einfach über die Haus App
// hochladen"): files, a whole folder or ZIPs into the music inbox, which
// laib's importer (beets) sorts into the music and Jellyfin. Anyone in the
// family; music only, into that folder only.
//
// The page's script sends each file in pieces (MusikTeilPath): a piece is
// appended to "._<name>.part" beside where the file goes, at the offset the
// page says (a resend of a piece already there is refused with the size
// there, so the page goes on from it), and after the last byte the part is
// renamed to the file's name — the contract with the importer, which
// never touches a dot file. The folder structure is kept.
const (
	MusikPath      = "/musik"
	MusikTeilPath  = "/musik/teil"
	MusikStandPath = "/musik/stand" // how far the box's import is (haus#113), for the page's script

	musikPiece   = 8 << 20        // what one request carries, under any proxy's limit
	musikMaxFile = int64(4) << 30 // a file, a ZIP of an album or several
	musikDepth   = 6              // folders a path may have

	saysMusikHow    = "Lieder, ganze Alben als Ordner oder ZIP-Dateien. Die Box sortiert sie danach selbst ein; nach ein paar Minuten sind sie in der Musik (Jellyfin) zu finden."
	saysMusikFolder = "Ordner wählen geht im Browser am Computer; auf dem Handy die Dateien oder eine ZIP wählen."
)

// musikTypes are the files the inbox takes: music and ZIPs of it.
var musikTypes = map[string]bool{".mp3": true, ".flac": true, ".m4a": true, ".ogg": true, ".opus": true, ".wav": true, ".zip": true}

// musikName is a path the page sends as a file's place in the inbox: up
// to musikDepth folders, no part empty, hidden or leaving, a music file or
// ZIP at the end. "" when it is none.
func musikName(p string) string {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") || len(p) > 1024 {
		return ""
	}
	parts := strings.Split(p, "/")
	if len(parts) > musikDepth+1 {
		return ""
	}
	for _, s := range parts {
		if s == "" || s == "." || s == ".." || strings.HasPrefix(s, ".") || len(s) > 255 {
			return ""
		}
	}
	clean := path.Join(parts...)
	if !musikTypes[strings.ToLower(path.Ext(clean))] {
		return ""
	}
	return clean
}

// Musik is the page: what it takes, the pickers, and a place for each
// file's progress, which the script fills.
func Musik(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == MusikTeilPath {
			musikTeil(w, r, cfg)
			return
		}
		if r.URL.Path == MusikStandPath && cfg.MusicInbox != "" {
			musikStand(w, r, cfg)
			return
		}
		if cfg.MusicInbox == "" || r.URL.Path != MusikPath {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		if r.Method == http.MethodPost {
			musikNochmal(w, r, cfg)
			return
		}
		if !showOnly(w, r, "Musik") {
			return
		}
		writeMusik(w, r, cfg, "", "")
	}
}

// writeMusik is the page: what was just done, the pickers, the place for
// the uploads' progress, and what beets did not take, each to try again.
// again is an upload put back just now: the script follows its import.
func writeMusik(w http.ResponseWriter, r *http.Request, cfg StartConfig, said, again string) {
	{
		b := familyHead(r, cfg, "Musik", "Musik")
		b.WriteString(musikStyle + keyHintStyle + "<h1>Musik</h1>")
		b.WriteString(musikPlatten(r, cfg))
		b.WriteString(`<h2 class=musik-teil>Musik hinzufügen</h2><p class=k>` + html.EscapeString(saysMusikHow) + "</p>")
		accept := ".mp3,.flac,.m4a,.ogg,.opus,.wav,.zip,audio/*,application/zip"
		b.WriteString(`<section class=karte aria-labelledby=m-w><h2 id=m-w>Was hochladen?</h2><div class=musik-wahl>` +
			`<label class=knopf>Dateien wählen<input type=file id=musik-dateien multiple accept="` + accept + `"></label>` +
			`<label class="knopf ordner">Ordner wählen<input type=file id=musik-ordner webkitdirectory multiple></label></div>` +
			`<p class=k>` + html.EscapeString(saysMusikFolder) + `</p></section>`)
		if said != "" {
			b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p></div>")
		}
		b.WriteString(`<section class=karte aria-labelledby=m-l hidden id=musik-lauf><h2 id=m-l>Hochladen</h2><p id=musik-stand role=status aria-live=polite></p><ul id=musik-liste class=musik-liste></ul></section>`)
		if again != "" {
			b.WriteString(`<div hidden id=musik-nochmal data-name="` + html.EscapeString(again) + `" data-seit="` + time.Now().UTC().Format(time.RFC3339) + `"></div>`)
		}
		b.WriteString(nichtUebernommen(cfg))
		b.WriteString(musikScript)
		writePage(w, http.StatusOK, b)
	}
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

// nichtUebernommen is the card of what beets kept back, each with
// "Noch einmal einsortieren"; none when there is nothing.
func nichtUebernommen(cfg StartConfig) string {
	kept := zurueck(cfg.MusicInbox)
	if len(kept) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<section class=karte aria-labelledby=m-n><h2 id=m-n>Nicht übernommen</h2><p class=k>Das hat die Box nicht einsortiert. Was inzwischen gehen sollte, legt „Noch einmal einsortieren“ zurück in den Musik-Eingang; was du nicht brauchst, „Löschen“.</p><ul class=musik-liste>`)
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

// musikNochmal puts an upload beets kept back into the inbox again, under
// its own name (beside one there already), for who asked: the importer
// takes it in about a minute; or (tun=loeschen) deletes it. A run left
// with only its GRUND.txt goes.
func musikNochmal(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	if !fromHaus(r) {
		http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
		return
	}
	r.ParseForm()
	run, name, tun := r.PostForm.Get("lauf"), r.PostForm.Get("name"), r.PostForm.Get("tun")
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
		if err := root.RemoveAll(from); err != nil {
			log.Printf("haus: musik: deleting %s: %v", from, err)
			http.Error(w, "Die Datei ließ sich nicht löschen.", http.StatusInternalServerError)
			return
		}
		tidyRun(root, run)
		log.Printf("haus: musik: %s deleted %s", r.Header.Get("Remote-User"), from)
		writeMusik(w, r, cfg, "„"+name+"“ ist gelöscht.", "")
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
	if who := uploader(r.Header.Get("Remote-User")); who != "" {
		if f, err := root.OpenFile("._"+to+".von", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o664); err == nil {
			f.WriteString(who + "\n")
			f.Close()
		}
	}
	if err := root.Rename(from, to); err != nil {
		log.Printf("haus: musik: again %s: %v", from, err)
		http.Error(w, "Die Datei ließ sich nicht zurücklegen.", http.StatusInternalServerError)
		return
	}
	tidyRun(root, run)
	log.Printf("haus: musik: %s put %s back into the inbox", r.Header.Get("Remote-User"), name)
	writeMusik(w, r, cfg, "„"+name+"“ liegt wieder im Musik-Eingang. Die Box sortiert es in etwa einer Minute ein.", to)
}

// plainName is one path part, no folder, nothing hidden or leaving.
func plainName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00") && !strings.HasPrefix(s, ".") && len(s) <= 255
}

// musikTeil takes one piece: ?pfad=<place>&ab=<offset>&gesamt=<size>, the
// bytes as the body. It answers {"ab": the part's size now, "fertig": true
// when the file is in place}; a piece at the wrong offset 409 with the
// size there; what is not music, too big or not Haus's own page, a
// sentence.
func musikTeil(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Hochladen geht nur über die Seite.", http.StatusMethodNotAllowed)
		return
	}
	if cfg.MusicInbox == "" {
		http.NotFound(w, r)
		return
	}
	if !fromHaus(r) {
		http.Error(w, "Hochladen geht nur von Haus' eigener Seite.", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	name := musikName(q.Get("pfad"))
	at, err1 := strconv.ParseInt(q.Get("ab"), 10, 64)
	total, err2 := strconv.ParseInt(q.Get("gesamt"), 10, 64)
	switch {
	case name == "":
		http.Error(w, "Das ist keine Musikdatei (MP3, FLAC, M4A, OGG, Opus, WAV) und keine ZIP.", http.StatusBadRequest)
		return
	case err1 != nil || err2 != nil || at < 0 || total <= 0 || at >= total:
		http.Error(w, "Die Angaben zum Stück fehlen.", http.StatusBadRequest)
		return
	case total > musikMaxFile:
		http.Error(w, "Die Datei ist zu groß: höchstens 4 GB.", http.StatusRequestEntityTooLarge)
		return
	}
	root, err := os.OpenRoot(cfg.MusicInbox)
	if err != nil {
		log.Printf("haus: musik: inbox: %v", err)
		http.Error(w, "Der Musik-Eingang ist gerade nicht erreichbar.", http.StatusServiceUnavailable)
		return
	}
	defer root.Close()
	dir, file := path.Split(name)
	dir = strings.TrimSuffix(dir, "/")
	if dir != "" {
		if err := root.MkdirAll(dir, 0o775); err != nil {
			log.Printf("haus: musik: %s: %v", dir, err)
			http.Error(w, "Der Ordner lässt sich nicht anlegen.", http.StatusInternalServerError)
			return
		}
	}
	part := path.Join(dir, "._"+file+".part")
	f, err := root.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o664)
	if err != nil {
		log.Printf("haus: musik: %s: %v", part, err)
		http.Error(w, "Die Datei lässt sich nicht schreiben.", http.StatusInternalServerError)
		return
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		http.Error(w, "Die Datei lässt sich nicht schreiben.", http.StatusInternalServerError)
		return
	}
	answer := func(code int, size int64, done bool) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{"ab": size, "fertig": done})
	}
	if st.Size() != at { // a piece twice, or one missing: the page goes on from what is there
		f.Close()
		answer(http.StatusConflict, st.Size(), false)
		return
	}
	n, err := io.Copy(f, io.LimitReader(r.Body, min(int64(musikPiece), total-at)+1))
	cerr := f.Close()
	size := at + n
	switch {
	case err != nil || cerr != nil:
		log.Printf("haus: musik: %s: %v %v", part, err, cerr)
		http.Error(w, "Das Stück kam nicht ganz an. Es wird noch einmal geschickt.", http.StatusBadGateway)
		return
	case size > total || n > musikPiece:
		root.Remove(part)
		http.Error(w, "Die Datei ist größer als angekündigt.", http.StatusBadRequest)
		return
	case size < total:
		answer(http.StatusOK, size, false)
		return
	}
	// the last byte: the file appears whole under its name, never over another
	final := name
	for i := 2; ; i++ {
		if _, err := root.Lstat(final); errors.Is(err, fs.ErrNotExist) {
			break
		}
		ext := path.Ext(name)
		final = strings.TrimSuffix(name, ext) + " (" + strconv.Itoa(i) + ")" + ext
		if i > 99 {
			http.Error(w, "Diese Datei gibt es schon zu oft.", http.StatusConflict)
			return
		}
	}
	// who uploaded, for the importer's message (laib templates/beets
	// README, "Uploading from Haus" 2): beside the upload at the inbox's
	// top, named after it, written before the upload is whole
	if who := uploader(r.Header.Get("Remote-User")); who != "" {
		top, _, _ := strings.Cut(final, "/")
		if f, err := root.OpenFile("._"+top+".von", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o664); err == nil {
			_, werr := f.WriteString(who + "\n")
			if cerr := f.Close(); werr != nil || cerr != nil {
				log.Printf("haus: musik: ._%s.von: %v %v", top, werr, cerr)
			}
		} else {
			log.Printf("haus: musik: ._%s.von: %v", top, err)
		}
	}
	if err := root.Rename(part, final); err != nil {
		log.Printf("haus: musik: %s → %s: %v", part, final, err)
		http.Error(w, "Die Datei lässt sich nicht ablegen.", http.StatusInternalServerError)
		return
	}
	log.Printf("haus: musik: %s by %s (%d bytes)", final, r.Header.Get("Remote-User"), size)
	answer(http.StatusOK, size, true)
}

const musikStyle = `<style>.musik-wahl{display:flex;gap:10px;flex-wrap:wrap;margin:12px 0 0}.musik-wahl .knopf{position:relative;display:inline-flex;align-items:center;min-height:44px;padding:0 18px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-weight:600;cursor:pointer}
.musik-wahl input{position:absolute;inset:0;opacity:0;cursor:pointer}.musik-liste{list-style:none;margin:10px 0 0;padding:0}.musik-liste li{padding:8px 0;border-top:1px solid var(--line);font-size:.9rem}
.musik-liste .name{overflow-wrap:anywhere;display:block}.musik-tun{display:flex;flex-wrap:wrap;gap:8px;margin:8px 0 0}.musik-tun form{margin:0}.musik-tun .weg{background:var(--danger-soft);color:var(--danger)}.musik-liste progress{display:block;width:100%;height:6px;margin:6px 0 0;accent-color:var(--accent)}.musik-liste .fehler{color:var(--danger)}.musik-liste .gut{color:var(--ok)}</style>`

// musikScript sends the chosen files one after another, each in pieces,
// showing each file's progress and how far the whole is; a piece that
// fails is sent again (three times), one the server already has is
// skipped. At the end: how many arrived, "Wird einsortiert …", and what
// became of each upload once beets took it (haus#107, laib#688: its titles
// and albums, or what was kept back and why), or after six minutes that it
// takes longer and a message follows.
const musikScript = `<script>(function(){var PIECE=` + "8388608" + `;
var lauf=document.getElementById('musik-lauf'),liste=document.getElementById('musik-liste'),stand=document.getElementById('musik-stand');
if(!window.fetch){return}
['musik-dateien','musik-ordner'].forEach(function(id){var i=document.getElementById(id);if(i)i.addEventListener('change',function(){go(Array.prototype.slice.call(i.files));i.value=''})});
var o=document.createElement('input');o.type='file';if(!('webkitdirectory' in o)){var od=document.querySelector('.ordner');if(od)od.hidden=true}
function go(files){if(!files.length)return;lauf.hidden=false;var rows=files.map(function(f){var li=document.createElement('li');
var p=f.webkitRelativePath?f.webkitRelativePath:f.name;li.innerHTML='<span class=name></span><progress max=100 value=0></progress><span class=k></span>';
li.querySelector('.name').textContent=p;liste.appendChild(li);return {f:f,p:p,li:li}});var done=0,bad=0,i=0,seit=new Date().toISOString(),names=[];
function say(){stand.textContent=(done+bad)+' von '+rows.length+' Dateien'+(bad?(', '+bad+' nicht angekommen'):'')}say();
function next(){if(i>=rows.length){var up=done+' von '+rows.length+' Dateien hochgeladen'+(bad?(', '+bad+' nicht'):'')+'. ';
if(!done){stand.textContent=up;return}stand.textContent=up+'Wird einsortiert …';sorted(up,Date.now(),names,seit,[]);return}
var x=rows[i++];send(x,0,0).then(function(){done++;var top=x.p.split('/')[0];if(names.indexOf(top)<0)names.push(top);x.li.querySelector('.k').textContent='angekommen';x.li.querySelector('.k').className='gut';say();next()},
function(t){bad++;x.li.querySelector('.k').textContent=t;x.li.querySelector('.k').className='fehler';say();next()})}
function send(x,at,tries){var f=x.f,bar=x.li.querySelector('progress');var end=Math.min(at+PIECE,f.size);
if(f.size===0)return Promise.reject('Die Datei ist leer.');
return fetch('` + MusikTeilPath + `?pfad='+encodeURIComponent(x.p)+'&ab='+at+'&gesamt='+f.size,{method:'POST',credentials:'same-origin',body:f.slice(at,end),headers:{'Content-Type':'application/octet-stream'}})
.then(function(r){var ct=r.headers.get('Content-Type');if(ct&&ct.indexOf('json')>=0)return r.json().then(function(j){return {code:r.status,j:j}});return r.text().then(function(t){throw {code:r.status,t:t.trim()}})})
.then(function(a){bar.value=Math.round(a.j.ab*100/f.size);if(a.j.fertig)return;return send(x,a.j.ab,0)})
.catch(function(e){if(typeof e==='string')return Promise.reject(e);if(e&&e.code&&e.code<500)return Promise.reject(e.t);
if(tries<3)return new Promise(function(ok){setTimeout(ok,2e3)}).then(function(){return send(x,at,tries+1)});return Promise.reject(e&&e.t?e.t:'Die Verbindung brach ab.')})}
next()}
var back=document.getElementById('musik-nochmal');if(back){lauf.hidden=false;stand.textContent='Wird einsortiert …';sorted('',Date.now(),[back.getAttribute('data-name')],back.getAttribute('data-seit'),[])}
function sorted(up,t0,names,seit,got){var q='?seit='+encodeURIComponent(seit)+names.map(function(n){return '&namen='+encodeURIComponent(n)}).join('');
fetch('` + MusikStandPath + `'+q,{credentials:'same-origin'}).then(function(r){return r.json()}).then(function(j){got=j.ergebnis||[];
if(got.length>=names.length){stand.textContent=up+got.map(function(e){return e.satz}).join(' ');return}throw 0}).catch(function(){
if(Date.now()-t0>6*60e3){stand.textContent=up+got.map(function(e){return e.satz+' '}).join('')+'Das Einsortieren dauert länger. Sobald es fertig ist, bekommst du eine Nachricht; danach ist die Musik in Jellyfin.';return}
setTimeout(function(){sorted(up,t0,names,seit,got)},10e3)})}})()</script>`

// uploader is the signed-in user as the importer takes it: lower case,
// [a-z0-9._@-], at most 64 characters; else "" (no sidecar).
func uploader(user string) string {
	u := strings.ToLower(strings.TrimSpace(user))
	if u == "" || len(u) > 64 {
		return ""
	}
	for _, r := range u {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("._@-", r)) {
			return ""
		}
	}
	return u
}

// musikStand is the import's line from laib's status of beets (haus#113),
// without the box's prefix and age: {"stand": "zuletzt 12 Titel (2
// Alben) importiert", "wartet": true while an upload still settles}; ""
// when laib does not say.
func musikStand(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	if !showOnly(w, r, "Der Stand") {
		return
	}
	out := map[string]any{"stand": "", "wartet": false, "ergebnis": []any{}}
	if cfg.Laib != nil {
		ctx, cancel := context.WithTimeout(r.Context(), backendWait)
		defer cancel()
		if line, own, err := cfg.Laib.ServiceStatus(ctx, "beets"); err != nil {
			log.Printf("haus: musik: laib status beets: %v", err)
		} else {
			out["stand"], out["wartet"] = importLine(line)
			out["ergebnis"] = uploadResults(own, r.URL.Query()["namen"], r.URL.Query().Get("seit"))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(out)
}

// importLine is beets' detail as the page says it: "Musik-Eingang: zuletzt
// 12 Titel importiert · 1 Eingang wird noch kopiert · geprüft vor 2 min"
// is "zuletzt 12 Titel importiert", still settling.
func importLine(detail string) (string, bool) {
	parts := strings.Split(detail, " · ")
	var keep []string
	wartet := false
	for _, p := range parts {
		switch {
		case strings.HasPrefix(p, "geprüft "):
		case strings.Contains(p, "noch kopiert"):
			wartet = true
		default:
			keep = append(keep, p)
		}
	}
	return strings.TrimPrefix(strings.Join(keep, " · "), "Musik-Eingang: "), wartet
}

// Ergebnis is what became of one upload (laib#688, beets' letzte[]): its
// name as uploaded, the titles and albums sorted in, and what was kept
// back with why — said as the page says it.
type Ergebnis struct {
	Eingang string `json:"eingang"`
	Satz    string `json:"satz"`
	Gut     bool   `json:"gut"`
}

// uploadResults are the results for these uploads (their names at the
// inbox's top) since seit (RFC 3339, the page's start; a minute's leeway
// for the clocks), newest first; none when laib does not say.
func uploadResults(own json.RawMessage, names []string, seit string) []Ergebnis {
	var st struct {
		Letzte []struct {
			At      string   `json:"at"`
			Eingang string   `json:"eingang"`
			Titel   int      `json:"titel"`
			Alben   int      `json:"alben"`
			Namen   []string `json:"namen"`
			Nicht   []struct {
				Datei string `json:"datei"`
				Grund string `json:"grund"`
			} `json:"nicht_importiert"`
		} `json:"letzte"`
	}
	out := []Ergebnis{}
	if len(own) == 0 || json.Unmarshal(own, &st) != nil {
		return out
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	since, err := time.Parse(time.RFC3339, seit)
	if err != nil {
		return out
	}
	for _, u := range st.Letzte {
		at, err := time.Parse(time.RFC3339, u.At)
		if err != nil || at.Before(since.Add(-time.Minute)) || !want[u.Eingang] {
			continue
		}
		e := Ergebnis{Eingang: u.Eingang}
		var parts []string
		if u.Titel > 0 {
			p := count(u.Titel, "Titel", "Titel")
			if u.Alben > 0 {
				p += " (" + count(u.Alben, "Album", "Alben") + ")"
			}
			if len(u.Namen) > 0 && len(u.Namen) <= 3 {
				p += ": " + strings.Join(u.Namen, ", ")
			}
			parts = append(parts, p+" in der Musik")
			e.Gut = true
		}
		for _, n := range u.Nicht {
			parts = append(parts, "nicht übernommen: "+n.Datei+" ("+n.Grund+")")
		}
		if len(parts) == 0 {
			parts = append(parts, "nichts übernommen")
		}
		e.Satz = u.Eingang + ": " + strings.Join(parts, "; ") + "."
		out = append(out, e)
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

// musikPlatten is Musik's part for records (owner 2026-10-26: Platte
// erfassen and Discogs belong under Musik): the music wish list, Platte
// erfassen and one's Discogs account; without the household's Discogs app,
// what is missing and, for an admin, the way to its keys.
func musikPlatten(r *http.Request, cfg StartConfig) string {
	if cfg.Wuensche == nil && cfg.Discogs == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<section class=karte aria-labelledby=m-p><h2 id=m-p>Platten und Wünsche</h2><p class=wege>`)
	if cfg.Wuensche != nil {
		b.WriteString(`<a class=knopf href="` + EinkaufPath + `?liste=` + einkauf.MusikListe.ID() + `">Musik-Wunschliste</a> `)
	}
	if cfg.Discogs != nil {
		b.WriteString(`<a class=knopf href=` + PlattePath + `>Platte erfassen</a> <a class=knopf href=` + DiscogsPath + `>Dein Discogs-Konto</a>`)
	}
	b.WriteString(`</p>`)
	if cfg.Discogs == nil {
		b.WriteString(keyHint(r, cfg, "Discogs", saysDiscogsEnables, publicBase(r, cfg)+MusikPath, discogsKeys))
	}
	b.WriteString(`</section>`)
	return b.String()
}
