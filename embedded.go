package embedded

import "embed"

//go:embed all:stdlib
var StdlibFS embed.FS
