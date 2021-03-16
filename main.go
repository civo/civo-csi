package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/civo/civo-csi/pkg/driver"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Debug().Msg("Hello world AJ")

	d, err := driver.NewDriver(os.Getenv("CIVO_API_URL"), os.Getenv("CIVO_API_KEY"), os.Getenv("CIVO_REGION"))
	if err != nil {
		log.Fatal().Err(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()

	if err := d.Run(ctx); err != nil {
		log.Fatal().Err(err)
	}
}
