package pages

import "testing"

// An address a template value left without its domain ("https://caldav.")
// is completed with the box's, from Haus's own; a whole one stays.
func TestOnDomain(t *testing.T) {
	for _, c := range []struct{ raw, haus, want string }{
		{"https://caldav.", "https://haus.dopp.cloud", "https://caldav.dopp.cloud"},
		{"https://home./", "https://haus.dopp.cloud/", "https://home.dopp.cloud/"},
		{"https://caldav.dopp.cloud", "https://haus.dopp.cloud", "https://caldav.dopp.cloud"},
		{"https://caldav.", "", "https://caldav."},
		{"", "https://haus.dopp.cloud", ""},
	} {
		if got := OnDomain(c.raw, c.haus); got != c.want {
			t.Errorf("OnDomain(%q, %q) = %q, want %q", c.raw, c.haus, got, c.want)
		}
	}
}
