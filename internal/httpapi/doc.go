// Package httpapi is the HTTP API framework: a Huma v2 API under /api/v1 on a
// net/http ServeMux, problem responses, route groups and authentication, list
// cursors and limits, the health routes, and the middleware around the
// ServeMux.
//
// Every route registers through Register with its RouteGroup, or through
// RegisterEventStream when it streams server-sent events to a browser, which
// takes the environment from the query instead of a header. Handlers return
// errors and never write error responses. A CodedError becomes its own
// problem, and any other error a 500 internal_error problem that is logged as
// http.internal_error. Once the client closed the request, any error becomes
// a 499 client_closed_request problem that is logged at info level as
// http.request_canceled. Validation failures become validation_failed problems
// that name each invalid field by location and never echo its value. A
// location stops at a map field, so it never repeats a map key. Routes
// outside RouteGroupPublic reject text Postgres cannot store, such as a NUL
// character, before their handler runs.
//
// The server wraps its ServeMux in this order, outermost first:
//
//	RequestID
//	SecurityHeaders
//	the tracing middleware
//	the metrics middleware
//	AccessLog
//	Recover
//	the ServeMux
//
// The ServeMux writes the matched route pattern to Request.Pattern of the
// request value it receives. The tracing middleware, the metrics middleware
// and AccessLog read it from the request value they pass on, after the
// ServeMux returns, so no middleware between them and the ServeMux may
// replace the request, as Request.WithContext does. RequestID replaces it, so
// it stays outside. Recover stays inside AccessLog and the metrics middleware,
// so a recovered panic is logged and counted with status 500.
package httpapi
