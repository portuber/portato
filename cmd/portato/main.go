package main

import (
	"os"

	"github.com/portuber/portato/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
