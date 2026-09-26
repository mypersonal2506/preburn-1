// Package web serves the dashboard. Builds with the embedweb build tag embed
// the built dashboard from web/dist and serve it. Builds without the tag, such
// as plain go build and go test, serve a page saying the dashboard is not
// built, so they never need web/dist.
package web
