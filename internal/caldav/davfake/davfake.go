// Package davfake is a fake Radicale for tests: Basic auth, PROPFIND on the
// account's home, REPORT calendar-query on a calendar and addressbook-query
// on an address book (each object with its ETag), PUT with If-None-Match
// or If-Match, MKCALENDAR, and MKCOL
// extended for an address book. It filters a calendar REPORT by component
// only and an address book REPORT not at all; the client filters itself,
// and tests read the query it sent.
package davfake

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Server is the fake.
type Server struct {
	*httptest.Server
	User, Password string

	mu      sync.Mutex
	cols    map[string]col    // path → calendar
	objs    map[string]string // path → ICS
	etags   map[string]int    // path → its version, the ETag "<n>"
	version int
	Reports []string      // REPORT bodies, in order
	Methods []string      // "METHOD path", in order
	Header  []http.Header // the PUT headers, in order

	// ExistsStatus is MKCALENDAR's answer for a calendar that is there
	// (default 405, as Radicale; some servers say 409).
	ExistsStatus int
	// MkcalendarStatus, when set, is MKCALENDAR's answer whatever the state (403: no right to).
	MkcalendarStatus int
	// MkcolStatus, when set, is MKCOL's answer whatever the state.
	MkcolStatus int
	// Redirect, when set, answers every request with a 302 to it.
	Redirect string
	// StallAfter, when set, makes an address book REPORT send that many
	// responses and then hang until the client gives up.
	StallAfter int
	// RaceOnPut, when set, is an object whose ETag changes just before the
	// next PUT to it is judged, as if a phone had saved it in between.
	RaceOnPut string
	// NoETag leaves the ETag out of REPORT answers, as a lax server might.
	NoETag bool
	behind map[string]col // made by "another client" right after the next PROPFIND
}

// CreateAfterPropfind makes a calendar appear right after the next PROPFIND
// has answered, as if another client had created it in between.
func (s *Server) CreateAfterPropfind(path, name string, comps ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.behind == nil {
		s.behind = map[string]col{}
	}
	s.behind[path] = col{name: name, comps: comps}
}

// CreateAddressBookAfterPropfind is CreateAfterPropfind for an address book.
func (s *Server) CreateAddressBookAfterPropfind(path, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.behind == nil {
		s.behind = map[string]col{}
	}
	s.behind[path] = col{name: name, book: true}
}

type col struct {
	name  string
	comps []string
	book  bool // an address book, not a calendar
}

// New starts a fake for one account.
func New(user, password string) *Server {
	s := &Server{User: user, Password: password, cols: map[string]col{}, objs: map[string]string{}, etags: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// AddCalendar makes a calendar at path (/<user>/<name>/) taking comps (none:
// all). A full URL as path is listed as such, as a hostile server would.
func (s *Server) AddCalendar(path, name string, comps ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols[path] = col{name: name, comps: comps}
}

// AddAddressBook makes an address book at path; a full URL as path is
// listed as such.
func (s *Server) AddAddressBook(path, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols[path] = col{name: name, book: true}
}

// Put stores an object as a phone would; its ETag changes.
func (s *Server) Put(path, ics string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store(path, ics)
}

func (s *Server) store(path, ics string) {
	s.version++
	s.objs[path] = ics
	s.etags[path] = s.version
}

func (s *Server) etag(path string) string { return `"` + strconv.Itoa(s.etags[path]) + `"` }

func (s *Server) etagProp(path string) string {
	if s.NoETag {
		return ""
	}
	return `<D:getetag>` + esc(s.etag(path)) + `</D:getetag>`
}

// Objects returns the stored objects by path.
func (s *Server) Objects() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.objs {
		out[k] = v
	}
	return out
}

// Calendars lists the calendar paths.
func (s *Server) Calendars() []string { return s.paths(false) }

// AddressBooks lists the address book paths.
func (s *Server) AddressBooks() []string { return s.paths(true) }

// DisplayName is the collection's display name.
func (s *Server) DisplayName(path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols[path].name
}

func (s *Server) paths(book bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k, c := range s.cols {
		if c.book == book {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	redirect := s.Redirect
	s.mu.Unlock()
	if redirect != "" {
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}
	u, p, ok := r.BasicAuth()
	if !ok || u != s.User || p != s.Password {
		w.Header().Set("WWW-Authenticate", `Basic realm="Radicale"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Methods = append(s.Methods, r.Method+" "+r.URL.Path)
	home := "/" + s.User + "/"
	switch r.Method {
	case "PROPFIND":
		if r.URL.Path != home {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="utf-8"?><multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:CR="urn:ietf:params:xml:ns:carddav">`)
		b.WriteString(`<response><href>` + home + `</href><propstat><prop><resourcetype><collection/></resourcetype></prop><status>HTTP/1.1 200 OK</status></propstat></response>`)
		var paths []string
		for k := range s.cols {
			paths = append(paths, k)
		}
		sort.Strings(paths)
		for _, k := range paths {
			c := s.cols[k]
			rt := `<C:calendar/>`
			if c.book {
				rt = `<CR:addressbook/>`
			}
			b.WriteString(`<response><href>` + esc(k) + `</href><propstat><prop><resourcetype><collection/>` + rt + `</resourcetype><displayname>` + esc(c.name) + `</displayname>`)
			if len(c.comps) > 0 {
				b.WriteString(`<C:supported-calendar-component-set>`)
				for _, cp := range c.comps {
					b.WriteString(`<C:comp name="` + cp + `"/>`)
				}
				b.WriteString(`</C:supported-calendar-component-set>`)
			}
			b.WriteString(`</prop><status>HTTP/1.1 200 OK</status></propstat></response>`)
		}
		b.WriteString(`</multistatus>`)
		for k, c := range s.behind {
			s.cols[k] = c
		}
		s.behind = nil
		w.WriteHeader(http.StatusMultiStatus)
		io.WriteString(w, b.String())
	case "REPORT":
		c, ok := s.cols[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s.Reports = append(s.Reports, string(body))
		comp, data := "VEVENT", "C:calendar-data"
		if strings.Contains(string(body), `name="VTODO"`) {
			comp = "VTODO"
		}
		if c.book {
			comp, data = "VCARD", "CR:address-data"
		}
		var paths []string
		for k := range s.objs {
			if strings.HasPrefix(k, r.URL.Path) && strings.Contains(s.objs[k], "BEGIN:"+comp) {
				paths = append(paths, k)
			}
		}
		sort.Strings(paths)
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:CR="urn:ietf:params:xml:ns:carddav">`)
		for i, k := range paths {
			if c.book && s.StallAfter > 0 && i == s.StallAfter {
				w.WriteHeader(http.StatusMultiStatus)
				io.WriteString(w, b.String())
				w.(http.Flusher).Flush()
				s.mu.Unlock()
				<-r.Context().Done() // hang mid-body until the client gives up
				s.mu.Lock()
				return
			}
			b.WriteString(`<D:response><D:href>` + esc(k) + `</D:href><D:propstat><D:prop>` + s.etagProp(k) + `<` + data + `>` + esc(s.objs[k]) + `</` + data + `></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`)
		}
		b.WriteString(`</D:multistatus>`)
		w.WriteHeader(http.StatusMultiStatus)
		io.WriteString(w, b.String())
	case http.MethodPut:
		s.Header = append(s.Header, r.Header.Clone())
		i := strings.LastIndex(r.URL.Path, "/")
		if _, ok := s.cols[r.URL.Path[:i+1]]; !ok {
			w.WriteHeader(http.StatusConflict)
			return
		}
		_, exists := s.objs[r.URL.Path]
		if exists && s.RaceOnPut == r.URL.Path {
			s.RaceOnPut = ""
			s.version++
			s.etags[r.URL.Path] = s.version
		}
		if exists && r.Header.Get("If-None-Match") == "*" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if m := r.Header.Get("If-Match"); m != "" && (!exists || m != s.etag(r.URL.Path)) {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		s.store(r.URL.Path, string(body))
		w.WriteHeader(http.StatusCreated)
	case "MKCALENDAR":
		if s.MkcalendarStatus != 0 {
			w.WriteHeader(s.MkcalendarStatus)
			return
		}
		if _, ok := s.cols[r.URL.Path]; ok {
			code := s.ExistsStatus
			if code == 0 {
				code = http.StatusMethodNotAllowed
			}
			w.WriteHeader(code)
			return
		}
		var comps []string
		for _, cp := range []string{"VEVENT", "VTODO"} {
			if strings.Contains(string(body), `name="`+cp+`"`) {
				comps = append(comps, cp)
			}
		}
		name := ""
		if i, j := strings.Index(string(body), "<D:displayname>"), strings.Index(string(body), "</D:displayname>"); i >= 0 && j > i {
			name = string(body)[i+len("<D:displayname>") : j]
		}
		s.cols[r.URL.Path] = col{name: name, comps: comps}
		w.WriteHeader(http.StatusCreated)
	case "MKCOL":
		if s.MkcolStatus != 0 {
			w.WriteHeader(s.MkcolStatus)
			return
		}
		if _, ok := s.cols[r.URL.Path]; ok {
			code := s.ExistsStatus
			if code == 0 {
				code = http.StatusMethodNotAllowed
			}
			w.WriteHeader(code)
			return
		}
		var mk struct {
			Name string    `xml:"set>prop>displayname"`
			Book *struct{} `xml:"set>prop>resourcetype>addressbook"`
		}
		if err := xml.Unmarshal(body, &mk); err != nil || mk.Book == nil {
			w.WriteHeader(http.StatusForbidden) // a plain collection: not what the client should make
			return
		}
		s.cols[r.URL.Path] = col{name: mk.Name, book: true}
		w.WriteHeader(http.StatusCreated)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
