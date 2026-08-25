package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/neurekadev/warrden/internal/app"
)

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return app.Run(ctx, os.Args, os.Stdout)
}
