package main

import (
	"context"
	"os"
	"testing"

	"github.com/civo/civo-csi/pkg/driver"
	"github.com/kubernetes-csi/csi-test/v4/pkg/sanity"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// TestCivoCSI runs the Sanity test suite
func TestCivoCSI(t *testing.T) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	d, _ := driver.NewTestDriver()

	ctx, cancel := context.WithCancel(context.Background())

	os.Setenv("NODE_ID", "12345")
	os.Setenv("REGION", "TESTING1")

	var eg errgroup.Group
	eg.Go(func() error {
		return d.Run(ctx)
	})

	config := sanity.NewTestConfig()
	if err := os.RemoveAll(config.TargetPath); err != nil {
		t.Fatalf("failed to delete target path %s: %s", config.TargetPath, err)
	}
	if err := os.RemoveAll(config.StagingPath); err != nil {
		t.Fatalf("failed to delete staging path %s: %s", config.StagingPath, err)
	}

	config.Address = d.SocketFilename

	sanity.Test(t, config)

	cancel()
	if err := eg.Wait(); err != nil {
		t.Errorf("driver run failed: %s", err)
	}
}
