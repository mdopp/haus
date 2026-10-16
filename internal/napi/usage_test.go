package napi

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/usage"
	"github.com/mdopp/haus/internal/usage/usagefake"
)

// #50: a widget's refresh is counted per /napi path it reads, only from a
// paired phone; a 401 and an unknown path count nothing, and neither the
// device token nor the entity in the path leaves Haus.
func TestWidgetRefreshesAreCounted(t *testing.T) {
	fake := usagefake.New("um")
	t.Cleanup(fake.Close)
	c := usage.New(fake.URL, "um")
	_, h := houseWith(t, func(cfg *Config) { cfg.Usage = c })
	for _, q := range []struct{ method, path, bearer, body string }{
		{"GET", "/napi/portal/state?entity_id=light.kueche", "dev1", ""},
		{"GET", "/napi/portal/state?entity_id=light.flur", "dev2", ""},
		{"GET", "/napi/portal/active", "dev1", ""},
		{"GET", "/napi/box/home", "dev1", ""},
		{"POST", "/napi/ha/call", "dev1", `{"domain":"light","service":"turn_off","entity_id":"light.kueche"}`},
		{"GET", "/napi/portal/state?entity_id=light.kueche", "fremd", ""}, // 401
		{"GET", "/napi/portal/state?entity_id=light.kueche", "", ""},      // 401
		{"GET", "/napi/gibtsnicht", "dev1", ""},                           // 404
	} {
		raw(t, h, q.method, q.path, q.bearer, q.body)
	}
	c.Flush(context.Background())
	got := map[string]int64{}
	for k, n := range fake.Counts() {
		if k.App != "haus" {
			t.Errorf("app %q", k.App)
		}
		got[k.Event] += n
	}
	want := map[string]int64{"napi.portal.state": 2, "napi.portal.active": 1, "napi.box.home": 1, "napi.ha.call": 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("counted %v, want %v", got, want)
	}
	for _, b := range fake.Bodies() {
		for _, leak := range []string{"dev1", "dev2", "kueche", "fremd"} {
			if strings.Contains(b, leak) {
				t.Errorf("%q left Haus: %s", leak, b)
			}
		}
	}
}
