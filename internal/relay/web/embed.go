package web

import "embed"

// Dist is the relay web static tree.
//
//go:embed all:dist
var Dist embed.FS
