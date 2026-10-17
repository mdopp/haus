package samba

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/samba/sambafake"
)

// file-share's end is sambafake: a real FIFO, read as samba-accounts.sh reads it (laib#296).

func TestSetHandsTheLineToFileShareAndWaitsForIt(t *testing.T) {
	fs := sambafake.New(t, map[string]int64{"anna": 0, "ben": 1700000000}, true)
	d := &Dir{Path: fs.Dir}
	pw := "Geheim äö 1 -x"
	if err := d.Set("anna", pw); err != nil {
		t.Fatal(err)
	}
	if got := <-fs.Lines; got != "set anna "+base64.StdEncoding.EncodeToString([]byte(pw)) {
		t.Fatalf("the line file-share reads: %q", got)
	}
	acc, err := d.Accounts()
	if err != nil || acc["anna"].IsZero() || acc["ben"] != time.Unix(1700000000, 0) {
		t.Fatalf("anna has one now, ben since 2023: %v %v", acc, err)
	}
	if err := d.Clear("ben"); err != nil || <-fs.Lines != "clear ben" {
		t.Fatalf("clear: %v", err)
	}
	if acc, _ := d.Accounts(); !acc["ben"].IsZero() {
		t.Fatal("ben has none after the reset")
	}
}

func TestWhatIsRefusedWritesNothing(t *testing.T) {
	fs := sambafake.New(t, map[string]int64{"anna": 0}, true)
	d := &Dir{Path: fs.Dir}
	for _, c := range []struct{ name, pw string }{{"anna", "kurz"}, {"anna", "zwei\nZeilen sind es"}, {"anna", strings.Repeat("x", 129)}, {"anna", "\xff\xfe ungültig"}, {"../x", "lang genug"}, {"Anna", "lang genug"}} {
		if err := d.Set(c.name, c.pw); err == nil {
			t.Errorf("%q %q: no error", c.name, c.pw)
		}
	}
	fs.NoLine(t)
	if err := d.Set("ben", "lang genug"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("ben is no member: %v", err)
	}
}

func TestNoReaderNoFIFOOrNoConfirmationIsSaid(t *testing.T) {
	Wait = 300 * time.Millisecond
	// samba not running: the FIFO has no reader
	dir := t.TempDir()
	syscall.Mkfifo(filepath.Join(dir, "passwords"), 0o600)
	os.WriteFile(filepath.Join(dir, "status"), []byte("anna 0\n"), 0o644)
	if err := (&Dir{Path: dir}).Set("anna", "lang genug"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("no reader: %v", err)
	}
	// a plain file where the FIFO belongs: the password is never written to disk
	dir = t.TempDir()
	os.WriteFile(filepath.Join(dir, "passwords"), nil, 0o600)
	os.WriteFile(filepath.Join(dir, "status"), []byte("anna 0\n"), 0o644)
	if err := (&Dir{Path: dir}).Set("anna", "lang genug"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("a file, not a FIFO: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "passwords")); len(b) != 0 {
		t.Fatalf("the password went into a file: %q", b)
	}
	// no file-share at all
	if _, err := (&Dir{Path: t.TempDir()}).Accounts(); !errors.Is(err, ErrNoShare) {
		t.Fatalf("no status: %v", err)
	}
	// read, never confirmed
	fs := sambafake.New(t, map[string]int64{"anna": 0}, false)
	if err := (&Dir{Path: fs.Dir}).Set("anna", "lang genug"); !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("unconfirmed: %v", err)
	}
}
