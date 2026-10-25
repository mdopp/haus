// Package merken makes a note from a link (haus#117; owner 2026-10-25:
// "faul und einfach"): Haus fetches the page itself, from the box, reads
// what the page says of itself in machine words (schema.org's JSON-LD: a
// recipe, an album's review, a hotel or an offer) and its meta tags and
// text, asks the house model for the core where the page carries no such
// data, and writes one note with its kind, tags and the link. The page's
// words stay with the note, so it outlives the page.
//
// It fetches only public addresses (never the house's own network, never
// the box), at most maxPage bytes, within fetchWait, at most five
// redirects, each checked again.
package merken

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxPage   = 3 << 20
	fetchWait = 20 * time.Second
	userAgent = "Mozilla/5.0 (compatible; Haus; +https://github.com/mdopp/haus) Notizen"
)

// What the family reads when a page cannot be had.
var (
	ErrNoAddress = errors.New("Das ist keine Webadresse. Sie beginnt mit https:// oder http://.")
	ErrPrivate   = errors.New("Diese Adresse liegt im eigenen Netz; Haus merkt sich nur Seiten aus dem Internet.")
	ErrNoAnswer  = errors.New("Die Seite antwortet gerade nicht. Später noch einmal versuchen.")
	ErrNoPage    = errors.New("Unter dieser Adresse ist keine Webseite.")
)

// Fetcher gets pages. AllowPrivate is for the tests, whose pages are on
// 127.0.0.1; never on a box.
type Fetcher struct {
	AllowPrivate bool
	Wait         time.Duration // 0: fetchWait
}

// public says an address may be fetched: not loopback, not the house's
// networks, not link-local, not CGNAT, not multicast or unspecified.
func public(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast() {
		return false
	}
	if a.Is4() && netip.MustParsePrefix("100.64.0.0/10").Contains(a) {
		return false
	}
	return true
}

// client is an HTTP client whose every connection is checked: the address
// it resolves to must be public, so a name pointing home is refused too.
func (f Fetcher) client() *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			// every allowed address in turn: the first may be one this
			// network cannot reach (an IPv6 one without a route)
			var last error = ErrPrivate
			for _, ip := range ips {
				if !f.AllowPrivate && !public(ip) {
					continue
				}
				c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return c, nil
				}
				last = err
			}
			return nil, last
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	return &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("zu viele Weiterleitungen")
		}
		return checkURL(req.URL)
	}}
}

// checkURL is an address Haus fetches: http(s), a host, no login in it.
func checkURL(u *url.URL) error {
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return ErrNoAddress
	}
	return nil
}

// Address reads what someone typed or sent as an address: the first
// http(s) address in it, without trailing punctuation.
func Address(s string) (*url.URL, error) {
	for _, w := range strings.Fields(s) {
		w = strings.TrimRight(strings.Trim(w, "<>\"'()[]"), ".,;:!?")
		if strings.HasPrefix(w, "http://") || strings.HasPrefix(w, "https://") {
			u, err := url.Parse(w)
			if err == nil && checkURL(u) == nil {
				return u, nil
			}
		}
	}
	return nil, ErrNoAddress
}

// Fetch gets a page: its final address and its text as UTF-8.
func (f Fetcher) Fetch(ctx context.Context, u *url.URL) (string, string, error) {
	final, ct, raw, charset, err := f.Get(ctx, u)
	if err != nil {
		return "", "", err
	}
	if ct != "" && ct != "text/html" && ct != "application/xhtml+xml" {
		return "", "", ErrNoPage
	}
	return final, toUTF8(raw, charset), nil
}

// Get fetches whatever is at an address (a page, a feed, an API's JSON),
// by the same rules: its final address, its media type, its bytes and
// the charset the server named.
func (f Fetcher) Get(ctx context.Context, u *url.URL) (string, string, []byte, string, error) {
	if err := checkURL(u); err != nil {
		return "", "", nil, "", err
	}
	if !f.AllowPrivate {
		if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !public(ip) {
			return "", "", nil, "", ErrPrivate
		}
		if h := strings.ToLower(u.Hostname()); h == "localhost" || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".box") || strings.HasSuffix(h, ".lan") || strings.HasSuffix(h, ".home.arpa") || !strings.Contains(h, ".") {
			return "", "", nil, "", ErrPrivate
		}
	}
	wait := f.Wait
	if wait == 0 {
		wait = fetchWait
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", "", nil, "", ErrNoAddress
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "de-DE,de;q=0.9,en;q=0.5")
	res, err := f.client().Do(req)
	if err != nil {
		if errors.Is(err, ErrPrivate) {
			return "", "", nil, "", ErrPrivate
		}
		var ue *url.Error
		if errors.As(err, &ue) && errors.Is(ue.Err, ErrNoAddress) {
			return "", "", nil, "", ErrNoAddress
		}
		log.Printf("haus: merken: %s: %v", u.Host, err) // why it did not answer: the name, the way, the time
		return "", "", nil, "", why(u.Hostname(), err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return "", "", nil, "", fmt.Errorf("Die Seite sagt „%d“: %w", res.StatusCode, ErrNoPage)
	}
	ct, params, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxPage))
	if err != nil {
		log.Printf("haus: merken: %s: reading: %v", u.Host, err)
		return "", "", nil, "", &NoAnswer{Reason: u.Hostname() + " hat die Seite nicht ganz geschickt; die Verbindung brach ab.", Next: "Gleich noch einmal teilen."}
	}
	return res.Request.URL.String(), ct, raw, params["charset"], nil
}

// toUTF8 is a page's bytes as text: UTF-8 as it is, Latin-1 or
// Windows-1252 byte by byte (near enough for a note), a <meta charset>
// looked for when the header said nothing.
func toUTF8(b []byte, charset string) string {
	cs := strings.ToLower(charset)
	if cs == "" {
		head := strings.ToLower(string(b[:min(len(b), 2048)]))
		if i := strings.Index(head, "charset="); i >= 0 {
			cs = strings.Trim(strings.SplitN(head[i+8:], `"`, 2)[0], `'" />`)
		}
	}
	if (cs == "iso-8859-1" || cs == "latin1" || cs == "windows-1252") && !utf8.Valid(b) {
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r)
	}
	return strings.ToValidUTF8(string(b), "�")
}

// NoAnswer is a page that did not answer, and why, in the family's words:
// the reason and what to do. It is ErrNoAnswer for errors.Is.
type NoAnswer struct {
	Reason, Next string
}

func (e *NoAnswer) Error() string        { return e.Reason + " " + e.Next }
func (e *NoAnswer) Is(target error) bool { return target == ErrNoAnswer }

// why is a transport error as a reason: the name not found, no way out,
// no answer in time, a refused or broken secure connection.
func why(host string, err error) error {
	var dns *net.DNSError
	var op *net.OpError
	var cert *tls.CertificateVerificationError
	switch {
	case errors.As(err, &dns) && dns.IsNotFound:
		return &NoAnswer{Reason: "Die Box findet " + host + " nicht: Diesen Namen gibt es im Internet nicht.", Next: "Ist der Link vollständig?"}
	case errors.As(err, &dns):
		return &NoAnswer{Reason: "Die Box konnte " + host + " nicht nachschlagen: Die Namensauflösung (DNS) antwortet nicht.", Next: "Später noch einmal teilen; bleibt es so, liegt es an der Box."}
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return &NoAnswer{Reason: host + " hat nicht rechtzeitig geantwortet (" + fetchWait.String() + ").", Next: "Später noch einmal teilen."}
	case errors.As(err, &cert):
		return &NoAnswer{Reason: "Die sichere Verbindung zu " + host + " kam nicht zustande: Das Zertifikat der Seite stimmt nicht.", Next: "Die Seite merkt sich Haus so nicht."}
	case errors.As(err, &op) && op.Op == "dial":
		return &NoAnswer{Reason: "Die Box kommt nicht zu " + host + " durch: keine Verbindung ins Internet von Haus aus.", Next: "Bleibt es so, liegt es an der Box; sag Bescheid."}
	}
	return &NoAnswer{Reason: host + " hat die Verbindung abgelehnt oder abgebrochen.", Next: "Später noch einmal teilen."}
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}
