package pages

import (
	"encoding/json"
	"errors"
	"html"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
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
	MusikPath     = "/musik"
	MusikTeilPath = "/musik/teil"

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
		if !showOnly(w, r, "Musik hinzufügen") {
			return
		}
		if cfg.MusicInbox == "" || r.URL.Path != MusikPath {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		b := familyHead(r, cfg, "Musik hinzufügen", "Musik hinzufügen")
		b.WriteString(musikStyle + "<h1>Musik hinzufügen</h1><p class=k>" + html.EscapeString(saysMusikHow) + "</p>")
		accept := ".mp3,.flac,.m4a,.ogg,.opus,.wav,.zip,audio/*,application/zip"
		b.WriteString(`<section class=karte aria-labelledby=m-w><h2 id=m-w>Was hochladen?</h2><div class=musik-wahl>` +
			`<label class=knopf>Dateien wählen<input type=file id=musik-dateien multiple accept="` + accept + `"></label>` +
			`<label class="knopf ordner">Ordner wählen<input type=file id=musik-ordner webkitdirectory multiple></label></div>` +
			`<p class=k>` + html.EscapeString(saysMusikFolder) + `</p></section>`)
		b.WriteString(`<section class=karte aria-labelledby=m-l hidden id=musik-lauf><h2 id=m-l>Hochladen</h2><p id=musik-stand role=status aria-live=polite></p><ul id=musik-liste class=musik-liste></ul></section>`)
		b.WriteString(musikScript)
		writePage(w, http.StatusOK, b)
	}
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
.musik-liste .name{overflow-wrap:anywhere}.musik-liste progress{display:block;width:100%;height:6px;margin:6px 0 0;accent-color:var(--accent)}.musik-liste .fehler{color:var(--danger)}.musik-liste .gut{color:var(--ok)}</style>`

// musikScript sends the chosen files one after another, each in pieces,
// showing each file's progress and how far the whole is; a piece that
// fails is sent again (three times), one the server already has is
// skipped. At the end: how many arrived, and that the box sorts them in.
const musikScript = `<script>(function(){var PIECE=` + "8388608" + `;
var lauf=document.getElementById('musik-lauf'),liste=document.getElementById('musik-liste'),stand=document.getElementById('musik-stand');
if(!window.fetch){return}
['musik-dateien','musik-ordner'].forEach(function(id){var i=document.getElementById(id);if(i)i.addEventListener('change',function(){go(Array.prototype.slice.call(i.files));i.value=''})});
var o=document.createElement('input');o.type='file';if(!('webkitdirectory' in o)){var od=document.querySelector('.ordner');if(od)od.hidden=true}
function go(files){if(!files.length)return;lauf.hidden=false;var rows=files.map(function(f){var li=document.createElement('li');
var p=f.webkitRelativePath?f.webkitRelativePath:f.name;li.innerHTML='<span class=name></span><progress max=100 value=0></progress><span class=k></span>';
li.querySelector('.name').textContent=p;liste.appendChild(li);return {f:f,p:p,li:li}});var done=0,bad=0,i=0;
function say(){stand.textContent=(done+bad)+' von '+rows.length+' Dateien'+(bad?(', '+bad+' nicht angekommen'):'')}say();
function next(){if(i>=rows.length){stand.textContent=done+' von '+rows.length+' Dateien hochgeladen'+(bad?(', '+bad+' nicht'):'')+'. Die Box sortiert sie jetzt ein; in ein paar Minuten sind sie in der Musik.';return}
var x=rows[i++];send(x,0,0).then(function(){done++;x.li.querySelector('.k').textContent='angekommen';x.li.querySelector('.k').className='gut';say();next()},
function(t){bad++;x.li.querySelector('.k').textContent=t;x.li.querySelector('.k').className='fehler';say();next()})}
function send(x,at,tries){var f=x.f,bar=x.li.querySelector('progress');var end=Math.min(at+PIECE,f.size);
if(f.size===0)return Promise.reject('Die Datei ist leer.');
return fetch('` + MusikTeilPath + `?pfad='+encodeURIComponent(x.p)+'&ab='+at+'&gesamt='+f.size,{method:'POST',credentials:'same-origin',body:f.slice(at,end),headers:{'Content-Type':'application/octet-stream'}})
.then(function(r){var ct=r.headers.get('Content-Type');if(ct&&ct.indexOf('json')>=0)return r.json().then(function(j){return {code:r.status,j:j}});return r.text().then(function(t){throw {code:r.status,t:t.trim()}})})
.then(function(a){bar.value=Math.round(a.j.ab*100/f.size);if(a.j.fertig)return;return send(x,a.j.ab,0)})
.catch(function(e){if(typeof e==='string')return Promise.reject(e);if(e&&e.code&&e.code<500)return Promise.reject(e.t);
if(tries<3)return new Promise(function(ok){setTimeout(ok,2e3)}).then(function(){return send(x,at,tries+1)});return Promise.reject(e&&e.t?e.t:'Die Verbindung brach ab.')})}
next()}})()</script>`
