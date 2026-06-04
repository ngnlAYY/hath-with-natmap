package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ngnlAYY/hath-with-natter/internal/app"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, os.Args[1:]); err != nil {
		log.Fatalf("%v", err)
	}
}
