package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/GuanyeSpace/clawquant-agent/internal/app"
	"github.com/GuanyeSpace/clawquant-agent/internal/buildinfo"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, buildinfo.Current()); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}

		log.Fatal(err)
	}
}
