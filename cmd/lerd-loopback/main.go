package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/geodro/lerd/internal/loopback"
)

func main() {
	var mapPath string
	var interval time.Duration

	flag.StringVar(&mapPath, "map", os.Getenv("LERD_LOOPBACK_MAP"), "path to loopback port map JSON file")
	flag.DurationVar(&interval, "interval", loopback.DefaultPollInterval, "poll interval for port map reloads")
	flag.Parse()

	if mapPath == "" {
		fmt.Fprintln(os.Stderr, "lerd-loopback: -map or LERD_LOOPBACK_MAP is required")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fwd := loopback.NewForwarder(mapPath, loopback.WithPollInterval(interval))
	if err := fwd.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "lerd-loopback: %v\n", err)
		os.Exit(1)
	}
}
