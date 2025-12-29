package main

import (
	"context"
	"fmt"
	"os"

	"github.com/theoffensivecoder/encoredev-migrator/cmd/migrate"
)

func main() {
	if err := migrate.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
