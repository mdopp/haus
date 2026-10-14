// Package app holds the names the Haus app (mdopp/haus-android) and Haus
// share: its deep-link scheme, its device-token prefix and where its widgets
// read the box. They are the new app's contract (laib ADR
// 0022 §1), one set, nothing old next to them (§4).
package app

const (
	// Scheme is the app's deep-link scheme: <scheme>://pair#token=…&id=… opens
	// the app after pairing, <scheme>://setup?server=…&token=… is what its QR
	// scanner reads.
	Scheme = "cloud.dopp.haus"
	// TokenPrefix starts every device token Haus mints; the app checks it
	// before it stores one.
	TokenPrefix = "haus_device_"
	// BoxPrefix is where the app's widgets read the box: home, services,
	// approvals, upgrades.
	BoxPrefix = "/napi/box"
)
