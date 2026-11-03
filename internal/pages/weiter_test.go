package pages

import "testing"

// An address a template value left without its domain ("https://caldav.")
// is completed with the box's, from Homes' own; a whole one stays.
func TestOnDomain(t *testing.T) {
	for _, c := range []struct{ raw, homes, want string }{
		{"https://caldav.", "https://homes.dopp.cloud", "https://caldav.dopp.cloud"},
		{"https://home./", "https://homes.dopp.cloud/", "https://home.dopp.cloud/"},
		{"https://caldav.dopp.cloud", "https://homes.dopp.cloud", "https://caldav.dopp.cloud"},
		{"https://caldav.", "", "https://caldav."},
		{"", "https://homes.dopp.cloud", ""},
	} {
		if got := OnDomain(c.raw, c.homes); got != c.want {
			t.Errorf("OnDomain(%q, %q) = %q, want %q", c.raw, c.homes, got, c.want)
		}
	}
}
