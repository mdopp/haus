package caldav

import "testing"

func TestLegacyAccountPath(t *testing.T) {
	for _, c := range []struct{ path, user, want string }{
		{"/haus/haushalt/", "homes", "/homes/haushalt/"},
		{"/haus/familie-urlaub/", "homes", "/homes/familie-urlaub/"},
		{"/homes/haushalt/", "homes", "/homes/haushalt/"},
		{"/lea/abo-feiertage/", "homes", "/lea/abo-feiertage/"},
		{"/hausmeister/privat-x/", "homes", "/hausmeister/privat-x/"}, // a person whose name starts with haus
		{"/haus/haushalt/", "haus", "/haus/haushalt/"},
		{"/haus/haushalt/", "", "/haus/haushalt/"},
		{"haus/haushalt/", "homes", "haus/haushalt/"},
	} {
		if got := LegacyAccountPath(c.path, c.user); got != c.want {
			t.Errorf("LegacyAccountPath(%q, %q) = %q, want %q", c.path, c.user, got, c.want)
		}
	}
}
