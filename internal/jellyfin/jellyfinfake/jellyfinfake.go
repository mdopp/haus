// Package jellyfinfake is Jellyfin's music as Homes reads it: the albums,
// newest first, and each one's cover, a plain square of its colour. For
// the tests and cmd/homesdemo only.
package jellyfinfake

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Album is one album the fake holds; Farbe its cover's colour, none: no cover.
type Album struct {
	ID, Name, Kuenstler string
	Farbe               color.RGBA
}

// Jellyfin is the fake on its own address (URL): its albums, the newest first.
type Jellyfin struct {
	*httptest.Server
	mu    sync.Mutex
	alben []Album
}

// New is the fake serving, with no album.
func New() *Jellyfin {
	f := &Jellyfin{}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

// Setze replaces the albums, the newest first.
func (f *Jellyfin) Setze(alben ...Album) {
	f.mu.Lock()
	f.alben = alben
	f.mu.Unlock()
}

func (f *Jellyfin) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	alben := f.alben
	f.mu.Unlock()
	if r.URL.Path == "/Items" && r.URL.Query().Get("IncludeItemTypes") == "MusicAlbum" {
		items := []map[string]any{}
		for _, a := range alben {
			tags := map[string]string{}
			if a.Farbe.A != 0 {
				tags["Primary"] = "t"
			}
			items = append(items, map[string]any{"Id": a.ID, "Name": a.Name, "AlbumArtist": a.Kuenstler, "ImageTags": tags})
		}
		json.NewEncoder(w).Encode(map[string]any{"Items": items, "TotalRecordCount": len(items) * 12})
		return
	}
	id, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/Items/"), "/Images/Primary")
	for _, a := range alben {
		if ok && a.ID == id && a.Farbe.A != 0 {
			img := image.NewRGBA(image.Rect(0, 0, 240, 240))
			for y := range 240 { // its colour, darker to the bottom
				d := func(c uint8) uint8 { return uint8(int(c) * (400 - y) / 400) }
				for x := range 240 {
					img.SetRGBA(x, y, color.RGBA{d(a.Farbe.R), d(a.Farbe.G), d(a.Farbe.B), 255})
				}
			}
			var b bytes.Buffer
			png.Encode(&b, img)
			w.Header().Set("Content-Type", "image/png")
			w.Write(b.Bytes())
			return
		}
	}
	http.NotFound(w, r)
}

// Demo is a household's music for cmd/homesdemo: albums with covers, one without.
var Demo = []Album{
	{"0a1b2c3d4e5f60718293a4b5c6d7e8f9", "Ciao!", "Wanda", color.RGBA{232, 120, 60, 255}},
	{"1b2c3d4e5f60718293a4b5c6d7e8f90a", "Erfundenes Album", "Die Beispiele", color.RGBA{70, 140, 210, 255}},
	{"2c3d4e5f60718293a4b5c6d7e8f90a1b", "Noch ein Album", "Die Beispiele", color.RGBA{120, 190, 120, 255}},
	{"3d4e5f60718293a4b5c6d7e8f90a1b2c", "Abbey Road", "The Beatles", color.RGBA{200, 180, 90, 255}},
	{"4e5f60718293a4b5c6d7e8f90a1b2c3d", "Ein Lächeln im Gesicht", "Farin Urlaub", color.RGBA{}},
	{"5f60718293a4b5c6d7e8f90a1b2c3d4e", "Kind of Blue", "Miles Davis", color.RGBA{60, 80, 160, 255}},
}
