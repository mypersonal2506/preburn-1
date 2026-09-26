// Package policies holds policy documents, their validation, the resolution
// that picks the policy deciding a check, and the service, cache and admin
// API that store and serve them.
//
// A policy applies to everyone, to the customers of one plan or to one
// customer, for one feature or for every feature. Its condition group
// compares the signals of package signals with fixed values, and its action
// allows, routes, caps or denies the request. DecodeDocument reads the API
// JSON form of a document and reports every malformed field by its JSON path,
// such as when.all[1].value. Validate checks a well-formed policy: scope ids,
// the feature name, condition nesting, and route and cap overrides against the
// parameter mappings of the catalog.
//
// Resolve considers the active policies whose feature and scope match the
// request and whose conditions hold. The customer level beats the plan level,
// which beats everyone. Within a level deny beats cap, cap beats route and
// route beats allow, then the latest update and the lowest id win. A cap
// keeps only the overrides the requested model supports, and a cap left with
// no override and no limit allows the request. Resolve and the document types
// are pure, so the server, the simulator and the demo generator share them.
//
// Service stores policies per environment. Create and Update decode and
// validate a document against the plans and customers of the request
// environment and the parameter mappings, store route chain aliases as the
// models they name, and answer 422 policy_invalid with one error per JSON
// path. A plan-level policy needs an active plan. Every update increments the
// version. Preview counts the active customers a draft would match now from
// their current period, without storing it. Cache keeps the active policies
// of each environment for 60 seconds, and every change clears it through the
// policies invalidation. List narrows the policies by status and plan.
// RegisterRoutes serves the policies, the preview and the parameter mappings
// on the admin route group.
package policies
