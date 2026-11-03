// Package app holds the names the Homes app (mdopp/homes-android) and Homes
// share: its deep-link scheme, its device-token prefix and where its widgets
// read the box. They are the app's contract (laib ADR 0022 §1), one set;
// the only old name beside them is LegacyTokenPrefix, with its end condition.
package app

const (
	// Scheme is the app's deep-link scheme: <scheme>://pair#token=…&id=… opens
	// the app after pairing, <scheme>://setup?server=…&token=… is what its QR
	// scanner reads.
	Scheme = "cloud.dopp.homes"
	// TokenPrefix starts every device token Homes mints; the app checks it
	// before it stores one.
	TokenPrefix = "homes_device_"
	// LegacyTokenPrefix is legacy: the prefix the old Haus app's tokens carry
	// (cloud.dopp.haus). A phone paired before the rename keeps working with
	// it; its stored hash is unchanged. Ends when every phone runs the Homes
	// app (laib#1021 step 9): drop this constant and its acceptance.
	LegacyTokenPrefix = "haus_device_"
	// Paket is the Homes app's package; GET /napi/app names it so the old
	// app (cloud.dopp.haus) can tell the person to install Homes.
	Paket = "cloud.dopp.homes"
	// Quelle is where the Homes app is released (Obtainium follows it).
	Quelle = "https://github.com/mdopp/homes-android"
	// BoxPrefix is where the app's widgets read the box: home, services,
	// approvals, upgrades.
	BoxPrefix = "/napi/box"
)
