// Package samba hands a person's network-drive password to file-share's
// Samba (laib#296, haus#52). File-share's samba-accounts.sh holds a FIFO
// open in HAUS_SAMBA_DIR; Haus writes one line into it, "set <name>
// <password, base64>" or "clear <name>", and reads back the status file,
// "<name> <unix time the password was set, 0: none>" for each family
// member and admin. The password never touches a disk, an argv or a log:
// it sits in the pipe until samba hands it to smbpasswd on stdin.
package samba

import (
	"bufio"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

// Dir is file-share's samba-accounts folder as Haus mounts it.
type Dir struct{ Path string }

// FromEnv is HAUS_SAMBA_DIR; nil when unset: no page.
func FromEnv() *Dir {
	if p := os.Getenv("HAUS_SAMBA_DIR"); p != "" {
		return &Dir{Path: p}
	}
	return nil
}

// What can go wrong, in the words the page builds on.
var (
	ErrNoShare     = errors.New("no file-share on this box (no status)")
	ErrNotRunning  = errors.New("samba is not running (no reader on the FIFO)")
	ErrNotMember   = errors.New("not in family or admins")
	ErrPassword    = errors.New("password: 8 to 128 characters, one line")
	ErrUnconfirmed = errors.New("handed over, but samba did not confirm it")
)

// Wait is how long Set and Clear wait for samba's status. A variable for the tests.
var Wait = 10 * time.Second

// MinLen and MaxLen bound a password; the line stays far below PIPE_BUF, so
// it arrives in one piece.
const MinLen, MaxLen = 8, 128

var name = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`) // as samba-accounts.sh takes them

// Accounts is the status: each member, and since when they have a password
// (zero: none). ErrNoShare without file-share.
func (d *Dir) Accounts() (map[string]time.Time, error) {
	f, err := os.Open(filepath.Join(d.Path, "status"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoShare
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]time.Time{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n, at, ok := strings.Cut(sc.Text(), " ")
		if !ok || !name.MatchString(n) {
			continue
		}
		var t time.Time
		if s, err := strconv.ParseInt(at, 10, 64); err == nil && s > 0 {
			t = time.Unix(s, 0)
		}
		out[n] = t
	}
	return out, sc.Err()
}

// Set gives who a new password and waits until samba says so.
func (d *Dir) Set(who, password string) error {
	if !okPassword(password) {
		return ErrPassword
	}
	since := time.Now().Truncate(time.Second)
	return d.send(who, "set "+who+" "+base64.StdEncoding.EncodeToString([]byte(password)), func(t time.Time) bool { return !t.Before(since) })
}

// Clear takes who's password away, and with it the account.
func (d *Dir) Clear(who string) error {
	return d.send(who, "clear "+who, time.Time.IsZero)
}

func okPassword(p string) bool {
	n := utf8.RuneCountInString(p)
	return utf8.ValidString(p) && n >= MinLen && n <= MaxLen && strings.IndexFunc(p, unicode.IsControl) < 0
}

func (d *Dir) send(who, line string, done func(time.Time) bool) error {
	if !name.MatchString(who) {
		return ErrNotMember
	}
	acc, err := d.Accounts()
	if err != nil {
		return err
	}
	if _, ok := acc[who]; !ok {
		return ErrNotMember
	}
	if err := d.write(line + "\n"); err != nil {
		return err
	}
	for end := time.Now().Add(Wait); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if acc, err := d.Accounts(); err == nil {
			if t, ok := acc[who]; ok && done(t) {
				return nil
			}
		}
	}
	return ErrUnconfirmed
}

// write puts the line into the FIFO in one write, never into anything else:
// a plain file where the FIFO belongs would keep the password on disk.
// Non-blocking: no reader (samba down) is an error at once, not a hang.
func (d *Dir) write(line string) error {
	p := filepath.Join(d.Path, "passwords")
	if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeNamedPipe == 0 {
		return ErrNotRunning
	}
	f, err := os.OpenFile(p, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ErrNotRunning // ENXIO: nobody reads
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || fi.Mode()&os.ModeNamedPipe == 0 {
		return ErrNotRunning
	}
	if _, err := f.Write([]byte(line)); err != nil {
		return ErrNotRunning
	}
	return nil
}
