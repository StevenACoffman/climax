package scaffold

import _ "embed"

//go:embed templates/main.go.tmpl
var mainTemplate string

//go:embed templates/cmd.go.tmpl
var cmdTemplate string

//go:embed templates/root.go.tmpl
var rootTemplate string

//go:embed templates/version.go.tmpl
var versionTemplate string

//go:embed templates/newcmd.go.tmpl
var newCmdTemplate string

//go:embed templates/man.go.tmpl
var manCmdTemplate string
