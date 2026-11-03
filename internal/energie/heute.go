package energie

import (
	"math"
	"strconv"
	"strings"

	"github.com/mdopp/homes/internal/ha"
)

// Added is what a counter added over its history points and its state
// now, in kWh: every rise counts; a fall below newMeter of the count is
// the counter starting over and counts from zero, a smaller dip is noise
// (HA's own rule for a total_increasing sensor). Without a reading among
// the points it is not known (false). The Energie page's Heute and the
// app's energy widget read today from here.
func Added(points []ha.State, now ha.State) (float64, bool) {
	var sum float64
	prev, seen, read := 0.0, false, false
	for i, p := range append(points[:len(points):len(points)], now) {
		v, err := strconv.ParseFloat(strings.TrimSpace(p.State), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		switch {
		case !seen:
			prev = v
		case v >= prev:
			sum += v - prev
			prev = v
		case v < newMeter*prev: // started over: counts from zero
			sum += v
			prev = v
		} // a smaller dip is noise: the count stands where it stood
		seen, read = true, read || i < len(points)
	}
	return sum * KWhScale(now), read
}

// newMeter: a counter falling below this share of its count started over
// (a new meter, a reset); HA's recorder draws the line at 10 %.
const newMeter = 0.9

// KWhScale turns a counter's unit into kWh.
func KWhScale(s ha.State) float64 {
	u, _ := s.Attributes["unit_of_measurement"].(string)
	switch strings.TrimSpace(u) {
	case "Wh":
		return 1e-3
	case "MWh":
		return 1e3
	}
	return 1
}
