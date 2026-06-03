package main

import (
	"os"

	"github.com/webkaz-labs/updev/internal/cmd"
)

func main() {
	os.Exit(cmd.Run(os.Args[1:]))
}
