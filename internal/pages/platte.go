package pages

import (
	"context"
	"errors"
	"html"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/discogs"
	"github.com/mdopp/haus/internal/judge"
)

// Platte erfassen (owner 2026-10-25): 1–4 photos of a record — the back
// with its barcode, the front, the label, the spine — and Haus finds the
// pressing on Discogs; the owner confirms, and it goes into the collection.
// The phone reads the barcode itself (the browser's BarcodeDetector) and
// makes the photos small before they go; the box's model reads the rest
// (catalogue number, label, artist, title) only when there is no barcode.
// Nothing is added without "Das ist sie".
const (
	PlattePath     = "/platte"
	PlatteBildPath = "/platte/bild"

	saysPlatteHow = "Fotos der Platte: die Rückseite mit dem Barcode zuerst, dazu Vorderseite, Etikett oder Rücken. Haus sucht die Pressung bei Discogs; du bestätigst, dann kommt sie in deine Sammlung."
)

// platteWait is how long reading the photos and asking Discogs may take.
var platteWait = 90 * time.Second

// Platte is the page and its POSTs: tun=suchen (foto…, barcode,
// katalognummer), tun=hinzufuegen (release, kuenstler, titel).
func Platte(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.Regal == nil {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		if r.URL.Path == PlatteBildPath {
			platteBild(w, r, cfg)
			return
		}
		if r.URL.Path != PlattePath {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		if r.Method != http.MethodPost {
			if showOnly(w, r, "Platte erfassen") {
				writePlatte(w, r, cfg, "", nil, nil, discogs.Hinweise{})
			}
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 40<<20)
		if err := r.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
			http.Error(w, "Die Fotos sind zu groß: höchstens vier, zusammen 40 MB.", http.StatusRequestEntityTooLarge)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), platteWait)
		defer cancel()
		switch r.FormValue("tun") {
		case "suchen":
			platteSuchen(ctx, w, r, cfg)
		case "hinzufuegen":
			platteHinzu(ctx, w, r, cfg)
		default:
			http.Error(w, "Das geht hier nicht.", http.StatusBadRequest)
		}
	}
}

// platteSuchen reads what there is — the barcode the phone read or typed,
// a catalogue number typed, else the photos through the model — and shows
// Discogs's best releases to confirm.
func platteSuchen(ctx context.Context, w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	h := discogs.Hinweise{Barcode: strings.TrimSpace(r.FormValue("barcode")), Katalognummer: strings.TrimSpace(r.FormValue("katalognummer"))}
	var notes []string
	if h.Barcode != "" && !discogs.EAN(h.Barcode) {
		notes = append(notes, "Der Barcode „"+h.Barcode+"“ stimmt nicht (Prüfziffer); Haus sucht ohne ihn.")
		h.Barcode = ""
	}
	if h.Barcode == "" && r.MultipartForm != nil && len(r.MultipartForm.File["foto"]) > 0 {
		if cfg.Judge == nil {
			notes = append(notes, "Fotos lesen kann diese Box nicht (kein Modell); gib den Barcode oder die Katalognummer ein.")
		} else {
			var bilder []judge.Bild
			for i, fh := range r.MultipartForm.File["foto"] {
				if i == 4 {
					break
				}
				f, err := fh.Open()
				if err != nil {
					continue
				}
				b, _ := io.ReadAll(io.LimitReader(f, 8<<20))
				f.Close()
				if ct := http.DetectContentType(b); strings.HasPrefix(ct, "image/jpeg") || strings.HasPrefix(ct, "image/png") || strings.HasPrefix(ct, "image/webp") {
					bilder = append(bilder, judge.Bild{Data: b, Type: ct})
				}
			}
			if len(bilder) == 0 {
				notes = append(notes, "Auf den Fotos war kein Bild, das Haus lesen kann (JPEG, PNG, WebP).")
			} else {
				got, err := cfg.Judge.LiesPlatte(ctx, bilder)
				var nl *judge.NotLoaded
				switch {
				case errors.As(err, &nl):
					notes = append(notes, nl.Satz()+" Mit dem Barcode geht es trotzdem.")
				case err != nil:
					log.Printf("haus: platte: reading the photos: %v", err)
					notes = append(notes, "Die Fotos ließen sich gerade nicht lesen. Gib den Barcode oder die Katalognummer ein.")
				default:
					if got.Barcode != "" && discogs.EAN(got.Barcode) {
						h.Barcode = got.Barcode
					} else if got.Barcode != "" {
						notes = append(notes, "Den Barcode hat Haus auf dem Foto nicht sicher gelesen.")
					}
					if h.Katalognummer == "" {
						h.Katalognummer = got.Katalognummer
					}
					h.Label, h.Kuenstler, h.Titel = got.Label, got.Kuenstler, got.Titel
				}
			}
		}
	}
	if h.Barcode == "" && h.Katalognummer == "" && h.Kuenstler == "" && h.Titel == "" {
		msg := "Haus hat nichts, wonach es suchen kann. Fotografier die Rückseite mit dem Barcode, oder gib Barcode oder Katalognummer ein."
		if len(notes) > 0 {
			msg = strings.Join(notes, " ") + " " + msg
		}
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	ts, err := cfg.Regal.C.Finden(ctx, h)
	if err != nil {
		log.Printf("haus: platte: discogs: %v", err)
		http.Error(w, discogsSays(err), http.StatusBadGateway)
		return
	}
	writePlatte(w, r, cfg, "", notes, ts, h)
}

// discogsSays is a Discogs refusal in the family's words.
func discogsSays(err error) string {
	var e *discogs.Error
	if errors.As(err, &e) && (e.Status == 401 || e.Status == 403) {
		return "Discogs lässt Haus nicht hinein: Der Schlüssel stimmt nicht mehr. Hinterleg auf Laibs Seite einen neuen."
	}
	if errors.As(err, &e) && e.Status == 429 {
		return "Discogs bremst gerade (zu viele Anfragen). In einer Minute noch einmal."
	}
	return "Discogs antwortet gerade nicht. Gleich noch einmal versuchen."
}

// platteHinzu adds the confirmed release, ticks off what waited for it on
// the music wish list, and says whether the music is there as a file too.
func platteHinzu(ctx context.Context, w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	id, err := strconv.Atoi(r.FormValue("release"))
	if err != nil || id <= 0 {
		http.Error(w, "Welche Platte? Die Nummer fehlt.", http.StatusBadRequest)
		return
	}
	if err := cfg.Regal.C.Hinzufuegen(ctx, id); err != nil {
		log.Printf("haus: platte: adding %d: %v", id, err)
		http.Error(w, discogsSays(err), http.StatusBadGateway)
		return
	}
	cfg.Regal.Vergessen()
	artist, title := r.FormValue("kuenstler"), r.FormValue("titel")
	name := strings.Trim(artist+" – "+title, " –")
	said := "„" + name + "“ ist in deiner Sammlung."
	log.Printf("haus: platte: %s added %d", r.Header.Get("Remote-User"), id)
	if cfg.Wuensche != nil {
		if done, err := cfg.Wuensche.Pruefen(ctx); err == nil {
			for _, d := range done {
				said += " " + d.Satz
			}
		}
		if cfg.Wuensche.Bib != nil && title != "" {
			if _, ok, err := cfg.Wuensche.Bib.Find(ctx, artist, title); err == nil && ok {
				said += " Als Datei hast du sie auch."
			} else if err == nil {
				said += " Als Datei ist sie noch nicht in deiner Musik."
			}
		}
	}
	writePlatte(w, r, cfg, said, nil, nil, discogs.Hinweise{})
}

// platteBild is a release's cover through Haus: the phone never asks
// Discogs itself.
func platteBild(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	id, err := strconv.Atoi(r.URL.Query().Get("r"))
	if err != nil || id <= 0 || !showOnly(w, r, "Das Bild") {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	b, ct, err := cfg.Regal.C.Bild(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(b)
}

const platteStyle = `<style>.platte-form label{display:block;font-size:.85rem;color:var(--muted);margin:12px 0 6px}
.platte-form input[type=text]{width:100%;min-height:48px;font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:12px;padding:0 14px}
.platte-form .knopf{position:relative;display:inline-flex;align-items:center;min-height:48px;padding:0 20px;border-radius:999px;background:var(--accent);color:var(--card);font-weight:600;cursor:pointer}
.platte-form .knopf input{position:absolute;inset:0;opacity:0;cursor:pointer}.platte-form button{margin:16px 0 0;min-height:48px}
.treffer{list-style:none;margin:12px 0 0;padding:0;display:grid;gap:10px}.treffer li{display:flex;gap:12px;align-items:flex-start;padding:12px;border:1px solid var(--edge);border-radius:14px;background:var(--card)}
.treffer img{width:72px;height:72px;border-radius:8px;object-fit:cover;flex:none;background:var(--line)}.treffer .was{flex:1;min-width:0;overflow-wrap:anywhere}.treffer b{display:block}
.treffer small{display:block;color:var(--muted)}.treffer .grund{display:inline-block;margin:4px 0;padding:2px 10px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-size:.8rem;font-weight:600}
.treffer form{margin:8px 0 0}.treffer button{min-height:44px}</style>`

// writePlatte is the page: what was just done, what the photos said, the
// releases to choose from, and the form for the (next) record.
func writePlatte(w http.ResponseWriter, r *http.Request, cfg StartConfig, said string, notes []string, ts []discogs.Treffer, h discogs.Hinweise) {
	b := familyHead(r, cfg, "Platte erfassen", "Platte erfassen")
	b.WriteString(platteStyle + "<h1>Platte erfassen</h1><p class=k>" + html.EscapeString(saysPlatteHow) + "</p>")
	if said != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p></div>")
	}
	for _, n := range notes {
		b.WriteString("<div class=\"achtung selbst\" role=status><p>" + html.EscapeString(n) + "</p></div>")
	}
	if ts != nil || h != (discogs.Hinweise{}) {
		var read []string
		for _, kv := range [][2]string{{"Barcode", h.Barcode}, {"Katalognummer", h.Katalognummer}, {"Label", h.Label}, {"", strings.Trim(h.Kuenstler+" – "+h.Titel, " –")}} {
			if kv[1] != "" {
				read = append(read, strings.TrimSpace(kv[0]+" "+kv[1]))
			}
		}
		b.WriteString(`<section class=karte aria-labelledby=k-treffer><h2 id=k-treffer>Ist es eine davon?</h2>`)
		if len(read) > 0 {
			b.WriteString("<p class=k>Gelesen: " + html.EscapeString(strings.Join(read, " · ")) + "</p>")
		}
		if len(ts) == 0 {
			b.WriteString("<p class=k>Discogs findet dazu keine Platte. Prüf Barcode oder Katalognummer unten, oder fotografier die Rückseite mit dem Barcode.</p>")
		}
		b.WriteString("<ul class=treffer>")
		for _, t := range ts {
			var meta []string
			for _, m := range []string{t.Year, t.Country, strings.Join(t.Format, ", ")} {
				if m != "" && m != "0" {
					meta = append(meta, m)
				}
			}
			label := strings.Join(t.Label, ", ")
			if t.Catno != "" {
				label = strings.TrimSpace(label + " " + t.Catno)
			}
			if label != "" {
				meta = append(meta, label)
			}
			id := strconv.Itoa(t.ID)
			b.WriteString(`<li><img src="` + PlatteBildPath + `?r=` + id + `" alt="" loading=lazy><div class=was><b>` + html.EscapeString(t.Kuenstler()+" – "+t.Titel()) + `</b>` +
				`<small>` + html.EscapeString(strings.Join(meta, " · ")) + `</small><span class=grund>` + html.EscapeString(t.Grund) + `</span>` +
				`<form method=post action=` + PlattePath + ` enctype=multipart/form-data><input type=hidden name=tun value=hinzufuegen><input type=hidden name=release value=` + id + `>` +
				`<input type=hidden name=kuenstler value="` + html.EscapeString(t.Kuenstler()) + `"><input type=hidden name=titel value="` + html.EscapeString(t.Titel()) + `">` +
				`<button data-laeuft="Trägt ein …">Das ist sie</button></form></div></li>`)
		}
		b.WriteString("</ul></section>")
	}
	b.WriteString(`<section class=karte aria-labelledby=k-erfassen><h2 id=k-erfassen>` + map[bool]string{true: "Nächste Platte", false: "Fotos"}[said != ""] + `</h2>` +
		`<form class=platte-form method=post action=` + PlattePath + ` enctype=multipart/form-data><input type=hidden name=tun value=suchen>` +
		`<label class=knopf>Fotos machen<input type=file id=platte-fotos name=foto accept="image/*" capture=environment multiple></label>` +
		`<p id=platte-gewaehlt class=k role=status></p>` +
		`<label for=barcode>Barcode (liest das Handy selbst vom Foto der Rückseite)</label><input type=text id=barcode name=barcode inputmode=numeric autocomplete=off value="` + html.EscapeString(h.Barcode) + `">` +
		`<label for=katalognummer>Katalognummer, wenn es keinen Barcode gibt</label><input type=text id=katalognummer name=katalognummer autocomplete=off placeholder="z. B. PB 011" value="` + html.EscapeString(h.Katalognummer) + `">` +
		`<button data-laeuft="Sucht die Platte … (bis zu einer Minute)">Platte suchen</button></form></section>`)
	b.WriteString(platteScript)
	writePage(w, http.StatusOK, b)
}

// platteScript makes each chosen photo small (1600 px, JPEG) before it
// goes, and reads a barcode from it with the browser's BarcodeDetector
// when the phone has one; it says what it has.
const platteScript = `<script>(function(){var i=document.getElementById('platte-fotos');if(!i||i.dataset.an)return;i.dataset.an=1;
var out=document.getElementById('platte-gewaehlt'),bc=document.getElementById('barcode');
var det=('BarcodeDetector' in window)?new BarcodeDetector({formats:['ean_13','ean_8','upc_a','upc_e']}):null;
i.addEventListener('change',function(){var fs=Array.prototype.slice.call(i.files,0,4);if(!fs.length)return;out.textContent='Bereitet '+fs.length+' Foto(s) vor …';
Promise.all(fs.map(function(f){return createImageBitmap(f).then(function(bm){var d=det?det.detect(bm).catch(function(){return []}):Promise.resolve([]);
return d.then(function(codes){if(codes.length&&!bc.value)bc.value=codes[0].rawValue;var s=Math.min(1,1600/Math.max(bm.width,bm.height));
var c=document.createElement('canvas');c.width=Math.round(bm.width*s);c.height=Math.round(bm.height*s);c.getContext('2d').drawImage(bm,0,0,c.width,c.height);
return new Promise(function(ok){c.toBlob(function(b){ok(new File([b],f.name.replace(/\.[^.]*$/,'')+'.jpg',{type:'image/jpeg'}))},'image/jpeg',0.85)})})}).catch(function(){return f})}))
.then(function(small){try{var dt=new DataTransfer();small.forEach(function(f){dt.items.add(f)});i.files=dt.files}catch(e){}
out.textContent=small.length+' Foto(s) bereit'+(bc.value?', Barcode '+bc.value+' gelesen':'')+'.'})})})()</script>`
