package driver_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/civo/civo-csi/pkg/driver"
	"github.com/civo/civogo"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/stretchr/testify/assert"
)

func TestProbe(t *testing.T) {
	fc, _ := civogo.NewFakeClient()
	d, _ := driver.NewTestDriver(fc)

	resp, err := d.Probe(context.Background(), &csi.ProbeRequest{})
	assert.Nil(t, err)

	assert.Equal(t, &wrappers.BoolValue{Value: true}, resp.Ready)
}

func TestProbeUnhealthy(t *testing.T) {
	fc, _ := civogo.NewFakeClient()
	fc.PingErr = fmt.Errorf("something went wrong")
	d, _ := driver.NewTestDriver(fc)

	_, err := d.Probe(context.Background(), &csi.ProbeRequest{})
	assert.NotNil(t, err)
}
