package devices

import (
	"strings"
	"testing"
)

func TestMintLookupRevoke(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d, tok, err := s.Mint("Pixel", "mdopp")
	if err != nil || !strings.HasPrefix(tok, "haus_device_") || d.UID != "mdopp" {
		t.Fatalf("mint: %v %q %+v", err, tok, d)
	}
	if got, ok := s.Lookup(tok); !ok || got.ID != d.ID || got.LastSeen.IsZero() {
		t.Fatalf("lookup: %v %+v", ok, got)
	}
	if _, ok := s.Lookup("haus_device_nope"); ok {
		t.Fatal("unknown token accepted")
	}
	// haus#26: a device paired before the new app is not migrated; its hash
	// in devices.json matches nothing, the phone pairs again
	old := "old_device_" + strings.Repeat("x", 43)
	s.list = append(s.list, Device{ID: "alt", Name: "Altes Telefon", UID: "mdopp", Hash: hash(old)})
	if _, ok := s.Lookup(old); ok {
		t.Fatal("a token without haus_device_ accepted")
	}
	s.list = s.list[:len(s.list)-1]
	// the file holds the hash, never the token
	s2, _ := Open(s.path[:len(s.path)-len("/devices.json")])
	if _, ok := s2.Lookup(tok); !ok {
		t.Fatal("token lost on reload")
	}
	if strings.Contains(string(mustRead(t, s.path)), tok) {
		t.Fatal("plaintext token on disk")
	}
	if err := s.Revoke(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Lookup(tok); ok {
		t.Fatal("revoked token still valid")
	}
}
