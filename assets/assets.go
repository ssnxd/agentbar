// Package assets embeds files that workflow materializes into its data
// directory at startup: the managed tmux server config and the skills
// injected into every spawned Claude session.
package assets

import "embed"

//go:embed tmux.conf skills
var FS embed.FS
