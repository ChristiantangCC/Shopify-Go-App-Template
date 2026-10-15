// Package web embeds the built admin frontend. The committed
// frontend/dist keeps plain `go build` working without Node; regenerate it
// with `pnpm build` in web/frontend after changing the frontend.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:frontend/dist
var distFS embed.FS

// Dist returns the built frontend as a filesystem rooted at dist/.
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "frontend/dist")
}
