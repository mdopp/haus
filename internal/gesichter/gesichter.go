// Package gesichter links the household's contacts with Immich's people
// (owner 2026-11-05): a contact's „Fotos“ opens the Immich person of its
// name, a contact without a picture gets that person's face on the phones,
// and a named person without a contact is offered on the Kontakte page.
//
// Immich is asked in the background only (Pflegen), never while a page or
// the phones' feed waits: the named people every hour, each face once and
// again only when Immich changed it or after BildTTL, made small (≤ 96 px)
// and kept in HOMES_DATA/immich-gesichter.json. Who was hidden from the
// suggestions is HOMES_DATA/immich-ausgeblendet.json. Without the owner's
// key there are no faces and no suggestions; the link searches Immich for
// the name.
package gesichter

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/jsonfile"
	"github.com/mdopp/homes/internal/namen"
)

const (
	cacheDatei = "immich-gesichter.json"
	ausDatei   = "immich-ausgeblendet.json"
	// BildTTL is how long a face is kept before it is fetched again.
	BildTTL = 7 * 24 * time.Hour
	// Kante is a face's longest side on the phones, in pixels.
	Kante = 96
)

// Gesichter is Immich as the contacts see it.
type Gesichter struct {
	Immich *immich.Client // nil: no key, so no faces and no suggestions
	Web    string         // HOMES_IMMICH_PUBLIC_URL, https://photos.<domain>
	Dir    string         // HOMES_DATA
	Now    func() time.Time

	mu        sync.Mutex
	geladen   bool
	stand     stand
	versteckt map[string]bool
}

// stand is what Homes keeps of Immich.
type stand struct {
	Personen []immich.Person `json:"personen"`
	Bilder   map[string]bild `json:"bilder"` // by person id
}

type bild struct {
	JPEG   []byte    `json:"jpeg"`
	Stand  string    `json:"stand"` // the person's updatedAt it was made from
	Geholt time.Time `json:"geholt"`
}

// FromEnv is Immich at HOMES_IMMICH_PUBLIC_URL, with the owner's key when
// it is set; nil without the address.
func FromEnv(dir string) *Gesichter {
	web := strings.TrimRight(strings.TrimSpace(os.Getenv("HOMES_IMMICH_PUBLIC_URL")), "/")
	if u, err := url.Parse(web); web == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil
	}
	return &Gesichter{Immich: immich.FromEnv(), Web: web, Dir: dir}
}

func (g *Gesichter) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// laden reads the kept state once; g.mu is held.
func (g *Gesichter) laden() {
	if g.geladen {
		return
	}
	g.geladen = true
	if b, err := os.ReadFile(filepath.Join(g.Dir, cacheDatei)); err == nil {
		json.Unmarshal(b, &g.stand)
	}
	var ids []string
	if b, err := os.ReadFile(filepath.Join(g.Dir, ausDatei)); err == nil {
		json.Unmarshal(b, &ids)
	}
	g.versteckt = map[string]bool{}
	for _, id := range ids {
		g.versteckt[id] = true
	}
}

// person is the one named person of that name; none for no name, no
// person or two of the same name. g.mu is held.
func (g *Gesichter) person(name string) (immich.Person, bool) {
	g.laden()
	want, found, n := namen.Plain(name), immich.Person{}, 0
	for _, p := range g.stand.Personen {
		if want != "" && namen.Plain(p.Name) == want {
			found, n = p, n+1
		}
	}
	return found, n == 1
}

// Link is where „Fotos“ opens Immich for name: the person's page, else
// Immich's search for the name (web/src/lib/route.ts: /people/<id>, and
// /search?query=<SmartSearchDto as JSON>).
func (g *Gesichter) Link(name string) string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	p, ok := g.person(name)
	g.mu.Unlock()
	if ok && g.Immich != nil {
		return g.Web + "/people/" + url.PathEscape(p.ID)
	}
	q, _ := json.Marshal(map[string]string{"query": strings.TrimSpace(name)})
	return g.Web + "/search?" + url.Values{"query": {string(q)}}.Encode()
}

// Bild is the small face of name's person, as kept; nil without one.
func (g *Gesichter) Bild(name string) []byte {
	if g == nil || g.Immich == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if p, ok := g.person(name); ok {
		return g.stand.Bilder[p.ID].JPEG
	}
	return nil
}

// Vorschlaege are the named people with no contact of their name among
// kontakte, none hidden, by name.
func (g *Gesichter) Vorschlaege(kontakte []string) []immich.Person {
	if g == nil || g.Immich == nil {
		return nil
	}
	da := map[string]bool{}
	for _, k := range kontakte {
		da[namen.Plain(k)] = true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.laden()
	var out []immich.Person
	for _, p := range g.stand.Personen {
		if !da[namen.Plain(p.Name)] && !g.versteckt[p.ID] {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b immich.Person) int { return strings.Compare(namen.Plain(a.Name), namen.Plain(b.Name)) })
	return out
}

// Vorschlag is the suggested person id; false for one Homes does not know.
func (g *Gesichter) Vorschlag(id string) (immich.Person, bool) {
	if g == nil {
		return immich.Person{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.laden()
	i := slices.IndexFunc(g.stand.Personen, func(p immich.Person) bool { return p.ID == id })
	if i < 0 {
		return immich.Person{}, false
	}
	return g.stand.Personen[i], true
}

// Ausblenden hides id from the suggestions for good.
func (g *Gesichter) Ausblenden(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.laden()
	if g.versteckt[id] {
		return nil
	}
	ids := []string{id}
	for v := range g.versteckt {
		ids = append(ids, v)
	}
	slices.Sort(ids)
	if err := jsonfile.Save(filepath.Join(g.Dir, ausDatei), ids); err != nil {
		return err
	}
	g.versteckt[id] = true
	return nil
}

// Auffrischen asks Immich for its named people, and fetches the face of
// each that has none kept, a changed one, or one older than BildTTL; a
// face Immich has not is left out. Faces of people gone are dropped.
func (g *Gesichter) Auffrischen(ctx context.Context) error {
	if g == nil || g.Immich == nil {
		return nil
	}
	people, err := g.Immich.People(ctx)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.laden()
	alt := g.stand.Bilder
	g.mu.Unlock()
	neu := map[string]bild{}
	for _, p := range people {
		b, ok := alt[p.ID]
		if !ok || b.Stand != p.Stand || g.now().Sub(b.Geholt) > BildTTL {
			if roh, err := g.Immich.Gesicht(ctx, p.ID); err == nil {
				if klein, err := Klein(roh, Kante); err == nil {
					b, ok = bild{JPEG: klein, Stand: p.Stand, Geholt: g.now()}, true
				}
			} else if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		if ok {
			neu[p.ID] = b
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stand = stand{Personen: people, Bilder: neu}
	return jsonfile.Save(filepath.Join(g.Dir, cacheDatei), g.stand)
}

// Pflegen keeps Immich's people fresh: now, then every jede.
func (g *Gesichter) Pflegen(ctx context.Context, jede time.Duration) {
	if g == nil || g.Immich == nil {
		return
	}
	for {
		if err := g.Auffrischen(ctx); err != nil && ctx.Err() == nil {
			log.Printf("homes: immich: people and faces: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(jede):
		}
	}
}

// Klein is the JPEG b made no larger than kante on its longer side, each
// pixel the mean of those it covers; the same b gives the same bytes.
func Klein(b []byte, kante int) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()
	if w > kante || h > kante {
		w, h = max(1, w*kante/max(w, h)), max(1, h*kante/max(w, h))
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := sb.Min.Y+y*sb.Dy()/h, sb.Min.Y+max((y+1)*sb.Dy()/h, y*sb.Dy()/h+1)
		for x := range w {
			x0, x1 := sb.Min.X+x*sb.Dx()/w, sb.Min.X+max((x+1)*sb.Dx()/w, x*sb.Dx()/w+1)
			var r, gr, bl, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, _ := src.At(xx, yy).RGBA()
					r, gr, bl, n = r+uint64(cr), gr+uint64(cg), bl+uint64(cb), n+1
				}
			}
			dst.Set(x, y, color.RGBA64{uint16(r / n), uint16(gr / n), uint16(bl / n), 0xffff})
		}
	}
	var out bytes.Buffer
	err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 80})
	return out.Bytes(), err
}
