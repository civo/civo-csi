package driver_test

import (
	"context"
	"os"
	"testing"

	"github.com/civo/civo-csi/pkg/driver"
	"github.com/civo/civogo"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNodeStageVolume(t *testing.T) {
	t.Run("Format and mount the volume to a global mount path", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		_, err := d.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
			VolumeId:          "volume-1",
			StagingTargetPath: "/mnt/my-target",
			VolumeCapability: &csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Mount{},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		})
		assert.Nil(t, err)

		formatted, _ := d.DiskHotPlugger.IsFormatted("")
		assert.True(t, formatted)

		mounted, _ := d.DiskHotPlugger.IsMounted("/mnt/my-target")
		assert.True(t, mounted)
	})

	t.Run("Does not format the volume if already formatted", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		hotPlugger := &driver.FakeDiskHotPlugger{
			Formatted: true,
		}
		d.DiskHotPlugger = hotPlugger

		_, err := d.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
			VolumeId:          "volume-1",
			StagingTargetPath: "/mnt/my-target",
			VolumeCapability: &csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Mount{},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		})
		assert.Nil(t, err)

		formatCalled := hotPlugger.FormatCalled
		assert.False(t, formatCalled)
	})

	t.Run("Returns Not Found gRPC error if the disk isn't plugged in", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		hotPlugger := &driver.FakeDiskHotPlugger{
			DiskAttachmentMissing: true,
		}
		d.DiskHotPlugger = hotPlugger

		_, err := d.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
			VolumeId:          "volume-1",
			StagingTargetPath: "/mnt/my-target",
			VolumeCapability: &csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Mount{},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		})

		assert.Equal(t, status.Code(err), codes.NotFound)
	})
}

func TestNodeUnstageVolume(t *testing.T) {
	t.Run("Unmount the volume", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		hotPlugger := &driver.FakeDiskHotPlugger{
			Formatted: true,
			Mounted:   true,
		}
		d.DiskHotPlugger = hotPlugger

		_, err := d.NodeUnstageVolume(context.Background(), &csi.NodeUnstageVolumeRequest{
			VolumeId:          "volume-1",
			StagingTargetPath: "/mnt/my-target",
		})
		assert.Nil(t, err)

		mounted, _ := d.DiskHotPlugger.IsMounted("/mnt/my-target")
		assert.False(t, mounted)
	})
}

func TestNodePublishVolume(t *testing.T) {
	t.Run("Bind-mount the volume from the general mount point in to the container", func(t *testing.T) {
		d, _ := driver.NewTestDriver(nil)

		hotPlugger := &driver.FakeDiskHotPlugger{}
		d.DiskHotPlugger = hotPlugger

		_, err := d.NodePublishVolume(context.Background(), &csi.NodePublishVolumeRequest{
			VolumeId:          "volume-1",
			StagingTargetPath: "/mnt/my-target",
			TargetPath:        "/var/lib/kubelet/some-path",
			VolumeCapability: &csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Mount{},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		})
		assert.Nil(t, err)

		mounted, _ := d.DiskHotPlugger.IsMounted("")
		assert.True(t, mounted)
	})
}

func TestNodeUnpublishVolume(t *testing.T) {
	t.Run("Unmount the bind-mount volume", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		hotPlugger := &driver.FakeDiskHotPlugger{
			Formatted: true,
			Mounted:   true,
		}
		d.DiskHotPlugger = hotPlugger

		_, err := d.NodeUnpublishVolume(context.Background(), &csi.NodeUnpublishVolumeRequest{
			VolumeId:   "volume-1",
			TargetPath: "/var/lib/kubelet/some-path",
		})
		assert.Nil(t, err)

		mounted, _ := d.DiskHotPlugger.IsMounted("/var/lib/kubelet/some-path")
		assert.False(t, mounted)
	})
}

func TestNodeGetInfo(t *testing.T) {
	t.Run("Find out the instance ID", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		os.Setenv("NODE_ID", "instance-1")
		os.Setenv("REGION", "TESTING")

		resp, err := d.NodeGetInfo(context.Background(), &csi.NodeGetInfoRequest{})
		assert.Nil(t, err)

		assert.Equal(t, "instance-1", resp.NodeId)
		assert.Equal(t, driver.MaxVolumesPerNode, resp.MaxVolumesPerNode)
		assert.Equal(t, "TESTING", resp.AccessibleTopology.Segments["region"])
	})
}

func TestNodeGetVolumeStats(t *testing.T) {
	t.Run("Format and mount the volume to a global mount path", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		hotPlugger := &driver.FakeDiskHotPlugger{
			Formatted:  true,
			Mounted:    true,
			Mountpoint: "/mnt/volume-1",
		}
		d.DiskHotPlugger = hotPlugger

		stats, err := d.DiskHotPlugger.GetStatistics(hotPlugger.Mountpoint)
		assert.Nil(t, err)

		assert.Equal(t, stats.AvailableBytes, int64(3221225472))
		assert.Equal(t, stats.TotalBytes, int64(10737418240))
		assert.Equal(t, stats.UsedBytes, int64(7516192768))
		assert.Equal(t, stats.AvailableInodes, int64(3000))
		assert.Equal(t, stats.TotalInodes, int64(10000))
		assert.Equal(t, stats.UsedInodes, int64(7000))
	})
}
