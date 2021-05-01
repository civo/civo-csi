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

	d, err := driver.NewDriver(os.Getenv("CIVO_API_URL"), os.Getenv("CIVO_API_KEY"), os.Getenv("CIVO_REGION"), os.Getenv("CIVO_NAMESPACE"), os.Getenv("CIVO_CLUSTER_ID"))
	if err != nil {
		log.Fatal().Err(err)
	}

	log.Info().Interface("d", d).Msg("Created a new driver")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		log.Info().Msg("Running until SIGINT/SIGTERM received")
		sig := <-c
		log.Info().Interface("signal", sig).Msg("Received signal")
		cancel()
	}()

	log.Info().Msg("Running the driver")

	if err := d.Run(ctx); err != nil {
		log.Fatal().Err(err)
	}
}
