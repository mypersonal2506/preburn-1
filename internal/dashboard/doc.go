// Package dashboard serves the read APIs of the dashboard under
// /api/v1/dashboard. Every route takes a member session only and acts in the
// environment of the session, except the decision stream, which takes the
// environment from its environment query parameter because the browser's
// EventSource cannot send headers.
//
// The overview sums revenue, AI cost, cost avoided and decisions for one
// period option. current and previous read each customer's period rollup:
// the latest-starting rollup that contains now, and the latest-starting
// rollup that contains the instant before the start of that current period
// (the start of the UTC calendar month for a customer without one).
// last_30_days reads the revenue entries, ledger entries and decisions of
// today and the 29 UTC days before it. The daily series, the customers to
// watch, the attention counts and the recent policy changes complete it.
//
// The customer list and the overview's pace attention compute the current
// signals of every active customer, so both return ErrEnvironmentTooLarge in
// an environment with more than ActiveCustomerMaximum active customers.
//
// The decision list filters by outcome, customer, feature and policy, newest
// first. The decision detail adds the request, the signals the check stored,
// the lifecycle times and the ledger entries of the decision's usage. The
// event list merges decisions, at the time of their check, with ledger
// entries, at the time their request ran.
//
// StreamService relays the decision stream of an environment as server-sent
// events. Its blocking reads use the separate stream client, so open streams
// never hold connections the check path needs. A stream resumes after the
// Last-Event-ID header or the last_event_id query parameter, sends a comment
// heartbeat every StreamHeartbeatInterval, and ends when the client
// disconnects, when Stop runs, or at the first heartbeat after its member
// session ends. Heartbeats check the session without extending it.
//
// The onboarding route returns the state of the get started checklist, and
// the feature route lists the features that usage estimates, active
// policies, active plan hold times and the decisions of the last 30 days
// name, for the feature pickers.
package dashboard
