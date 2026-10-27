package pages

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"html"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Stimme (laib#825, #828; owner 2026-10-27: „Der Rest ja“): each person
// says the house's wake phrase a few times, so the wake word learns the
// family's own voices. The page records in the browser, makes it 16 kHz
// mono 16-bit PCM there, and sends one WAV per take; Haus checks it and
// puts it into the folder laib mounts (stimme/recordings/<wakeword>/haus),
// as <person>-<YYYYMMDD-HHMMSS>.wav, 0600. Nothing about a recording goes
// into a log beyond that one was kept; laib offers the owner a retraining
// once there are enough.
const StimmePath = "/stimme"

const (
	stimmeRate   = 16000
	stimmeMinSec = 0.25
	stimmeMaxSec = 4.0
	stimmeWanted = 5 // takes per person
)

// stimmeShown is whether this box can take recordings.
func stimmeShown(cfg StartConfig) bool {
	return cfg.WakeRecordings != "" && House.Wakeword != "" && House.Phrase != ""
}

// Stimme is the page and its POST (one WAV).
func Stimme(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != StimmePath || !stimmeShown(cfg) {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		person := uploader(r.Header.Get("Remote-User"))
		if r.Method == http.MethodPost {
			stimmeAufnahme(w, r, cfg, person)
			return
		}
		if !showOnly(w, r, "Stimme") {
			return
		}
		b := familyHead(r, cfg, "Stimme", "Stimme")
		phrase := html.EscapeString(House.Phrase)
		n := stimmeCount(cfg, person)
		b.WriteString(stimmeStyle + `<h1>Stimme</h1><p class=k>Sag ` + strconv.Itoa(stimmeWanted) + `-mal „` + phrase + `“, so wie du es sonst sagst. Dann wacht ` +
			html.EscapeString(House.Display) + ` bei deiner Stimme zuverlässiger auf und seltener ohne Grund. Die Aufnahmen bleiben auf der Box.</p>`)
		b.WriteString(`<section class=karte aria-labelledby=st-a><h2 id=st-a>Einsprechen</h2>`)
		b.WriteString(`<p id=stimme-stand role=status aria-live=polite data-n=` + strconv.Itoa(n) + `>` + html.EscapeString(stimmeSays(n)) + `</p>`)
		b.WriteString(`<p class=wege><button type=button id=stimme-los class=knopf data-phrase="` + phrase + `">` + html.EscapeString(stimmeButton(n)) + `</button></p>`)
		b.WriteString(`<p class=k>Tipp auf den Knopf, warte auf „Jetzt sprechen“ und sag „` + phrase + `“. Nach drei Sekunden hört die Aufnahme von selbst auf.</p></section>`)
		b.WriteString(stimmeScript)
		writePage(w, http.StatusOK, b)
	}
}

func stimmeSays(n int) string {
	switch {
	case n == 0:
		return "Noch keine Aufnahme von dir."
	case n >= stimmeWanted:
		return strconv.Itoa(n) + " Aufnahmen von dir. Das reicht; mehr schaden nicht."
	}
	return strconv.Itoa(n) + " von " + strconv.Itoa(stimmeWanted) + " Aufnahmen."
}

func stimmeButton(n int) string {
	if n >= stimmeWanted {
		return "Noch eine Aufnahme"
	}
	return "Aufnahme " + strconv.Itoa(n+1) + " von " + strconv.Itoa(stimmeWanted)
}

// stimmeCount is how many recordings the person has there.
func stimmeCount(cfg StartConfig, person string) int {
	if person == "" {
		return 0
	}
	es, err := os.ReadDir(cfg.WakeRecordings)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range es {
		if strings.HasPrefix(e.Name(), person+"-") && strings.HasSuffix(e.Name(), ".wav") {
			n++
		}
	}
	return n
}

// stimmeAufnahme keeps one take: a WAV as laib takes it, not silent, not
// clipped; the answer is JSON for the page's script.
func stimmeAufnahme(w http.ResponseWriter, r *http.Request, cfg StartConfig, person string) {
	if !fromHaus(r) || person == "" {
		http.Error(w, "Das geht nur von der Seite selbst.", http.StatusForbidden)
		return
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "Die Aufnahme kam nicht ganz an. Versuch es noch einmal.", http.StatusBadRequest)
		return
	}
	if msg := checkWAV(b); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	root, err := os.OpenRoot(cfg.WakeRecordings)
	if err != nil {
		log.Printf("haus: stimme: the recordings folder: %v", err)
		http.Error(w, "Die Box nimmt gerade keine Aufnahmen an. Versuch es später noch einmal.", http.StatusServiceUnavailable)
		return
	}
	defer root.Close()
	now := time.Now()
	if cfg.Now != nil {
		now = cfg.Now()
	}
	name := person + "-" + now.Format("20060102-150405") + ".wav"
	for i := 2; ; i++ {
		if _, err := root.Lstat(name); errors.Is(err, fs.ErrNotExist) {
			break
		}
		name = person + "-" + now.Format("20060102-150405") + "-" + strconv.Itoa(i) + ".wav"
	}
	tmp := "." + name + ".part"
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.Write(b)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err == nil {
		err = root.Rename(tmp, name)
	}
	if err != nil {
		root.Remove(tmp)
		log.Printf("haus: stimme: keeping a recording: %v", err)
		http.Error(w, "Die Aufnahme ließ sich nicht speichern. Versuch es noch einmal.", http.StatusInternalServerError)
		return
	}
	log.Printf("haus: stimme: a recording kept")
	n := stimmeCount(cfg, person)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"n": n, "satz": stimmeSays(n), "knopf": stimmeButton(n)})
}

// checkWAV is "" for a take laib takes (16 kHz, mono, 16-bit PCM, 0.25–4 s,
// some voice, not clipped), else what is wrong, in the family's words.
func checkWAV(b []byte) string {
	const bad = "Das war keine Aufnahme, die die Box lesen kann. Versuch es noch einmal."
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return bad
	}
	var pcm []byte
	var format, channels, bits uint16
	var rate uint32
	for p := 12; p+8 <= len(b); {
		id, size := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:p+8]))
		body := p + 8
		if size < 0 || body+size > len(b) {
			if id == "data" { // a recorder that did not know the length
				size = len(b) - body
			} else {
				return bad
			}
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return bad
			}
			format = binary.LittleEndian.Uint16(b[body:])
			channels = binary.LittleEndian.Uint16(b[body+2:])
			rate = binary.LittleEndian.Uint32(b[body+4:])
			bits = binary.LittleEndian.Uint16(b[body+14:])
		case "data":
			pcm = b[body : body+size]
		}
		p = body + size + size%2
	}
	if format != 1 || channels != 1 || rate != stimmeRate || bits != 16 || pcm == nil {
		return bad
	}
	n := len(pcm) / 2
	if sec := float64(n) / stimmeRate; sec < stimmeMinSec || sec > stimmeMaxSec {
		return "Die Aufnahme war zu kurz oder zu lang. Sag „" + House.Phrase + "“ einmal, direkt nach „Jetzt sprechen“."
	}
	var sum float64
	clipped := 0
	var s int16
	rd := bytes.NewReader(pcm)
	for i := 0; i < n; i++ {
		binary.Read(rd, binary.LittleEndian, &s)
		v := float64(s) / 32768
		sum += v * v
		if s >= 32700 || s <= -32700 {
			clipped++
		}
	}
	if rms := math.Sqrt(sum / float64(n)); rms < 0.005 {
		return "Da war nichts zu hören. Ist das Mikrofon frei? Sprich in normaler Lautstärke und versuch es noch einmal."
	}
	if clipped*100 > n {
		return "Das war zu laut oder zu nah am Mikrofon. Etwas weiter weg, und noch einmal."
	}
	return ""
}

const stimmeStyle = `<style>#stimme-los{min-height:56px;font-size:1.05rem}#stimme-los[aria-pressed=true]{background:var(--accent);color:var(--card)}#stimme-stand.fehler{color:var(--danger)}</style>`

// stimmeScript records about three seconds, makes it 16 kHz mono 16-bit
// PCM, cuts the quiet at both ends, and sends it as a WAV. Every state is
// said on the button and the line: listening, saving, kept, failed, no
// microphone.
const stimmeScript = `<script>(function(){var k=document.getElementById('stimme-los'),st=document.getElementById('stimme-stand');if(!k||k.dataset.an)return;k.dataset.an=1;
function say(t,bad){st.textContent=t;st.className=bad?'fehler':''}
function wav(f){var n=f.length,b=new ArrayBuffer(44+n*2),v=new DataView(b),p=0;function s(x){for(var i=0;i<x.length;i++)v.setUint8(p++,x.charCodeAt(i))}
s('RIFF');v.setUint32(p,36+n*2,true);p+=4;s('WAVEfmt ');v.setUint32(p,16,true);p+=4;v.setUint16(p,1,true);p+=2;v.setUint16(p,1,true);p+=2;v.setUint32(p,16000,true);p+=4;v.setUint32(p,32000,true);p+=4;v.setUint16(p,2,true);p+=2;v.setUint16(p,16,true);p+=2;s('data');v.setUint32(p,n*2,true);p+=4;
for(var i=0;i<n;i++){var x=Math.max(-1,Math.min(1,f[i]));v.setInt16(p,x<0?x*32768:x*32767,true);p+=2}return new Blob([b],{type:'audio/wav'})}
function down(f,rate){var r=rate/16000,n=Math.floor(f.length/r),o=new Float32Array(n);for(var i=0;i<n;i++){var a=Math.floor(i*r),e=Math.min(f.length,Math.floor((i+1)*r)),t=0;for(var j=a;j<e;j++)t+=f[j];o[i]=t/Math.max(1,e-a)}return o}
function cut(f){var w=320,lo=0,hi=f.length,th=0.02;function loud(a){var t=0;for(var i=a;i<a+w&&i<f.length;i++)t+=f[i]*f[i];return Math.sqrt(t/w)>th}
while(lo<hi-w&&!loud(lo))lo+=w;while(hi>lo+w&&!loud(hi-w))hi-=w;lo=Math.max(0,lo-3200);hi=Math.min(f.length,hi+3200);return f.subarray(lo,hi)}
function idle(t){k.disabled=false;k.removeAttribute('aria-pressed');if(t)k.textContent=t}
k.addEventListener('click',function(){if(!navigator.mediaDevices||!navigator.mediaDevices.getUserMedia){say('Dieser Browser lässt Haus nicht ans Mikrofon. Öffne die Seite im Browser des Handys (Chrome, Firefox oder Safari).',true);return}
k.disabled=true;k.textContent='Wartet aufs Mikrofon …';say('');
navigator.mediaDevices.getUserMedia({audio:{channelCount:1,echoCancellation:false,noiseSuppression:false,autoGainControl:true}}).then(function(m){
var C=window.AudioContext||window.webkitAudioContext,ac=new C(),src=ac.createMediaStreamSource(m),pr=ac.createScriptProcessor(4096,1,1),parts=[];
pr.onaudioprocess=function(e){parts.push(new Float32Array(e.inputBuffer.getChannelData(0)))};src.connect(pr);pr.connect(ac.destination);
k.setAttribute('aria-pressed','true');k.textContent='Jetzt sprechen: „'+k.dataset.phrase+'“';say('Hört zu …');
setTimeout(function(){pr.disconnect();src.disconnect();m.getTracks().forEach(function(t){t.stop()});
var len=0;parts.forEach(function(p){len+=p.length});var all=new Float32Array(len),o=0;parts.forEach(function(p){all.set(p,o);o+=p.length});var rate=ac.sampleRate;ac.close();
var f=cut(down(all,rate));k.removeAttribute('aria-pressed');k.textContent='Speichert …';
var ctl=new AbortController(),to=setTimeout(function(){ctl.abort()},15000);
fetch(location.pathname,{method:'POST',headers:{'Content-Type':'audio/wav'},body:wav(f),signal:ctl.signal}).then(function(r){clearTimeout(to);
if(r.ok)return r.json().then(function(j){say('Gespeichert. '+j.satz);idle(j.knopf)});return r.text().then(function(t){say(t||'Das hat nicht geklappt. Versuch es noch einmal.',true);idle('Noch einmal')})})
.catch(function(){clearTimeout(to);say('Die Box hat nicht geantwortet. Versuch es gleich noch einmal.',true);idle('Noch einmal')})},3000)
},function(e){say(e&&e.name==='NotAllowedError'?'Das Mikrofon ist für diese Seite gesperrt. Erlaube es in den Einstellungen des Browsers (Schloss neben der Adresse) und tipp dann noch einmal.':'Kein Mikrofon gefunden.',true);idle('Noch einmal')})})})()</script>`
