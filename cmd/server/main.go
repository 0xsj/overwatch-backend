// Command server is a shim. It calls the composition root and does nothing else.
//
// decisions/0016: the root is an ordinary package so that what it composes can
// be imported, tested and measured. Everything this file could grow — a flag, a
// branch, a default — belongs in overwatch-backend/root, where something can
// reach it.
package main

import (
	"os"

	"github.com/0xsj/overwatch-backend/root"
)

func main() {
	if err := root.Run(); err != nil {
		root.Report(err)
		os.Exit(1)
	}
}
