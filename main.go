package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/civo/civo-csi/pkg/driver"
	"github.com/civo/civo-csi/pkg/driver/hook"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	versionInfo = flag.Bool("version", false, "Print the driver version")
)

var (
	apiURL    = strings.TrimSpace(os.Getenv("CIVO_API_URL"))
	apiKey    = strings.TrimSpace(os.Getenv("CIVO_API_KEY"))
	region    = strings.TrimSpace(os.Getenv("CIVO_REGION"))
	ns        = strings.TrimSpace(os.Getenv("CIVO_NAMESPACE"))
	clusterID = strings.TrimSpace(os.Getenv("CIVO_CLUSTER_ID"))
	node      = strings.TrimSpace(os.Getenv("KUBE_NODE_NAME"))
)

func run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		log.Info().Msg("Running until SIGINT/SIGTERM received")
		sig := <-c
		log.Info().Interface("signal", sig).Msg("Received signal")
		cancel()
	}()

	flag.Parse()
	if *versionInfo {
		log.Info().Str("version", driver.Version).Msg("CSI driver")
		return nil
	}

	if len(os.Args) > 1 {
		hook, err := hook.NewHook(
			hook.WithNodeName(node),
		)
		if err != nil {
			return err
		}
		switch cmd := os.Args[1]; cmd {
		case "pre-stop":
			log.Info().Msg("Running the pre-stop hook for driver")
			// return hook.PreStop(ctx)
			return hook.PreStop(context.Background()) // TODO: delete this code later.
		default:
			return fmt.Errorf("not supported command: %q", cmd)
		}
	}

	d, err := driver.NewDriver(apiURL, apiKey, region, ns, clusterID)
	if err != nil {
		return err
	}
	log.Info().Interface("d", d).Msg("Created a new driver")

	log.Info().Msg("Running the driver")
	return d.Run(ctx)
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	if err := run(context.Background()); err != nil {
		log.Fatal().Err(err).Msg("Application error occured")
	}
}
