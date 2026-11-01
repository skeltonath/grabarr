// Package webui embeds the built SvelteKit frontend (web/build) into the
// binary. `make web` builds it; the Dockerfile builds it in a Node stage.
package webui

import "embed"

//go:embed all:build
var Build embed.FS
