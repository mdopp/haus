package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/usage"
	"github.com/mdopp/haus/internal/usage/usagefake"
)

// #50: the routes count, behind the proxy secret only: a page Caddy let
// through is a view; one without the secret (403), a path Haus does not
// serve (404) and a /napi call without a paired phone (401) are not.
func TestRoutesCountPageViews(t *testing.T) {
	fake := usagefake.New("um")
	t.Cleanup(fake.Close)
	s := testSite(t)
	s.usage = usage.New(fake.URL, "um")
	h := routes(s)
	for _, q := range []struct{ path, secret string }{
		{"/start", "geheim"},
		{"/start?von=widget.device", "geheim"},
		{"/start", ""},
		{"/gibtsnicht", "geheim"},
		{"/napi/box/home", ""},
		{"/healthz", ""},
	} {
		req := httptest.NewRequest("GET", q.path, nil)
		req.Header.Set("Remote-User", "anna")
		if q.secret != "" {
			req.Header.Set("X-Haus-Proxy", q.secret)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	s.usage.Flush(context.Background())
	got := map[string]int64{}
	for k, n := range fake.Counts() {
		got[k.App+" "+k.Event] += n
	}
	// testSite's Laib does not answer: Start says so at the bottom, and that line counts too
	if want := map[string]int64{"haus seite.start": 2, "haus von.widget.device": 1, "haus gestoert.start.box": 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("counted %v, want %v", got, want)
	}
	for _, b := range fake.Bodies() {
		if strings.Contains(b, "anna") || strings.Contains(b, "geheim") {
			t.Errorf("left Haus: %s", b)
		}
	}
}

// #50, for laib's boot test: Haus stopped with SIGTERM (podman stop) sends
// what it counted before it exits, and runs on the environment alone.
func TestSIGTERMFlushes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and starts the binary")
	}
	fake := usagefake.New("um")
	t.Cleanup(fake.Close)
	bin := filepath.Join(t.TempDir(), "haus")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "LAIB_TOKEN=ops", "LAIB_URL=http://127.0.0.1:1", "HAUS_PROXY_SECRET=geheim", "HAUS_DATA="+t.TempDir(),
		"HAUS_LISTEN="+addr, "HAUS_USAGE_URL="+fake.URL, "HAUS_USAGE_TOKEN=um", "HAUS_PERSONA_DIR="+t.TempDir())
	var logs strings.Builder
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	var resp *http.Response
	for i := 0; i < 100; i++ {
		req, _ := http.NewRequest("GET", "http://"+addr+"/start", nil)
		req.Header.Set("X-Haus-Proxy", "geheim")
		req.Header.Set("Remote-User", "anna")
		if resp, err = http.DefaultClient.Do(req); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("haus did not answer: %v\n%s", err, logs.String())
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/start: %d\n%s", resp.StatusCode, logs.String())
	}
	if len(fake.Counts()) != 0 {
		t.Fatalf("sent before the flush: %v", fake.Counts())
	}
	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("haus did not stop on SIGTERM\n%s", logs.String())
	}
	if n := fake.Counts()[usagefake.Count{App: "haus", Event: "seite.start", Day: time.Now().UTC().Format(time.DateOnly)}]; n != 1 {
		t.Errorf("seite.start %d after SIGTERM, want 1: %v\n%s", n, fake.Counts(), logs.String())
	}
}
