// Package web embeds the HTML templates and static assets so the compiled
// binary is self-contained.
package web

import "embed"

// Templates holds the html/template files.
//
//go:embed templates/*.html
var Templates embed.FS

// Static holds CSS, JavaScript and images served under /static/.
//
//go:embed static
var Static embed.FS
