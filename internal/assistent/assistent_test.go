package assistent

import "testing"

func TestNameDefaultsToHomes(t *testing.T) {
	t.Cleanup(func() { Set("") })
	if Name() != "Homes" {
		t.Fatalf("unset: %q", Name())
	}
	Set("  Ada ")
	if Name() != "Ada" {
		t.Fatalf("set: %q", Name())
	}
	Set(" ")
	if Name() != "Homes" {
		t.Fatalf("blank: %q", Name())
	}
}
