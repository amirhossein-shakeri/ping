package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	cfg, err := ParseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(2)
	}

	app, err := NewApp(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "startup error: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := signalContext(context.Background())
	defer cancel()

	if err := app.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "runtime error: %v\n", err)
		os.Exit(1)
	}
}
