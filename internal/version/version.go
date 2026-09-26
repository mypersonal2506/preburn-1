package version

// These must stay string variables initialized to constant strings, because
// -ldflags -X silently ignores constants and computed values.
var (
	// Version is the release version, such as v0.1.0, or dev in a development build.
	Version = "dev"
	// Commit is the source commit the binary was built from, or unknown.
	Commit = "unknown"
	// BuildDate is the build time in RFC 3339 format, or unknown.
	BuildDate = "unknown"
)
