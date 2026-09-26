// Package cachetest opens cache clients for tests against the test Valkey at
// PREBURN_TEST_REDIS_URL. Each client has its own key prefix, so parallel
// tests share one server without seeing each other's keys.
package cachetest
