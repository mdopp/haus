package pages

import (
	"html"
	"net/url"
	"strings"
)

// App is another app of the box a family page hands over to, for what
// Haus does not do itself (haus#64): "Weiter in <App>", one pattern on
// every page — Home Assistant on Geräte now, Paperless (haus#68) and the
// calendar (haus#66) the same way. Name is the app's name for the family,
// URL its public address from the config; no URL, no link.
type App struct{ Name, URL string }

// AppURL is a public address from the config: http(s)://host[/…], without
// a trailing slash; anything else is "" (no link rather than a wrong one).
func AppURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return raw
}

// OnDomain completes an address whose host ends in a dot ("https://caldav."
// from a template value "https://caldav.${DOMAIN}" that reached Haus with
// ${DOMAIN} empty) with the box's domain, taken from Haus's own public
// address (haus.<domain>); else the address as it is.
func OnDomain(raw, haus string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.HasSuffix(u.Host, ".") {
		return raw
	}
	h, err := url.Parse(strings.TrimSpace(haus))
	if err != nil || h.Host == "" {
		return raw
	}
	_, domain, ok := strings.Cut(h.Hostname(), ".")
	if !ok || domain == "" {
		return raw
	}
	u.Host += domain
	return u.String()
}

// Set says the app has an address.
func (a App) Set() bool { return a.URL != "" }

// href is path in the app ("/config/devices/device/<id>").
func (a App) href(path string) string { return a.URL + path }

// Weiter is the page's way on (haus#64): "In <App> öffnen" to path, and
// in a quiet line what the family finds there. "" without an address.
func (a App) Weiter(path, what string) string {
	if !a.Set() {
		return ""
	}
	b := `<p class=weiter-in><a href="` + html.EscapeString(a.href(path)) + `">In ` + html.EscapeString(a.Name) + " öffnen</a>"
	if what != "" {
		b += " <span class=k>" + html.EscapeString(what) + "</span>"
	}
	return b + "</p>"
}

// homeAssistant is Home Assistant as Geräte hands over to it.
func (cfg StartConfig) homeAssistant() App { return App{"Home Assistant", cfg.HAPublicURL} }

// haThing is a thing's own page in Home Assistant: its device's
// (/config/devices/device/<id>), else the entity's history.
func haThing(id, device string) string {
	if device != "" {
		return "/config/devices/device/" + url.PathEscape(device)
	}
	return "/history?entity_id=" + url.QueryEscape(id)
}
