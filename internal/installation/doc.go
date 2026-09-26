// Package installation holds the one installation row, its first-run setup
// and the installation settings.
//
// A new installation has no members. While setup is pending, the api
// process prepares a setup link, {PREBURN_PUBLIC_URL}/setup#<token>, and
// stores only the SHA-256 hash of the token. The token sits in the URL
// fragment, so it never reaches server or proxy logs. Preparing a link
// again replaces the token. POST /api/v1/setup with the token creates the
// first member, completes setup and signs the member in. After that, setup
// is no longer available.
//
// SettingsService reads and changes the settings that members edit on the
// dashboard routes GET and PATCH /api/v1/settings. The installation name
// belongs to the installation, and each environment has its own default
// plan and Stripe customer metadata key. The default plan applies to the
// customers of the environment that have no plan, and it must be an active
// plan of that environment. Every change publishes the settings
// invalidation.
package installation
