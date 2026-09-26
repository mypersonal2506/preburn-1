// Package apikeys holds the API keys that services send to call Preburn, and
// the authenticator of their bearer tokens.
//
// A key acts in one environment with the runtime or admin scope. Its secret
// has the form pb_<environment>_<scope>_<32 base62 characters> and appears
// once, in the response that creates the key. The database keeps only its
// SHA-256 hash and its last four characters. Runtime keys call the runtime
// route group, admin keys also call the admin group, and no key calls the
// dashboard group.
//
// A request is for the authenticator when its Authorization header has the
// Bearer scheme, as HasBearerAuthorization reports. The authenticator keeps
// the keys it resolved in process memory for 30 seconds, and a lookup that
// overlaps a clear of that cache does not cache its result. Revoking a key
// clears that cache in the revoking process and publishes the api_key
// invalidation, which clears it in every other process that subscribes
// Authenticator.Invalidate.
package apikeys
