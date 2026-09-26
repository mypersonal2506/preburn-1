// Package database opens the Postgres connection pool, runs work in
// transactions, takes named advisory locks and applies the schema migrations:
// the goose migrations embedded in package db, then River's migrations.
// Every pooled connection uses the UTC time zone.
package database
