package driver_test

import (
	"context"
	"testing"

	"github.com/civo/civo-csi/pkg/driver"
	"github.com/civo/civogo"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateVolume(t *testing.T) {
	tests := []struct {
		name             string
		req              *csi.CreateVolumeRequest
		existingVolume   *civogo.VolumeConfig
		expectedError    bool
		expectedVolumeID string
		expectedSizeGB   int
		expectedErrorMsg string
	}{
		{
			name: "Create a default size volume",
			req: &csi.CreateVolumeRequest{
				Name: "foo",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessType: &csi.VolumeCapability_Mount{},
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			expectedError:  false,
			expectedSizeGB: 10,
		},
		{
			name: "Disallow block volumes",
			req: &csi.CreateVolumeRequest{
				Name: "foo",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessType: &csi.VolumeCapability_Block{},
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			expectedError: true,
			expectedErrorMsg: "CreateVolume block types aren't supported, only mount types",
		},
		{
			name: "Create a specified size volume",
			req: &csi.CreateVolumeRequest{
				Name: "foo",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 26843545600, // 25GB
				},
			},
			expectedError:  false,
			expectedSizeGB: 25,
		},
		{
			name: "Don't create if the volume already exists and just return it",
			req: &csi.CreateVolumeRequest{
				Name: "foo",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			existingVolume: &civogo.VolumeConfig{
				Name:          "foo",
				SizeGigabytes: 10,
			},
			expectedError:  false,
			expectedSizeGB: 10,
		},
		{
			name: "Empty Name",
			req: &csi.CreateVolumeRequest{
				Name: "",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			expectedError:  true,
			expectedErrorMsg: "CreateVolume Name must be provided",
		},
		{
			name: "Missing VolumeCapabilities",
			req: &csi.CreateVolumeRequest{
				Name: "foo",
			},
			expectedError:    true,
			expectedErrorMsg: "CreateVolume Volume capabilities must be provided",
		},
		{
			name: "Unsupported Access Mode",
			req: &csi.CreateVolumeRequest{
				Name: "foo",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
						},
					},
				},
			},
			expectedError:    true,
			expectedErrorMsg: "CreateVolume access mode isn't supported",
		},
		{
			name: "Desired volume capacity exceeding the DiskGigabytesLimit",
			req: &csi.CreateVolumeRequest{
				Name: "foo",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 161061273600, // 150GB, DiskGigabytesLimit: 100GB for fakeClient
				},
			},
			expectedError:    true,
			expectedErrorMsg: "Requested volume would exceed volume space quota by 50 GB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup driver and environment
			d, _ := driver.NewTestDriver(nil)

			// Create existing volume if needed
			if tt.existingVolume != nil {
				volume, err := d.CivoClient.NewVolume(tt.existingVolume)
				assert.Nil(t, err)
				tt.expectedVolumeID = volume.ID
			}

			// Call CreateVolume
			resp, err := d.CreateVolume(context.Background(), tt.req)

			if tt.expectedError {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorMsg)
				return
			}

			assert.Nil(t, err)
			assert.NotNil(t, resp)
			assert.NotEmpty(t, resp.Volume.VolumeId)

			if tt.expectedVolumeID != "" {
				assert.Equal(t, tt.expectedVolumeID, resp.Volume.VolumeId)
			}

			// Validate volume creation
			volumes, _ := d.CivoClient.ListVolumes()
			assert.Equal(t, 1, len(volumes))
			assert.Equal(t, tt.req.Name, volumes[0].Name)
			assert.Equal(t, tt.expectedSizeGB, volumes[0].SizeGigabytes)
			assert.Equal(t, resp.Volume.VolumeId, volumes[0].ID)
		})
	}
}

func TestDeleteVolume(t *testing.T) {
	tests := []struct{
		name				string
		existingVolume		*civogo.VolumeConfig
		req 				*csi.DeleteVolumeRequest
		expectedError		bool
		expectedErrorMsg	string
	}{
		{
			name: "Delete an existing volume",
			existingVolume: &civogo.VolumeConfig{
				Name: "civolume",
			},
			req: &csi.DeleteVolumeRequest{},
			expectedError: false,
		},
		{
			name: "Delete a non-existent volume",
			req: &csi.DeleteVolumeRequest{
				VolumeId: "non-existent-id",
			},
			expectedError:    false,  // Non-existance is treated as success
		},
		{
			name:           "Delete with empty VolumeId",
			req:            &csi.DeleteVolumeRequest{VolumeId: ""},
			expectedError:  true,
			expectedErrorMsg: "must provide a VolumeId to DeleteVolume",
		},
	}

	for _, tt := range tests{
		t.Run(tt.name, func(t *testing.T) {
			d, _ := driver.NewTestDriver(nil)

			// setup existing volume if specified
			if tt.existingVolume != nil{
				v, err := d.CivoClient.NewVolume(tt.existingVolume)
				assert.Nil(t, err)
				tt.req.VolumeId = v.ID  // assign dynamically
			}

			// Perform the delete operation
			_, err := d.DeleteVolume(context.Background(), tt.req)

			// validate the error
			if tt.expectedError{
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorMsg)
			}else{
				assert.Nil(t, err)
			}

			// Check remaining volumes
			volumes, _ := d.CivoClient.ListVolumes()
			assert.Equal(t, 0, len(volumes))
		})
	}
}

func TestControllerPublishVolume(t *testing.T) {
	t.Run("Publish a volume", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		instanceID := "i-12345678"
		fc.Clusters = []civogo.KubernetesCluster{{
			ID: "12345678",
			Instances: []civogo.KubernetesInstance{{
				ID:       instanceID,
				Hostname: "instance-1",
			}},
		}}
		fc.Instances = []civogo.Instance{{
			ID:       instanceID,
			Hostname: "instance-1",
		}}
		d, _ := driver.NewTestDriver(fc)

		volume, err := d.CivoClient.NewVolume(&civogo.VolumeConfig{
			Name: "foo",
		})
		assert.Nil(t, err)

		_, err = d.ControllerPublishVolume(context.Background(), &csi.ControllerPublishVolumeRequest{
			VolumeId:         volume.ID,
			NodeId:           instanceID,
			VolumeCapability: &csi.VolumeCapability{},
		})
		assert.Nil(t, err)

		volumes, _ := d.CivoClient.ListVolumes()
		assert.Equal(t, instanceID, volumes[0].InstanceID)
	})
}

func TestControllerUnpublishVolume(t *testing.T) {
	t.Run("Unpublish a volume if attached to the correct node", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		volume, err := d.CivoClient.NewVolume(&civogo.VolumeConfig{
			Name: "foo",
		})
		assert.Nil(t, err)

		volConfig := civogo.VolumeAttachConfig{
			InstanceID: "instance-1",
			Region:     d.Region,
		}

		_, err = d.CivoClient.AttachVolume(volume.ID, volConfig)
		assert.Nil(t, err)

		_, err = d.ControllerUnpublishVolume(context.Background(), &csi.ControllerUnpublishVolumeRequest{
			VolumeId: volume.ID,
			NodeId:   "instance-1",
		})
		assert.Nil(t, err)

		volumes, _ := d.CivoClient.ListVolumes()
		assert.Equal(t, "", volumes[0].InstanceID)
	})

	t.Run("Doesn't unpublish a volume if attached to a different node", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		volume, err := d.CivoClient.NewVolume(&civogo.VolumeConfig{
			Name: "foo",
		})
		assert.Nil(t, err)

		volConfig := civogo.VolumeAttachConfig{
			InstanceID: "other-instance",
			Region:     d.Region,
		}
		_, err = d.CivoClient.AttachVolume(volume.ID, volConfig)
		assert.Nil(t, err)

		_, err = d.ControllerUnpublishVolume(context.Background(), &csi.ControllerUnpublishVolumeRequest{
			VolumeId: volume.ID,
			NodeId:   "this-instance",
		})
		assert.Nil(t, err)

		volumes, _ := d.CivoClient.ListVolumes()
		assert.Equal(t, "other-instance", volumes[0].InstanceID)
	})
}

func TestListVolumes(t *testing.T) {
	t.Run("Lists available existing volumes", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		volume, err := d.CivoClient.NewVolume(&civogo.VolumeConfig{
			Name: "foo",
		})
		assert.Nil(t, err)

		resp, err := d.ListVolumes(context.Background(), &csi.ListVolumesRequest{
			MaxEntries:    20,
			StartingToken: "",
		})
		assert.Nil(t, err)

		assert.Equal(t, volume.ID, resp.Entries[0].Volume.VolumeId)
	})
}

func TestGetCapacity(t *testing.T) {
	t.Run("Has available capacity from usage and limit", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		civoClient, _ := civogo.NewFakeClient()
		d.CivoClient = civoClient

		civoClient.Quota.DiskGigabytesUsage = 24
		civoClient.Quota.DiskGigabytesLimit = 25

		resp, err := d.GetCapacity(context.Background(), &csi.GetCapacityRequest{
			VolumeCapabilities: []*csi.VolumeCapability{},
			Parameters:         map[string]string{},
			AccessibleTopology: &csi.Topology{},
		})
		assert.Nil(t, err)

		assert.Equal(t, (1 * driver.BytesInGigabyte), resp.AvailableCapacity)
	})

	t.Run("Has no capacity from usage and limit", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		civoClient, _ := civogo.NewFakeClient()
		d.CivoClient = civoClient

		civoClient.Quota.DiskGigabytesUsage = 25
		civoClient.Quota.DiskGigabytesLimit = 25

		resp, err := d.GetCapacity(context.Background(), &csi.GetCapacityRequest{
			VolumeCapabilities: []*csi.VolumeCapability{},
			Parameters:         map[string]string{},
			AccessibleTopology: &csi.Topology{},
		})
		assert.Nil(t, err)

		assert.Equal(t, int64(0), resp.AvailableCapacity)
	})

	t.Run("Has no capacity from volume count limit", func(t *testing.T) {
		fc, _ := civogo.NewFakeClient()
		d, _ := driver.NewTestDriver(fc)

		civoClient, _ := civogo.NewFakeClient()
		d.CivoClient = civoClient

		civoClient.Quota.DiskVolumeCountUsage = 10
		civoClient.Quota.DiskVolumeCountLimit = 10

		resp, err := d.GetCapacity(context.Background(), &csi.GetCapacityRequest{
			VolumeCapabilities: []*csi.VolumeCapability{},
			Parameters:         map[string]string{},
			AccessibleTopology: &csi.Topology{},
		})
		assert.Nil(t, err)

		assert.Equal(t, int64(0), resp.AvailableCapacity)
	})
}

func TestControllerExpandVolume(t *testing.T) {
	tests := []struct {
		name           string
		volumeID       string
		capacityRange  *csi.CapacityRange
		initialVolume  *civogo.Volume
		expectedError  error
		expectedSizeGB int64
	}{
		{
			name:     "Successfully expand volume",
			volumeID: "vol-123",
			capacityRange: &csi.CapacityRange{
				RequiredBytes: 20 * driver.BytesInGigabyte,
			},
			initialVolume: &civogo.Volume{
				ID:            "vol-123",
				SizeGigabytes: 10,
				Status:        "available",
			},
			expectedError:  nil,
			expectedSizeGB: 20,
		},
		{
			name:     "Desired size not an exact multiple of BytesInGigabyte",
			volumeID: "vol-123",
			capacityRange: &csi.CapacityRange{
				RequiredBytes: 20*driver.BytesInGigabyte + 1, // 20 GB + 1 byte
			},
			initialVolume: &civogo.Volume{
				ID:            "vol-123",
				SizeGigabytes: 10,
				Status:        "available",
			},
			expectedError:  nil,
			expectedSizeGB: 21, // Desired size should be rounded up to 21 GB
		},
		{
			name:     "Volume ID is missing",
			volumeID: "",
			capacityRange: &csi.CapacityRange{
				RequiredBytes: 20 * driver.BytesInGigabyte,
			},
			initialVolume:  nil,
			expectedError:  status.Error(codes.InvalidArgument, "must provide a VolumeId to ControllerExpandVolume"),
			expectedSizeGB: 0,
		},
		{
			name:          "Capacity range is missing",
			volumeID:      "vol-123",
			capacityRange: nil,
			initialVolume: &civogo.Volume{
				ID:            "vol-123",
				SizeGigabytes: 10,
				Status:        "available",
			},
			expectedError:  status.Error(codes.InvalidArgument, "must provide a capacity range to ControllerExpandVolume"),
			expectedSizeGB: 0,
		},
		{
			name:     "Volume is already resizing",
			volumeID: "vol-123",
			capacityRange: &csi.CapacityRange{
				RequiredBytes: 20 * driver.BytesInGigabyte,
			},
			initialVolume: &civogo.Volume{
				ID:            "vol-123",
				SizeGigabytes: 10,
				Status:        "resizing",
			},
			expectedError:  status.Error(codes.Aborted, "volume is already being resized"),
			expectedSizeGB: 0,
		},
		{
			name:     "Volume is not available for expansion",
			volumeID: "vol-123",
			capacityRange: &csi.CapacityRange{
				RequiredBytes: 20 * driver.BytesInGigabyte,
			},
			initialVolume: &civogo.Volume{
				ID:            "vol-123",
				SizeGigabytes: 10,
				Status:        "attached",
			},
			expectedError:  status.Error(codes.FailedPrecondition, "volume is not in an availble state for OFFLINE expansion"),
			expectedSizeGB: 0,
		},
		{
			name:     "Desired size is smaller than current size",
			volumeID: "vol-123",
			capacityRange: &csi.CapacityRange{
				RequiredBytes: 5 * driver.BytesInGigabyte,
			},
			initialVolume: &civogo.Volume{
				ID:            "vol-123",
				SizeGigabytes: 10,
				Status:        "available",
			},
			expectedError:  nil,
			expectedSizeGB: 10,
		},
		{
			name:     "Failed to find the volume",
			volumeID: "vol-123",
			capacityRange: &csi.CapacityRange{
				RequiredBytes: 20 * driver.BytesInGigabyte,
			},
			initialVolume: &civogo.Volume{
				ID:            "vol-1234",
				SizeGigabytes: 10,
				Status:        "available",
			},
			expectedError:  status.Errorf(codes.Internal, "ControllerExpandVolume could not retrieve existing volume: ZeroMatchesError: unable to get volume vol-123"),
			expectedSizeGB: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc, _ := civogo.NewFakeClient()
			d, _ := driver.NewTestDriver(fc)

			// Populate the fake client with the initial volume
			if tt.initialVolume != nil {
				fc.Volumes = []civogo.Volume{*tt.initialVolume}
			}

			// Call the method under test
			resp, err := d.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
				VolumeId:      tt.volumeID,
				CapacityRange: tt.capacityRange,
			})

			// Assert the expected error
			if tt.expectedError != nil {
				assert.Equal(t, tt.expectedError, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expectedSizeGB*driver.BytesInGigabyte, resp.CapacityBytes)
				assert.True(t, resp.NodeExpansionRequired)
			}
		})
	}
}
