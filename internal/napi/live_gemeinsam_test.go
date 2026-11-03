package napi

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/caldav/davfake"
)

// haus#223: a change of the household's shared contacts or calendars goes
// to every open stream as one gemeinsam frame of its part, found by the
// collections' getctag; a list's change and no change send nothing.
func TestSharedChangesBecomeOneFrameOfTheirPart(t *testing.T) {
	fake := davfake.New("homes", "pw")
	defer fake.Close()
	fake.AddCalendar("/homes/haushalt/", "Haushalt", "VEVENT", "VTODO")
	fake.AddCalendar("/homes/einkauf/", "Einkauf", "VTODO")
	fake.AddAddressBook("/homes/haushalt-kontakte/", "Haushalt")
	dav := &caldav.Client{URL: fake.URL, User: "homes", Password: "pw"}

	l := newLive(nil, time.Minute, time.Second)
	var mu sync.Mutex
	n := 0
	l.shared = func(ctx context.Context) (string, string, error) {
		k, c, err := dav.FamilyVersions(ctx)
		mu.Lock()
		n++
		mu.Unlock()
		return k, c, err
	}
	l.sharedEvery = 20 * time.Millisecond
	phone := &listener{device: "p", ch: make(chan []byte, 16), kick: make(chan struct{})}
	l.mu.Lock()
	l.subs[phone] = true
	l.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.watchShared(ctx)

	rounds := func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
	// wait for two more reads than now: the change is seen, whatever the phase
	settle := func() {
		want := rounds() + 2
		for i := 0; i < 200 && rounds() < want; i++ {
			time.Sleep(5 * time.Millisecond)
		}
	}
	frames := func() []string {
		var out []string
		for {
			select {
			case f := <-phone.ch:
				out = append(out, string(f))
			default:
				return out
			}
		}
	}
	settle()
	if f := frames(); len(f) != 0 {
		t.Fatalf("the baseline sent %q", f)
	}
	fake.Put("/homes/einkauf/milch.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:milch\r\nSUMMARY:Milch\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	settle()
	if f := frames(); len(f) != 0 {
		t.Fatalf("the shopping list is no calendar of the phones: %q", f)
	}
	fake.Put("/homes/haushalt/arzt.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:arzt\r\nSUMMARY:Arzt\r\nDUE:20261104T080000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	settle()
	if f := frames(); len(f) != 1 || f[0] != "event: gemeinsam\ndata: {\"teil\":\"kalender\"}\n\n" {
		t.Fatalf("a new reminder: %q", f)
	}
	fake.Put("/homes/haushalt-kontakte/oma.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:oma\r\nFN:Oma\r\nEND:VCARD\r\n")
	settle()
	if f := frames(); len(f) != 1 || f[0] != "event: gemeinsam\ndata: {\"teil\":\"kontakte\"}\n\n" {
		t.Fatalf("a new card: %q", f)
	}
	settle()
	if f := frames(); len(f) != 0 {
		t.Fatalf("no change, yet %q", f)
	}
}

// Nobody listening, Homes asks Radicale nothing; the first stream starts the
// look, the last one leaving stops it.
func TestSharedIsReadOnlyWhileSomeoneListens(t *testing.T) {
	fake := davfake.New("homes", "pw")
	defer fake.Close()
	fake.AddCalendar("/homes/haushalt/", "Haushalt")
	l := newLive(nil, time.Minute, time.Second)
	reads := make(chan struct{}, 100)
	l.shared, l.sharedEvery = func(ctx context.Context) (string, string, error) {
		reads <- struct{}{}
		return "", "", nil
	}, 10*time.Millisecond
	time.Sleep(50 * time.Millisecond)
	if len(reads) != 0 {
		t.Fatal("read with nobody listening")
	}
	me := &listener{device: "p", ch: make(chan []byte, 4), kick: make(chan struct{})}
	l.join(me)
	time.Sleep(50 * time.Millisecond)
	if len(reads) == 0 {
		t.Fatal("not read while a phone listens")
	}
	l.leave(me)
	time.Sleep(30 * time.Millisecond)
	for len(reads) > 0 {
		<-reads
	}
	time.Sleep(50 * time.Millisecond)
	if len(reads) != 0 {
		t.Fatal("still read after the last stream left")
	}
}
