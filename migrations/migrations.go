// Package migrations embeds the SQL migrations, so every binary built from
// this commit carries exactly the schema it expects.
//
// Files are named NNNN_description.sql and contain "-- +goose Up" and
// "-- +goose Down" sections. Rules for writing them are in README.md
// (Migrations): every migration must keep working with the previous release
// of the application (expand/contract).
package migrations

import "embed"

// FS holds the migration files at its root.
//
//go:embed *.sql
var FS embed.FS
