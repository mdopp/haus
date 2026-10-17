// Package sambafake is file-share's end of the network-drive passwords
// (laib#296, samba-accounts.sh) for tests: a real FIFO held open, each line
// read and recorded, and the status written as the script writes it.
package sambafake

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// FileShare is the fake. Dir is HAUS_SAMBA_DIR; Lines gets each line read.
type FileShare struct {
	Dir   string
	Lines chan string
	mu    sync.Mutex
	at    map[string]int64
}

// New starts one with these members (name → unix time of their password,
// 0: none). confirm false: it reads and never confirms.
func New(t testing.TB, members map[string]int64, confirm bool) *FileShare {
	t.Helper()
	fs := &FileShare{Dir: t.TempDir(), Lines: make(chan string, 16), at: members}
	if err := syscall.Mkfifo(filepath.Join(fs.Dir, "passwords"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs.write()
	f, err := os.OpenFile(filepath.Join(fs.Dir, "passwords"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	go func() {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs.Lines <- sc.Text()
			if !confirm {
				continue
			}
			op, rest, _ := strings.Cut(sc.Text(), " ")
			name, _, _ := strings.Cut(rest, " ")
			fs.mu.Lock()
			if op == "set" {
				fs.at[name] = time.Now().Unix()
			} else {
				fs.at[name] = 0
			}
			fs.mu.Unlock()
			fs.write()
		}
	}()
	return fs
}

// NoLine fails when a line arrives within a moment.
func (fs *FileShare) NoLine(t testing.TB) {
	t.Helper()
	select {
	case l := <-fs.Lines:
		t.Fatalf("a line reached file-share: %q", l)
	case <-time.After(100 * time.Millisecond):
	}
}

func (fs *FileShare) write() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	names := make([]string, 0, len(fs.at))
	for n := range fs.at {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "%s %d\n", n, fs.at[n])
	}
	os.WriteFile(filepath.Join(fs.Dir, "status.tmp"), []byte(b.String()), 0o644)
	os.Rename(filepath.Join(fs.Dir, "status.tmp"), filepath.Join(fs.Dir, "status"))
}
