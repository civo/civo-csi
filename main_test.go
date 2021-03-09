package main

import (
	"context"
	"os"
	"testing"

	"github.com/civo/civo-csi/internal/driver"
	"github.com/kubernetes-csi/csi-test/v4/pkg/sanity"
	"golang.org/x/sync/errgroup"
)

// TestCivoCSI runs the Sanity test suite
func TestCivoCSI(t *testing.T) {
	drv, _ := driver.NewDriver("https://civo-api.example.com", "NO_API_KEY_NEEDED", "TEST1")
	drv.SocketFilename = "unix:///tmp/civo-csi.sock"
	if err := os.Remove(drv.SocketFilename); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove unix domain socket file %s, error: %s", drv.SocketFilename, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	os.Setenv("NODE_ID", "12345")
	os.Setenv("REGION", "TESTING1")

	var eg errgroup.Group
	eg.Go(func() error {
		return drv.Run(ctx)
	})

	config := sanity.NewTestConfig()
	if err := os.RemoveAll(config.TargetPath); err != nil {
		t.Fatalf("failed to delete target path %s: %s", config.TargetPath, err)
	}
	if err := os.RemoveAll(config.StagingPath); err != nil {
		t.Fatalf("failed to delete staging path %s: %s", config.StagingPath, err)
	}

	config.Address = drv.SocketFilename

	sanity.Test(t, config)

	cancel()
	if err := eg.Wait(); err != nil {
		t.Errorf("driver run failed: %s", err)
	}
}
