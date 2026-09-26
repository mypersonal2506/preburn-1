// Package metrics exposes Prometheus metrics. NewRegistry builds the registry
// every component registers on, HTTP counts and times API requests, and Serve
// answers /metrics on the metrics address. Domain packages register their own
// preburn_ metrics on the registry passed to their constructors.
//
// HTTP.Middleware labels requests by Request.Pattern, which net/http.ServeMux
// writes on the *http.Request value it receives. The middleware must pass the
// mux the same request value it received. A middleware between the two that
// calls Request.WithContext hides the pattern, and every request is labelled
// unmatched.
package metrics
