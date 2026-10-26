package podcasts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/abs/absfake"
	"github.com/mdopp/haus/internal/merken"
)

func service(t *testing.T) (*Service, *absfake.Server, string) {
	t.Helper()
	fake := absfake.New("k")
	t.Cleanup(fake.Close)
	feeds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(`<?xml version="1.0"?><rss><channel><title>x</title></channel></rss>`))
	}))
	t.Cleanup(feeds.Close)
	fake.AddFeed(feeds.URL+"/lage", "Lage der Nation")
	fake.AddFeed(feeds.URL+"/zeit", "Was jetzt?")
	s := Open(&abs.Client{URL: fake.URL, Token: "k"}, t.TempDir())
	s.Fetch = merken.Fetcher{AllowPrivate: true}
	return s, fake, feeds.URL
}

// haus#123: subscribing by a link puts the podcast into ABS once, set to
// keep only the last three, and notes the person; a second person shares
// it; the same person again is told so. Ending: the person goes, the last
// one takes the podcast off the box.
func TestAbonnierenUndBeenden(t *testing.T) {
	s, fake, feeds := service(t)
	ctx := context.Background()
	got, err := s.Abonnieren(ctx, "anna", feeds+"/lage")
	if err != nil || got.Title != "Lage der Nation" || !strings.Contains(got.Satz, "Die letzten 3 Folgen") {
		t.Fatalf("%+v %v", got, err)
	}
	if len(fake.Items()) != 1 || len(fake.Patches) != 1 || fake.Patches[0]["maxEpisodesToKeep"] != float64(3) || fake.Patches[0]["maxNewEpisodesToDownload"] != float64(3) {
		t.Fatalf("items %v patches %v", fake.Items(), fake.Patches)
	}
	if again, _ := s.Abonnieren(ctx, "anna", feeds+"/lage"); !again.Schon {
		t.Fatalf("again %+v", again)
	}
	if _, err := s.Abonnieren(ctx, "ben", feeds+"/lage"); err != nil || len(fake.Items()) != 1 {
		t.Fatalf("ben: %v %v", err, fake.Items())
	}
	if _, err := s.Abonnieren(ctx, "", feeds+"/lage"); err == nil {
		t.Error("nobody subscribed")
	}
	meine, _ := s.Meine("ben")
	if len(meine) != 1 || !reflect.DeepEqual(meine[0].Personen, []string{"anna", "ben"}) {
		t.Fatalf("%+v", meine)
	}
	if say, err := s.Beenden(ctx, "anna", meine[0].Feed); err != nil || !strings.Contains(say, "Andere hören ihn weiter") || len(fake.Removed) != 0 {
		t.Fatalf("%q %v %v", say, err, fake.Removed)
	}
	if say, err := s.Beenden(ctx, "ben", meine[0].Feed); err != nil || !strings.Contains(say, "von der Box genommen") || len(fake.Removed) != 1 {
		t.Fatalf("%q %v %v", say, err, fake.Removed)
	}
	if _, err := s.Beenden(ctx, "ben", meine[0].Feed); err == nil {
		t.Error("ended twice")
	}
}

// A new episode is told only to who subscribed to its podcast; the first
// look notes what is there and tells nothing; an episode is told once.
func TestNeueFolgenNurAnAbonnenten(t *testing.T) {
	s, fake, feeds := service(t)
	ctx := context.Background()
	s.Abonnieren(ctx, "anna", feeds+"/lage")
	s.Abonnieren(ctx, "ben", feeds+"/zeit")
	fake.Episode("Lage der Nation", "LdN 400", "<p>Heute: die Wahl. Und mehr.</p>")
	if got, err := s.Neue(ctx); err != nil || len(got) != 0 {
		t.Fatalf("first look: %v %v", got, err)
	}
	fake.Episode("Lage der Nation", "LdN 401", "<p>Diesmal geht es um Wärmepumpen und Fernwärme.</p>")
	fake.Episode("Was jetzt?", "Morgen", "")
	got, err := s.Neue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Nachricht{
		{Person: "ben", Text: "Neue Folge von „Was jetzt?“: Morgen"},
		{Person: "anna", Text: "Neue Folge von „Lage der Nation“: LdN 401\nDiesmal geht es um Wärmepumpen und Fernwärme."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%#v", got)
	}
	if got, _ := s.Neue(ctx); len(got) != 0 {
		t.Fatalf("told twice: %v", got)
	}
}

// owner 2026-10-26: "Die Folgen werden nicht geladen" — ABS's own check
// takes only episodes after the subscription, so Haus asks for the newest
// three at once; they are old, so nobody is told of them. A subscription
// from before is caught up once.
func TestErsteFolgen(t *testing.T) {
	s, fake, feeds := service(t)
	ctx := context.Background()
	if _, err := s.Abonnieren(ctx, "anna", feeds+"/lage"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fake.Loaded, ",") != "Folge 5,Folge 4,Folge 3" {
		t.Fatalf("loaded %v", fake.Loaded)
	}
	s.Neue(ctx) // the first look
	s.Abonnieren(ctx, "ben", feeds+"/lage")
	if len(fake.Loaded) != 3 {
		t.Errorf("loaded again for a second subscriber: %v", fake.Loaded)
	}
	if got, _ := s.Neue(ctx); len(got) != 0 {
		t.Errorf("told of the old ones: %v", got)
	}
	fake.Episode("Lage der Nation", "LdN 402", "")
	if got, _ := s.Neue(ctx); len(got) != 2 {
		t.Errorf("a new one, to both: %v", got)
	}

	// a subscription from before Haus fetched the first ones: once
	st, _ := s.load()
	st.Abos[0].Geladen, st.Abos[0].Seit = false, time.Time{}
	st.Abos = append(st.Abos, Abo{Feed: feeds + "/zeit", Item: "nix", Title: "Weg"})
	s.save(st)
	fake.Loaded = nil
	if err := s.Nachladen(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fake.Loaded) != 0 {
		t.Errorf("fetched for one that has episodes: %v", fake.Loaded)
	}
	st, _ = s.load()
	if !st.Abos[0].Geladen || st.Abos[0].Seit.IsZero() || st.Abos[1].Geladen {
		t.Errorf("%+v", st.Abos)
	}
}
