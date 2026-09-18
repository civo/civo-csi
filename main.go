package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/civo/civo-csi/pkg/driver"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var versionInfo = flag.Bool("version", false, "Print the driver version")

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	flag.Parse()
	if *versionInfo {
		log.Info().Str("version", driver.Version).Msg("CSI driver")
		return
	}

	apiURL := strings.TrimSpace(os.Getenv("CIVO_API_URL"))
	apiKey := strings.TrimSpace(os.Getenv("CIVO_API_KEY"))
	region := strings.TrimSpace(os.Getenv("CIVO_REGION"))
	ns := strings.TrimSpace(os.Getenv("CIVO_NAMESPACE"))
	clusterID := strings.TrimSpace(os.Getenv("CIVO_CLUSTER_ID"))

	d, err := driver.NewDriver(apiURL, apiKey, region, ns, clusterID)
	if err != nil {
		log.Fatal().Err(err)
	}

	log.Info().Interface("d", d).Msg("Created a new driver")

	if d.ClusterID == "" {
		// STANDALONE MODE (civo-standalone-csi). There is no Civo Kubernetes cluster: the nodes are
		// plain Civo compute instances joined to a self-managed or third-party-managed Kubernetes
		// cluster. Volumes are created without a cluster id and attached by instance id; the volume
		// type comes from CIVO_VOLUME_TYPE (default "standard", the only type the compute API offers
		// today).
		d.ClusterVolumeType = strings.TrimSpace(os.Getenv("CIVO_VOLUME_TYPE"))
		if d.ClusterVolumeType == "" {
			d.ClusterVolumeType = "standard"
		}
		// The volumes API requires network_id; a Civo Kubernetes cluster implies it server-side, a
		// standalone driver must say. CIVO_NETWORK_ID, else the region's default network.
		d.NetworkID = strings.TrimSpace(os.Getenv("CIVO_NETWORK_ID"))
		if d.NetworkID == "" {
			net, err := d.CivoClient.GetDefaultNetwork()
			if err != nil {
				log.Fatal().Err(err).Msg("Standalone mode: CIVO_NETWORK_ID unset and the region's default network could not be read")
			}
			d.NetworkID = net.ID
		}
		log.Info().Str("volume_type", d.ClusterVolumeType).Str("network_id", d.NetworkID).Msg("Standalone mode: no CIVO_CLUSTER_ID, volumes attach to plain instances")
	} else {
		log.Debug().Msg("Determining volumeType of cluster")
		cluster, err := d.CivoClient.GetKubernetesCluster(d.ClusterID)
		if err != nil {
			log.Fatal().Err(err).Msg("Unable to read the Civo Kubernetes cluster (set CIVO_CLUSTER_ID empty for standalone mode)")
		}
		d.ClusterVolumeType = cluster.VolumeType
	}

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
