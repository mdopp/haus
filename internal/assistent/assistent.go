// Package assistent is the house's assistant as the family addresses it: one
// name, set once for the whole box in Laib (box.yml assistant:, which reaches
// Homes as HOMES_ASSISTANT), and the wake phrase every speaker listens for.
// The name appears where a sentence says how to address it („Sag Homes:
// Merk dir …“); the pages wear the house's own palette.
package assistent

import "strings"

// Default is the name when Laib sets none.
const Default = "Homes"

// Wake is the phrase every voice satellite listens for: openWakeWord's
// pretrained hey_mycroft, which a Voice PE offers out of the box.
const Wake = "Hey Mycroft"

var name = Default

// Name is the assistant's name.
func Name() string { return name }

// Set sets the name once at start; blank is Default.
func Set(s string) {
	if s = strings.TrimSpace(s); s == "" {
		s = Default
	}
	name = s
}
