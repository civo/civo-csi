package driver_test

import (
	"os"
	"testing"

	"github.com/civo/civo-csi/pkg/driver"
	"github.com/stretchr/testify/assert"
)

func TestFixHangingVolume(t *testing.T) {
	t.Run("Find out the instance ID", func(t *testing.T) {
		os.Setenv("NODE_ID", "instance-1")

		d, _ := driver.NewTestDriver()

		d.CivoClient.ListVolumes()

		err := d.FixHangingVolume()
		assert.Nil(t, err)
	})
}
