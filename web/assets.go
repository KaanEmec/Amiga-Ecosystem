// Package assets embeds the browser assets: static files and page templates.
package assets

import "embed"

// FS holds static/** and templates/**. Directory embeds skip files that
// start with "_", so the template partials are listed explicitly.
//
//go:embed static templates templates/_*.html
var FS embed.FS
