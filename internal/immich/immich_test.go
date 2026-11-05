package immich

import (
	"fmt"
	"testing"

	"github.com/mdopp/homes/internal/immich/immichfake"
)

// People reads every page, and leaves out the unnamed and the hidden.
func TestPeopleEveryPageNamedOnly(t *testing.T) {
	fake := immichfake.New("key")
	defer fake.Close()
	for i := range 1203 {
		fake.Person(fmt.Sprint("p", i), fmt.Sprint("Person ", i), "", false, nil)
	}
	fake.Person("leer", " ", "", false, nil)
	fake.Person("weg", "Versteckt", "", true, nil)
	ps, err := (&Client{URL: fake.URL, Key: "key"}).People(t.Context())
	if err != nil || len(ps) != 1203 || ps[1202].Name != "Person 1202" {
		t.Fatalf("%d people, %v", len(ps), err)
	}
	if _, err := (&Client{URL: fake.URL, Key: "falsch"}).People(t.Context()); err == nil {
		t.Fatal("a refused key is no error")
	}
}

func TestGesicht(t *testing.T) {
	fake := immichfake.New("key")
	defer fake.Close()
	fake.Person("a", "Anna", "", false, []byte("jpeg"))
	c := &Client{URL: fake.URL, Key: "key"}
	if b, err := c.Gesicht(t.Context(), "a"); err != nil || string(b) != "jpeg" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := c.Gesicht(t.Context(), "b"); err == nil {
		t.Fatal("no face is no error")
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("HOMES_IMMICH_URL", "http://immich:2283/")
	t.Setenv("HOMES_IMMICH_KEY", "")
	if FromEnv() != nil {
		t.Fatal("without a key")
	}
	t.Setenv("HOMES_IMMICH_KEY", "k")
	if c := FromEnv(); c == nil || c.URL != "http://immich:2283" {
		t.Fatalf("%+v", c)
	}
}
