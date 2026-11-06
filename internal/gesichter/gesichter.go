// Package gesichter links the contacts with Immich's people (owner
// 2026-11-05): a contact's „Fotos“ opens its Immich person, and a contact
// without a picture gets that person's face on the phones.
//
// Every contact has an Immich person of its name (owner 2026-11-06,
// Abgleichen): the Haushalts-Kontakte and the key owner's own
// Handy-Kontakte. Homes makes one where Immich has none of that name, and
// keeps the name and birthday of the ones it made; it never deletes,
// hides, merges or renames another. The owner names a face in Immich,
// Immich offers the person of that name, the owner merges: the faces come
// from Immich itself.
//
// Immich is asked in the background only (Pflegen), never while a page or
// the phones' feed waits: every hour, each face once and again only when
// Immich changed it or after BildTTL, made small (≤ 96 px). All of it is
// kept in HOMES_DATA/immich-gesichter.json. Without the owner's key there
// are no faces and no persons; the link searches Immich for the name.
package gesichter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/jsonfile"
	"github.com/mdopp/homes/internal/namen"
)

const (
	cacheDatei = "immich-gesichter.json"
	// maxAufrufe bounds a run's calls to Immich while matching contacts.
	maxAufrufe = 200
	// BildTTL is how long a face is kept before it is fetched again.
	BildTTL = 7 * 24 * time.Hour
	// Kante is a face's longest side on the phones, in pixels.
	Kante = 96
)

// Gesichter is Immich as the contacts see it.
type Gesichter struct {
	Immich   *immich.Client // nil: no key, so no faces and no persons
	Kontakte *caldav.Client // the household's and the owner's own contacts; nil: none
	Web      string         // HOMES_IMMICH_PUBLIC_URL, https://photos.<domain>
	Dir      string         // HOMES_DATA
	Now      func() time.Time

	mu      sync.Mutex
	geladen bool
	stand   stand
}

// stand is what Homes keeps of Immich.
type stand struct {
	Personen []immich.Person `json:"personen"`
	Bilder   map[string]bild `json:"bilder"` // by person id
	Email    string          `json:"email"`  // the key's Immich account
	Login    string          `json:"login"`  // the Homes login seen with that e-mail
	// Zuordnung is each contact's person, by "haushalt/<uid>" or "privat/<uid>"
	Zuordnung map[string]zuordnung `json:"zuordnung"`
	Fehlt     string               `json:"fehlt,omitempty"` // the permission the last run lacked
}

// zuordnung is a contact's Immich person; Eigen: Homes made it, so Homes
// keeps its name and birthday.
type zuordnung struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Geburtstag string `json:"geburtstag,omitempty"`
	Eigen      bool   `json:"eigen,omitempty"`
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
	if g.stand.Zuordnung == nil {
		g.stand.Zuordnung = map[string]zuordnung{}
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

// Link is where „Fotos“ opens Immich for the contact (key as in
// Zuordnung) of that name: its person's page, else the one person of the
// name, else Immich's search for the name (web/src/lib/route.ts:
// /people/<id>, and /search?query=<SmartSearchDto as JSON>).
func (g *Gesichter) Link(key, name string) string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	p, ok := g.person(name)
	if z, da := g.stand.Zuordnung[key]; da {
		p.ID, ok = z.ID, true
	}
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

// Wessen is the Homes login whose e-mail is the key's Immich account,
// as last seen (Merke); "" before.
func (g *Gesichter) Wessen() string {
	if g == nil || g.Immich == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.laden()
	return g.stand.Login
}

// Merke notes login as the key's owner when email is the key's account
// (Authelia's Remote-Email of a page request): the background then knows
// whose Handy-Kontakte to match.
func (g *Gesichter) Merke(login, email string) {
	if g == nil || g.Immich == nil || login == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.laden()
	if g.stand.Email != "" && strings.EqualFold(g.stand.Email, strings.TrimSpace(email)) && g.stand.Login != login {
		g.stand.Login = login
		g.save()
	}
}

// Fehlt is the permission the key lacked in the last run; "" none.
func (g *Gesichter) Fehlt() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.laden()
	return g.stand.Fehlt
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
	email, err := g.Immich.Wessen(ctx)
	g.mu.Lock()
	g.laden()
	if err == nil || errors.As(err, new(immich.FehltRecht)) {
		g.stand.Email = email
	}
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
	g.stand.Personen, g.stand.Bilder = people, neu
	return g.save()
}

// save keeps the stand; g.mu is held.
func (g *Gesichter) save() error {
	return jsonfile.Save(filepath.Join(g.Dir, cacheDatei), g.stand)
}

// Pflegen keeps Immich's people fresh and gives each of kontakte's
// contacts its person: now, then every jede.
func (g *Gesichter) Pflegen(ctx context.Context, jede time.Duration, kontakte *caldav.Client) {
	if g == nil || g.Immich == nil {
		return
	}
	g.Kontakte = kontakte
	for {
		if err := g.Auffrischen(ctx); err != nil && ctx.Err() == nil {
			log.Printf("homes: immich: people and faces: %v", err)
		}
		if err := g.Abgleichen(ctx); err != nil && ctx.Err() == nil {
			log.Printf("homes: immich: a person per contact: %v", err)
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
