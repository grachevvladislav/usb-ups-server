package app

import (
	_ "embed"
	"strings"
)

//go:embed version.txt
var versionText string

var Version = strings.TrimSpace(versionText)
