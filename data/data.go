// Package data embeds the versioned tuning tables. It lives at the repository
// root so the Go server and the Vite client read the same files: the server
// embeds them here, the client imports them from web/src/tuning.ts.
package data

import "embed"

// Tuning holds the tuning/*.json tables. Parse them with
// spellfire/server/internal/tuning.
//
//go:embed tuning/*.json
var Tuning embed.FS
