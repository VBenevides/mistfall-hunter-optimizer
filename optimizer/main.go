package main

import (
	_ "embed"
	"os"

	"mistfall/v2/core"
)

//go:embed db_mistfalldb.sqlite
var embeddedDatabase []byte

//go:embed affixes.json
var embeddedAffixes []byte

func init() {
	core.ConfigureAssets(embeddedDatabase, embeddedAffixes)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--cli" {
		core.RunCLI(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		core.RunCLI(os.Args[1:])
		return
	}
	runGUI()
}
