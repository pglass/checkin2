// Package db holds the SQL schema and generated query code.
package db

import _ "embed"

// Schema is the CREATE TABLE / INDEX DDL, applied at database open.
//
//go:embed schema.sql
var Schema string
