package web

import "embed"

// Dist 中继网页静态资源。
//
//go:embed all:dist
var Dist embed.FS
