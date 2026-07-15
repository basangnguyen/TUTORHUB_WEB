package migrations

import "embed"

// Files contains immutable SQL migrations shipped with the Core API release.
//
//go:embed *.sql
var Files embed.FS
