package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/paperless/paperlessfake"
)

// haus#13: `haus export-documents` is the refresh timer's first step. It
// mirrors Paperless into HAUS_DOCUMENTS_DIR and says what it did; unset,
// it does nothing and succeeds. Paperless in trouble does not fail the
// refresh (the notes are refreshed all the same), a folder Haus cannot
// write does; neither says the token.
func TestExportDocumentsCommand(t *testing.T) {
	fake := paperlessfake.New("pl-geheim")
	defer fake.Close()
	fake.Add(paperlessfake.Doc{ID: 7, Title: "Stromvertrag", Content: "Laufzeit bis 2027", Modified: "2026-10-01T12:00:00Z"})
	dir := t.TempDir()
	env := map[string]string{"HAUS_DOCUMENTS_DIR": dir, "HAUS_PAPERLESS_URL": fake.URL, "HAUS_PAPERLESS_TOKEN": "pl-geheim"}
	run := func() (int, string) {
		var out, errw bytes.Buffer
		code := command(context.Background(), []string{"export-documents"}, func(k string) string { return env[k] }, &out, &errw)
		return code, out.String() + errw.String()
	}
	if code, said := run(); code != 0 || !strings.Contains(said, "1 written, 0 unchanged, 0 removed") {
		t.Fatalf("%d %q", code, said)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "7.md")); err != nil || !strings.Contains(string(b), "Laufzeit bis 2027") {
		t.Fatalf("%q %v", b, err)
	}
	fake.Status = 500
	if code, said := run(); code != 0 || !strings.Contains(said, "stay as they were") || strings.Contains(said, "pl-geheim") {
		t.Fatalf("Paperless failing: %d %q", code, said)
	}
	fake.Status = 0
	env["HAUS_DOCUMENTS_DIR"] = filepath.Join(dir, "gibtsnicht")
	if code, said := run(); code != 1 || strings.Contains(said, "pl-geheim") {
		t.Fatalf("folder failing: %d %q", code, said)
	}
	env = map[string]string{}
	if code, said := run(); code != 0 || !strings.Contains(said, "nothing to export") {
		t.Fatalf("unset: %d %q", code, said)
	}
	var errw bytes.Buffer
	if code := command(context.Background(), []string{"bake-cake"}, func(string) string { return "" }, &errw, &errw); code != 2 {
		t.Fatalf("unknown: %d", code)
	}
}
