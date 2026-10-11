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
	if err != nil || !strings.HasPrefix(tok, "sol_device_") || d.UID != "mdopp" {
		t.Fatalf("mint: %v %q %+v", err, tok, d)
	}
	if got, ok := s.Lookup(tok); !ok || got.ID != d.ID || got.LastSeen.IsZero() {
		t.Fatalf("lookup: %v %+v", ok, got)
	}
	if _, ok := s.Lookup("sol_device_nope"); ok {
		t.Fatal("unknown token accepted")
	}
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
