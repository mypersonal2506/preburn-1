// Package config loads the PREBURN_ environment variables into a typed Config.
//
// Load reads every variable through a lookup function, applies the defaults,
// validates the values and reports every problem at once, one per line. An
// empty variable counts as unset, so it takes its default or, when it is
// required, is reported as missing.
package config
