package pages

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
)

// medienPath is Medien's own path, where its buttons land back.
const medienPath = "/medien"

// What Medien says when there is nothing to show.
const (
	saysNoPlayers    = "Lautsprecher und Fernseher antworten gerade nicht."
	saysNoPlayersYet = "Im Haus ist noch kein Lautsprecher oder Fernseher eingerichtet."
	saysNothingPlays = "Gerade läuft nichts."
)

// HA's media player features (MediaPlayerEntityFeature) a button needs.
const (
	playFeature       = 16384
	volumeSetFeature  = 4
	volumeStepFeature = 1024
)

// Medien answers "what plays where?" (haus#12): Läuft gerade, each
// playing or paused player with its room and what it plays, and the
// buttons it can take — Pause or Weiter, Leiser and Lauter — through
// /schalten, back to Medien; Gerade still, the other players and their
// state. A group is not a player, what HA hides is not shown. No players
// is one plain line; HA not answering is one plain line. A path under
// /medien/ is a 404 with the way back.
func Medien(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Medien") {
			return
		}
		if cfg.HA == nil {
			notSetHA.write(w, r, cfg, "Medien")
			return
		}
		if r.URL.Path != medienPath {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Medien", medienPath, "Medien"}.write(w, r, cfg)
			return
		}
		var (
			states []ha.State
			haErr  error
			reg    ha.Registry
		)
		fetch(r,
			func(ctx context.Context) { states, haErr = cfg.HA.States(ctx) },
			// what HA hides stays hidden; until the registry has been read, nothing is, and no room is named
			func(ctx context.Context) { reg, _ = cfg.HA.CachedRegistry(ctx) })
		logErrs("medien", map[string]error{"home assistant": haErr})
		b := familyHead(r, cfg, "Medien", "Medien")
		b.WriteString(liveRefresh("seite", 120)) // what plays changes by itself: by push
		b.WriteString("<h1>Medien</h1>")
		if haErr != nil {
			b.WriteString("<p class=k>" + saysNoPlayers + "</p>")
			writePage(w, http.StatusOK, b)
			return
		}
		var on, quiet []ha.State
		twins := twinPlayers(states, reg, true)
		for _, s := range states {
			if !strings.HasPrefix(s.EntityID, "media_player.") || !shown(s, reg.Hidden) || twins[s.EntityID] {
				continue
			}
			if s.State == "playing" || s.State == "paused" {
				on = append(on, s)
			} else {
				quiet = append(quiet, s)
			}
		}
		if len(on)+len(quiet) == 0 {
			b.WriteString("<p class=k>" + saysNoPlayersYet + "</p>")
			writePage(w, http.StatusOK, b)
			return
		}
		byName := func(xs []ha.State) {
			sort.SliceStable(xs, func(i, j int) bool {
				if (xs[i].State == "playing") != (xs[j].State == "playing") {
					return xs[i].State == "playing"
				}
				return strings.ToLower(friendly(xs[i])) < strings.ToLower(friendly(xs[j]))
			})
		}
		byName(on)
		byName(quiet)
		laeuft := cards.Card{Title: "Läuft gerade", Look: cards.Boxes}
		for _, s := range on {
			row := cards.Row{Text: friendly(s), Detail: playingWords(s, reg.Rooms[s.EntityID]), Href: cards.ThingPage(s.EntityID)}
			row.Action, row.Also = playerButtons(s, medienPath)
			laeuft.Rows = append(laeuft.Rows, row)
		}
		if len(on) == 0 {
			laeuft.Add(saysNothingPlays, "")
		}
		b.WriteString(laeuft.HTML())
		still := cards.Card{Title: "Gerade still"}
		for i, s := range quiet {
			if i == maxRoomRows {
				break
			}
			still.AddLink(friendly(s), roomDetail("media_player", s, ""), cards.ThingPage(s.EntityID))
		}
		more(&still, len(quiet)-maxRoomRows)
		b.WriteString(still.HTML())
		writePage(w, http.StatusOK, b)
	}
}

// twinPlayers is the media players to leave out, so a thing is one tile
// (haus#83): of twins (one Chromecast through Cast and through Music
// Assistant, ha.Registry.Twins) all but the one to steer — the one that
// plays, then the paused one, then the device's own integration over
// Music Assistant's mirror, which can only steer what Music Assistant
// plays itself. A Jellyfin session is someone's app, not a thing in the
// house: left out, unless clients and it plays or is paused.
func twinPlayers(states []ha.State, reg ha.Registry, clients bool) map[string]bool {
	rank := func(s ha.State) int {
		r := 0
		switch s.State {
		case "playing":
			r = 4
		case "paused":
			r = 2
		}
		if reg.Platforms[s.EntityID] != "music_assistant" {
			r++
		}
		return r
	}
	out := map[string]bool{}
	best := map[string]ha.State{}
	for _, s := range states {
		if !strings.HasPrefix(s.EntityID, "media_player.") {
			continue
		}
		if reg.Platforms[s.EntityID] == "jellyfin" && !(clients && (s.State == "playing" || s.State == "paused")) {
			out[s.EntityID] = true
			continue
		}
		k := reg.Twins[s.EntityID]
		if k == "" {
			continue
		}
		b, ok := best[k]
		switch {
		case !ok:
			best[k] = s
		case rank(s) > rank(b):
			out[b.EntityID] = true
			best[k] = s
		default:
			out[s.EntityID] = true
		}
	}
	return out
}

// playingWords is what a player plays, and where: its room (unless the
// player is named for it), "pausiert", the title or else the app, and its
// volume.
func playingWords(s ha.State, room string) string {
	var parts []string
	if room = trimRunes(oneLine(room), 60); room != "" && !strings.EqualFold(room, friendly(s)) {
		parts = append(parts, room)
	}
	if s.State == "paused" {
		parts = append(parts, "pausiert")
	}
	title, _ := s.Attributes["media_title"].(string)
	artist, _ := s.Attributes["media_artist"].(string)
	app, _ := s.Attributes["app_name"].(string)
	switch {
	case oneLine(title) != "":
		parts = append(parts, trimRunes(oneLine(title), 80))
		if a := trimRunes(oneLine(artist), 60); a != "" {
			parts = append(parts, a)
		}
	case oneLine(app) != "":
		parts = append(parts, trimRunes(oneLine(app), 40))
	}
	if v, ok := number(s.Attributes["volume_level"]); ok && v >= 0 && v <= 1 {
		parts = append(parts, "Lautstärke\u00a0"+strconv.Itoa(int(math.Round(v*100)))+"\u00a0%") // kept on one line
	}
	return strings.Join(parts, " · ")
}

// playerButtons are what a playing or paused player can take, by its
// features: Pause or Weiter, then Leiser and Lauter; each lands on back.
// None for a player that is neither playing nor paused. Also is never nil
// when there is a button, so even a lone Pause sits on its own line, like
// the others' buttons.
func playerButtons(s ha.State, back string) (*cards.Action, []*cards.Action) {
	f, _ := number(s.Attributes["supported_features"])
	has := func(bit int64) bool { return int64(f)&bit != 0 }
	button := func(label, service string) *cards.Action {
		return &cards.Action{Label: label, Entity: s.EntityID, Do: "media_player." + service, Back: back}
	}
	var all []*cards.Action
	switch {
	case s.State == "playing" && has(pauseFeature):
		all = append(all, button("Pause", "media_pause"))
	case s.State == "paused" && has(playFeature):
		all = append(all, button("Weiter", "media_play"))
	}
	if (s.State == "playing" || s.State == "paused") && (has(volumeSetFeature) || has(volumeStepFeature)) {
		all = append(all, button("Leiser", "volume_down"), button("Lauter", "volume_up"))
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all[0], append([]*cards.Action{}, all[1:]...)
}
