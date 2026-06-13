package main

import (
	"os"

	"github.com/kipkaev55/portato/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
