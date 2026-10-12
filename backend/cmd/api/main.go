// Command api runs pitbox-api. All the wiring lives in internal/bootstrap.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/bootstrap"
)

// Go note: os.Exit ends the process immediately and does NOT run deferred
// functions. So main only calls os.Exit after run has returned, and every defer
// lives inside run.
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "pitbox-api: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// After the first signal, restore the default behaviour so a second Ctrl+C
	// kills the process even if the graceful shutdown is stuck.
	go func() {
		<-ctx.Done()
		stop()
	}()

	return bootstrap.RunAPI(ctx, os.Stdout)
}
