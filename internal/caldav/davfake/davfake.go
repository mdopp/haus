// Package davfake is a fake Radicale for tests: Basic auth, PROPFIND on the
// account's home, REPORT calendar-query on a collection, PUT with
// If-None-Match, MKCALENDAR. It filters a REPORT by component only; the
// client filters the time range itself, and tests read the query it sent.
package davfake

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
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
	Reports []string          // REPORT bodies, in order
	Methods []string          // "METHOD path", in order
	Header  []http.Header     // the PUT headers, in order

	// ExistsStatus is MKCALENDAR's answer for a calendar that is there
	// (default 405, as Radicale; some servers say 409).
	ExistsStatus int
	// MkcalendarStatus, when set, is MKCALENDAR's answer whatever the state (403: no right to).
	MkcalendarStatus int
	behind           map[string]col // made by "another client" right after the next PROPFIND
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

type col struct {
	name  string
	comps []string
}

// New starts a fake for one account.
func New(user, password string) *Server {
	s := &Server{User: user, Password: password, cols: map[string]col{}, objs: map[string]string{}}
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

// Put stores an object as a phone would.
func (s *Server) Put(path, ics string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objs[path] = ics
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
func (s *Server) Calendars() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.cols {
		out = append(out, k)
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
		b.WriteString(`<?xml version="1.0" encoding="utf-8"?><multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">`)
		b.WriteString(`<response><href>` + home + `</href><propstat><prop><resourcetype><collection/></resourcetype></prop><status>HTTP/1.1 200 OK</status></propstat></response>`)
		var paths []string
		for k := range s.cols {
			paths = append(paths, k)
		}
		sort.Strings(paths)
		for _, k := range paths {
			c := s.cols[k]
			b.WriteString(`<response><href>` + esc(k) + `</href><propstat><prop><resourcetype><collection/><C:calendar/></resourcetype><displayname>` + esc(c.name) + `</displayname>`)
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
		if _, ok := s.cols[r.URL.Path]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s.Reports = append(s.Reports, string(body))
		comp := "VEVENT"
		if strings.Contains(string(body), `name="VTODO"`) {
			comp = "VTODO"
		}
		var paths []string
		for k := range s.objs {
			if strings.HasPrefix(k, r.URL.Path) && strings.Contains(s.objs[k], "BEGIN:"+comp) {
				paths = append(paths, k)
			}
		}
		sort.Strings(paths)
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">`)
		for _, k := range paths {
			b.WriteString(`<D:response><D:href>` + esc(k) + `</D:href><D:propstat><D:prop><D:getetag>"1"</D:getetag><C:calendar-data>` + esc(s.objs[k]) + `</C:calendar-data></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`)
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
		if _, exists := s.objs[r.URL.Path]; exists && r.Header.Get("If-None-Match") == "*" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		s.objs[r.URL.Path] = string(body)
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
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
