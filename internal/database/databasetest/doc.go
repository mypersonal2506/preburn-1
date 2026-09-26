// Package databasetest gives integration tests their own Postgres database on
// the server named by PREBURN_TEST_DATABASE_URL. NewPool copies a template
// database that holds every migration, and NewEmptyPool starts from an empty
// database. Each database is dropped when its test ends.
package databasetest
