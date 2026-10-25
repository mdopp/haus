package main

import (
	"os"
	"strings"
	"testing"
)

// The image is scratch: what Haus needs from a system must be put in. The
// owner's shared Chefkoch recipe came back "Das Zertifikat der Seite stimmt
// nicht" (2026-10-25) because the image had no roots to check any https
// page against.
func TestImageCarriesTheRoots(t *testing.T) {
	b, err := os.ReadFile("../../Containerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt") {
		t.Error("the image has no CA certificates: every https page fails")
	}
}
