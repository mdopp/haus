// Package legacy holds the names Haus still speaks because the installed
// Android app depends on them (laib ADR 0021 rule 8). They are the app's
// contract, not Haus's names; each ends with laib#130, when the app ships new
// names next to these and Haus can switch.
package legacy

const (
	// AppScheme is the app's deep-link scheme: <scheme>://pair#token=… opens
	// the app, <scheme>://setup?server=…&token=… is what its QR scanner reads.
	AppScheme = "cloud.dopp.solaris"
	// TokenPrefix is what the app checks before it stores a device token.
	TokenPrefix = "sol_device_"
	// NapiPrefix is where the app's widgets read the box: home, services,
	// approvals, upgrades.
	NapiPrefix = "/napi/servicebay"
)
