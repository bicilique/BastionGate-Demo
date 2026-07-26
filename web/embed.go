package webassets

import "embed"

// Files contains the complete browser application served by the Go facade.
//
//go:embed index.html styles.css app.js app-state.js favicon.svg
var Files embed.FS
