package gesichter

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/immich"
)

// A contact's picture as a suggestion, never as an answer (owner
// 2026-11-06: „Es sollte mich aber dennoch fragen ob es die richtige
// Person ist. Manchmal sind da auch Fantasy-Bilder, Frau oder Mann oder
// Kinder oder jemand anders drauf.“).
//
// The hourly run (Kontaktbilder) uploads the picture of the owner's own
// Handy-Kontakt whose person has no face yet, archived, so Immich finds
// its face. Immich gives that face to a person on its own when one is
// close enough (person.service.ts handleRecognizeFaces: a face of an
// archived photo joins an existing person, it never starts one); that
// person is the question „Ist das …?“ (Fragen), answered by the owner
// (Bestaetigen):
//
//   - „Ja“ merges the contact's person into that one: its faces stay, the
//     contact's name comes along when it has none.
//   - „Nein“ deletes the upload (Immich has no way to take a face from a
//     person but to delete it) and that person is never asked again.
//   - „Kein Foto“ deletes the upload, and that picture is never used again.
//
// Nothing is merged or named without a Ja. Homes deletes only photos it
// uploaded itself; a picture Immich had already is not touched.

const (
	maxHochladen = 10 // pictures uploaded in one run
	// erkennenZeit is how long Immich gets to find a face before the
	// picture counts as one without (a drawing, a landscape).
	erkennenZeit = 24 * time.Hour
)

// The stands of a contact's picture: wartet and frage are open, the others
// done for this picture.
const (
	bildWartet     = "wartet"       // uploaded; Immich has not given its face a person (yet)
	bildFrage      = "frage"        // its face is in Person: the owner is asked
	bildJa         = "ja"           // merged on the owner's Ja
	bildPasst      = "passt"        // Immich gave the face to the contact's own person
	bildNein       = "nein"         // the owner: not that person; deleted
	bildKeinFoto   = "kein-foto"    // the owner: no picture of the contact; deleted
	bildOhne       = "ohne-gesicht" // Immich found no face in erkennenZeit; deleted
	bildMehrere    = "mehrere"      // more than one face: no picture of one person; deleted
	bildAnderer    = "anderer"      // in a hidden person, or one the owner said Nein to; deleted
	bildDoppelt    = "doppelt"      // Immich had these bytes already: not Homes' photo
	bildHatGesicht = "hat-gesicht"  // the contact's person got a face meanwhile; deleted
)

// kontaktbild is what became of a contact's picture in Immich.
type kontaktbild struct {
	Hash   string    `json:"hash"`             // the picture's
	Asset  string    `json:"asset,omitempty"`  // Homes' upload; "" none (deleted, or never Homes')
	Seit   time.Time `json:"seit"`             // uploaded
	Stand  string    `json:"stand"`            // bildWartet, …
	Person string    `json:"person,omitempty"` // the person Immich gave the face
	Name   string    `json:"name,omitempty"`   // that person's name; "" none
	Bild   []byte    `json:"bild,omitempty"`   // the picture, small
	Dessen []byte    `json:"dessen,omitempty"` // Person's face, small
	Nie    []string  `json:"nie,omitempty"`    // pictures never used again („Kein Foto“)
	Nein   []string  `json:"nein,omitempty"`   // persons never asked again („Nein“)
}

func (b kontaktbild) offen() bool { return b.Stand == bildWartet || b.Stand == bildFrage }

// ErrKeineFrage is an answer to a question not asked (any more).
var ErrKeineFrage = errors.New("gesichter: no such question")

// bilder are the JPEG pictures of root's cards by UID: PHOTO as vCard
// 3.0's base64 or 4.0's data URI; a face Homes added (a homes- group or
// X-HOMES) is no picture of the contact's.
func bilder(root *caldav.Component) map[string][]byte {
	out := map[string][]byte{}
	root.Walk(func(c *caldav.Component) {
		if c.Name != "VCARD" {
			return
		}
		uid, foto := c.Text("UID"), []byte(nil)
		for _, p := range c.Props {
			gruppe, name, _ := strings.Cut(strings.ToUpper(p.Name), ".")
			if name == "" {
				gruppe, name = "", gruppe
			}
			_, homes := p.Params["X-HOMES"]
			if name != "PHOTO" || foto != nil || homes || strings.HasPrefix(gruppe, "HOMES-") {
				continue
			}
			v := p.Value
			if i := strings.Index(v, "base64,"); strings.HasPrefix(v, "data:") && i > 0 {
				v = v[i+len("base64,"):]
			}
			if b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(v), "")); err == nil && bytes.HasPrefix(b, []byte{0xff, 0xd8}) {
				foto = b
			}
		}
		if uid != "" && foto != nil {
			out[uid] = foto
		}
	})
	return out
}

func bildHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// Kontaktbilder brings the owner's own contacts' pictures to Immich and
// follows what Immich made of each (the file's comment), at most
// maxHochladen uploads a run. Without the key's asset.upload, face.read
// and asset.delete it does nothing, and Fehlt says so.
func (g *Gesichter) Kontaktbilder(ctx context.Context) error {
	if g == nil || g.Immich == nil || g.Kontakte == nil {
		return nil
	}
	g.mu.Lock()
	g.laden()
	darf := !slices.ContainsFunc(immich.BildRechte, func(r string) bool { return !g.darf(r) })
	g.mu.Unlock()
	if !darf || g.Wessen() == "" {
		return nil
	}
	ks, ganz, err := g.kontakte(ctx)
	if err != nil {
		return err
	}
	g.mu.Lock()
	alt, zu := maps.Clone(g.stand.Kontaktbilder), maps.Clone(g.stand.Zuordnung)
	mitGesicht := map[string]bool{}
	for _, p := range g.stand.Personen {
		mitGesicht[p.ID] = true
	}
	g.mu.Unlock()
	neu, hoch := map[string]kontaktbild{}, 0
	for _, k := range ks {
		b, z := alt[k.key], zu[k.key]
		delete(alt, k.key)
		if z.ID != "" && err == nil {
			var up bool
			b, up, err = g.einBild(ctx, b, k.foto, z.ID, mitGesicht[z.ID], hoch < maxHochladen)
			hoch += btoi(up)
		}
		if b.Hash != "" || len(b.Nie)+len(b.Nein) > 0 {
			neu[k.key] = b
		}
	}
	for key, b := range alt { // a contact gone: its open upload goes with it
		if ganz && err == nil && b.offen() && b.Asset != "" {
			err = g.Immich.Entfernen(ctx, b.Asset)
		}
		if !ganz || err != nil {
			neu[key] = b
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stand.Kontaktbilder = neu
	return errors.Join(err, g.save())
}

// einBild follows one contact's picture foto (nil: none) for its person
// eigen: a new or changed one is uploaded when eigen has no face and darf
// (up), Homes' upload of the one before deleted; an open one checked.
func (g *Gesichter) einBild(ctx context.Context, b kontaktbild, foto []byte, eigen string, mitGesicht, darf bool) (_ kontaktbild, up bool, err error) {
	h := ""
	if foto != nil {
		h = bildHash(foto)
	}
	if b.Hash == h {
		if b.offen() {
			b, err = g.pruefen(ctx, b, eigen, mitGesicht)
		}
		return b, false, err
	}
	if b.Asset != "" && b.Stand != bildJa && b.Stand != bildPasst { // Homes' upload of the picture before goes
		if err = g.Immich.Entfernen(ctx, b.Asset); err != nil {
			return b, false, err
		}
	}
	b = kontaktbild{Nie: b.Nie, Nein: b.Nein}
	if h == "" || mitGesicht || slices.Contains(b.Nie, h) || !darf {
		return b, false, nil
	}
	b, err = g.hochladen(ctx, b, foto, h)
	return b, true, err
}

// hochladen uploads the picture foto of hash h, archived.
func (g *Gesichter) hochladen(ctx context.Context, b kontaktbild, foto []byte, h string) (kontaktbild, error) {
	id, doppelt, err := g.Immich.Hochladen(ctx, "homes-kontaktbild-"+h+".jpg", foto, g.now())
	if err != nil {
		return kontaktbild{Nie: b.Nie, Nein: b.Nein}, err
	}
	b.Hash, b.Seit, b.Stand, b.Asset = h, g.now(), bildWartet, id
	if doppelt {
		b.Stand, b.Asset = bildDoppelt, ""
	}
	if klein, err := Klein(foto, Kante); err == nil {
		b.Bild = klein
	}
	return b, nil
}

// pruefen is what Immich made of b's upload for the contact's person
// eigen (mitGesicht: it has a face now): still waiting, a question, or
// done, and then Homes' upload deleted unless it helps eigen.
func (g *Gesichter) pruefen(ctx context.Context, b kontaktbild, eigen string, mitGesicht bool) (kontaktbild, error) {
	funde, err := g.Immich.Funde(ctx, b.Asset)
	var a immich.Antwort
	if errors.As(err, &a) && a.Code == 400 { // deleted in Immich meanwhile
		b.Stand, b.Asset = bildKeinFoto, ""
		return b, nil
	}
	if err != nil {
		return b, err
	}
	var p immich.Person
	if len(funde) == 1 && funde[0].Person != nil {
		p = *funde[0].Person
	}
	stand := ""
	switch {
	case p.ID != "" && p.ID == eigen:
		b.Stand = bildPasst
		return b, nil
	case mitGesicht:
		stand = bildHatGesicht
	case len(funde) == 0 && g.now().Sub(b.Seit) < erkennenZeit, len(funde) == 1 && p.ID == "":
		b.Stand = bildWartet
		return b, nil
	case len(funde) == 0:
		stand = bildOhne
	case len(funde) > 1:
		stand = bildMehrere
	case p.Versteckt || slices.Contains(b.Nein, p.ID):
		stand = bildAnderer
	default:
		if b.Person != p.ID || b.Dessen == nil {
			b.Dessen = nil
			if roh, err := g.Immich.Gesicht(ctx, p.ID); err == nil {
				b.Dessen, _ = Klein(roh, Kante)
			}
		}
		b.Stand, b.Person, b.Name = bildFrage, p.ID, strings.TrimSpace(p.Name)
		return b, nil
	}
	if err := g.Immich.Entfernen(ctx, b.Asset); err != nil {
		return b, err
	}
	b.Stand, b.Asset, b.Person = stand, "", ""
	return b, nil
}

// Frage is „Ist das …?“: a contact's picture beside the person Immich
// gave its face.
type Frage struct {
	Key, Name string // the contact, as in Zuordnung, and its name
	Bild      []byte // its picture, small
	Asset     string // Homes' upload of it in Immich; "" none
	Person    Seite  // the person Immich holds it for; Fotos as counted, -1 not
}

// Fragen are the questions open, by the contact's name; one without both
// pictures (a valid JPEG each) or whose person is in no photo waits.
func (g *Gesichter) Fragen() []Frage {
	if g == nil || g.Immich == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.laden()
	var out []Frage
	for key, b := range g.stand.Kontaktbilder {
		if b.Stand != bildFrage {
			continue
		}
		n := -1
		if z, ok := g.stand.Fotos[b.Person]; ok {
			n = z.N
		}
		f := Frage{key, g.stand.Zuordnung[key].Name, b.Bild, b.Asset, g.stand.seite(b.Person, b.Name, n, b.Dessen)}
		if Gesehen(f.Bild) && f.Person.zeigbar() { // both faces to show, or nothing to decide
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(x, y Frage) int { return strings.Compare(x.Name+x.Key, y.Name+y.Key) })
	return out
}

// Bestaetigen takes the owner's answer to the question of the contact key
// about person: "ja" merges the contact's person into it and answers the
// name it keeps; "nein" and "keinfoto" delete Homes' upload and keep the
// person or the picture from coming back.
func (g *Gesichter) Bestaetigen(ctx context.Context, key, person, antwort string) (string, error) {
	g.mu.Lock()
	g.laden()
	b, z := g.stand.Kontaktbilder[key], g.stand.Zuordnung[key]
	g.mu.Unlock()
	if b.Stand != bildFrage || b.Person != person || z.ID == "" {
		return "", ErrKeineFrage
	}
	var err error
	switch antwort {
	case "ja":
		var weg []string
		if weg, err = g.verschmelzen(ctx, person, []string{z.ID}); err == nil && len(weg) == 0 {
			b.Stand, err = bildWartet, ErrNichtZusammen // one is gone: Immich is asked again
		} else if err == nil {
			b.Stand = bildJa
		}
	case "nein":
		if err = g.Immich.Entfernen(ctx, b.Asset); err == nil {
			b.Asset, b.Stand, b.Nein = "", bildNein, append(b.Nein, person)
		}
	case "keinfoto":
		if err = g.Immich.Entfernen(ctx, b.Asset); err == nil {
			b.Asset, b.Stand, b.Nie = "", bildKeinFoto, append(b.Nie, b.Hash)
		}
	default:
		return "", fmt.Errorf("gesichter: no answer %q", antwort)
	}
	var fehlt immich.FehltRecht
	g.mu.Lock()
	defer g.mu.Unlock()
	if errors.As(err, &fehlt) && antwort == "ja" {
		g.stand.FehltMerge = fehlt.Recht
	}
	if errors.As(err, &fehlt) || (err != nil && !errors.Is(err, ErrNichtZusammen)) {
		return "", err
	}
	g.stand.Kontaktbilder[key] = b
	return cmp.Or(b.Name, z.Name), errors.Join(err, g.save())
}
