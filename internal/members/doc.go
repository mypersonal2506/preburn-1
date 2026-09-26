// Package members holds the members of an installation, their passwords and
// browser sessions, login with its rate limits, the session authenticator of
// the admin and dashboard route groups, member management with one-time
// invite and password reset links, and the member_cleanup job.
//
// A member signs in with email and password. Login starts a session whose
// token lives only in the preburn_session cookie, and the database stores
// its SHA-256 hash. A session expires 30 days after its last use. Requests
// with unsafe methods also send the X-CSRF-Token header equal to the
// preburn_csrf cookie, and every session request names its environment in
// the X-Preburn-Environment header. A login, setup and a used link each
// record the time as the member's last login.
//
// Members add other members without a password. Adding a member returns an
// invite link that sets the password, and a password reset link replaces
// it. Both have the form {PREBURN_PUBLIC_URL}/link#<token>, work once and
// expire after 24 hours, and the database stores only the SHA-256 hash of
// their tokens. A new reset link ends every earlier unused link of the
// member, invites included, and using a link ends the member's other unused
// links. Removing a member disables them and ends their sessions. Nobody
// removes themselves or the last active member with a password, and a
// removed member removes nobody.
//
// The member_cleanup job runs every day at 04:00 UTC. It deletes expired
// sessions and the links that expired or were consumed more than 7 days
// ago.
package members
