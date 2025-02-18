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
	t.Run("Create a default size volume", func(t *testing.T) {
		d, _ := driver.NewTestDriver(nil)

		resp, err := d.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
			Name: "foo",
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
		})
		assert.Nil(t, err)

		volumes, _ := d.CivoClient.ListVolumes()
		assert.Equal(t, "foo", volumes[0].Name)
		assert.Equal(t, 10, volumes[0].SizeGigabytes)
		assert.Equal(t, volumes[0].ID, resp.Volume.VolumeId)
	})

	t.Run("Disallow block volumes", func(t *testing.T) {
		d, _ := driver.NewTestDriver(nil)

		_, err := d.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
			Name: "foo",
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
		})
		assert.NotNil(t, err)
	})

	t.Run("Create a specified size volume", func(t *testing.T) {
		d, _ := driver.NewTestDriver(nil)

		_, err := d.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
			Name: "foo",
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 26843545600,
			},
		})
		assert.Nil(t, err)

		volumes, _ := d.CivoClient.ListVolumes()
		assert.Equal(t, 25, volumes[0].SizeGigabytes)
	})

	t.Run("Don't create if the volume already exists and just return it", func(t *testing.T) {
		d, _ := driver.NewTestDriver(nil)

		volume, err := d.CivoClient.NewVolume(&civogo.VolumeConfig{
			Name:          "foo",
			SizeGigabytes: 10,
		})
		assert.Nil(t, err)

		resp, err := d.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
			Name: "foo",
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
		})
		assert.Nil(t, err)

		assert.Equal(t, volume.ID, resp.Volume.VolumeId)

		volumes, _ := d.CivoClient.ListVolumes()
		assert.Equal(t, 1, len(volumes))
	})
}

func TestDeleteVolume(t *testing.T) {
	t.Run("Delete a volume", func(t *testing.T) {
		d, _ := driver.NewTestDriver(nil)

		volume, err := d.CivoClient.NewVolume(&civogo.VolumeConfig{
			Name: "foo",
		})
		assert.Nil(t, err)

		_, err = d.DeleteVolume(context.Background(), &csi.DeleteVolumeRequest{
			VolumeId: volume.ID,
		})
		assert.Nil(t, err)

		volumes, _ := d.CivoClient.ListVolumes()
		assert.Equal(t, 0, len(volumes))
	})
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

func TestCreateSnapshot(t *testing.T){
	tests := []struct {
		name            string
		req  			*csi.CreateSnapshotRequest
		sourceVolume    *civogo.Volume
		volSnapshots	[]civogo.VolumeSnapshot
		expectedError   error
		expectedResp 	*csi.CreateSnapshotResponse
	}{
		{
			name: "snapshot name missing",
			req: &csi.CreateSnapshotRequest{
				Name: "snapshot-vol-1",
				SourceVolumeId: "vol-1",
			},
			sourceVolume: &civogo.Volume{
				ID: "vol-1",
			},
			volSnapshots: []civogo.VolumeSnapshot{
				{
					Name: "snapshot-vol-1",
					VolumeID: "vol-x",
				},
			},
			expectedError: status.Error(codes.AlreadyExists, "snapshot with the same name \"snapshot-vol-1\" but with different SourceVolumeId already exist"),
			expectedResp: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc, _ := civogo.NewFakeClient()
			fc.Volumes = []civogo.Volume{*tt.sourceVolume}
			fc.VolumeSnapshots = tt.volSnapshots


			d, _ := driver.NewTestDriver(fc)

			resp, err := d.CreateSnapshot(context.Background(), tt.req)

			if tt.expectedError != nil {
				assert.Nil(t, resp)
				assert.Equal(t, tt.expectedError, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expectedResp, resp)
			}
		})
	}
	
}

func TestListSnapshots(t *testing.T) {
	tests := []struct {
		name          string
		req           *csi.ListSnapshotsRequest
		snapshots     []civogo.VolumeSnapshot
		expectedError error
		expectedResp  *csi.ListSnapshotsResponse
	}{
		{
			name: "No snapshots found",
			req:  &csi.ListSnapshotsRequest{},
			snapshots: []civogo.VolumeSnapshot{},
			expectedResp: &csi.ListSnapshotsResponse{
				Entries: []*csi.ListSnapshotsResponse_Entry{},
			},
		},
		{
			name: "starting token provided",
			req:  &csi.ListSnapshotsRequest{
				StartingToken: "12",
			},
			snapshots: []civogo.VolumeSnapshot{},
			expectedError: status.Error(codes.Aborted, "starting-token not supported"),
			expectedResp: nil,
		},
		{
			name: "Both snapshotID and sourceVolumeID given",
			req:  &csi.ListSnapshotsRequest{
				SnapshotId: "snap-1",
				SourceVolumeId: "vol-1",
			},
			snapshots: []civogo.VolumeSnapshot{
				{SnapshotID: "snap-1", VolumeID: "vol-1"},
			},
			expectedResp: &csi.ListSnapshotsResponse{
				Entries: []*csi.ListSnapshotsResponse_Entry{
					{Snapshot: &csi.Snapshot{SnapshotId: "snap-1", SourceVolumeId: "vol-1"}},
				},
			},
		},
		{
			name: "Only snapshotID given",
			req:  &csi.ListSnapshotsRequest{
				SnapshotId: "snap-1",
			},
			snapshots: []civogo.VolumeSnapshot{
				{SnapshotID: "snap-1", VolumeID: "vol-1"},
			},
			expectedResp: &csi.ListSnapshotsResponse{
				Entries: []*csi.ListSnapshotsResponse_Entry{
					{Snapshot: &csi.Snapshot{SnapshotId: "snap-1", SourceVolumeId: "vol-1"}},
				},
			},
		},
		{
			name: "non-existing snapshotID given",
			req:  &csi.ListSnapshotsRequest{
				SnapshotId: "snap-2",
			},
			snapshots: []civogo.VolumeSnapshot{
				{SnapshotID: "snap-1", VolumeID: "vol-1"},
			},
			expectedResp: &csi.ListSnapshotsResponse{},
		},
		{
			name: "Only sourceVolumeID having single snapshot given",
			req:  &csi.ListSnapshotsRequest{
				SourceVolumeId: "vol-1",
			},
			snapshots: []civogo.VolumeSnapshot{
				{SnapshotID: "snap-1", VolumeID: "vol-1"},
			},
			expectedResp: &csi.ListSnapshotsResponse{
				Entries: []*csi.ListSnapshotsResponse_Entry{
					{Snapshot: &csi.Snapshot{SnapshotId: "snap-1", SourceVolumeId: "vol-1"}},
				},
			},
		},
		{
			name: "Only sourceVolumeID having multiple snapshots given",
			req:  &csi.ListSnapshotsRequest{
				SourceVolumeId: "vol-1",
			},
			snapshots: []civogo.VolumeSnapshot{
				{SnapshotID: "snap-1", VolumeID: "vol-1"},
				{SnapshotID: "snap-2", VolumeID: "vol-1"},
				{SnapshotID: "snap-3", VolumeID: "vol-1"},
			},
			expectedResp: &csi.ListSnapshotsResponse{
				Entries: []*csi.ListSnapshotsResponse_Entry{
					{Snapshot: &csi.Snapshot{SnapshotId: "snap-1", SourceVolumeId: "vol-1"}},
					{Snapshot: &csi.Snapshot{SnapshotId: "snap-2", SourceVolumeId: "vol-1"}},
					{Snapshot: &csi.Snapshot{SnapshotId: "snap-3", SourceVolumeId: "vol-1"}},
				},
			},
		},
		{
			name: "Multiple snapshots found",
			req:  &csi.ListSnapshotsRequest{},
			snapshots: []civogo.VolumeSnapshot{
				{SnapshotID: "snap-2", VolumeID: "vol-2"},
				{SnapshotID: "snap-1", VolumeID: "vol-1"},
			},
			expectedResp: &csi.ListSnapshotsResponse{
				Entries: []*csi.ListSnapshotsResponse_Entry{
					{Snapshot: &csi.Snapshot{SnapshotId: "snap-1", SourceVolumeId: "vol-1"}},
					{Snapshot: &csi.Snapshot{SnapshotId: "snap-2", SourceVolumeId: "vol-2"}},
				},
			},
		},
		{
			name: "Empty snapshot creationTime",
			req:  &csi.ListSnapshotsRequest{},
			snapshots: []civogo.VolumeSnapshot{
				{SnapshotID: "snap-1", VolumeID: "vol-1", CreationTime: ""},
			},
			expectedResp: &csi.ListSnapshotsResponse{
				Entries: []*csi.ListSnapshotsResponse_Entry{
					{Snapshot: &csi.Snapshot{SnapshotId: "snap-1", SourceVolumeId: "vol-1"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc, _ := civogo.NewFakeClient()
			fc.VolumeSnapshots = tt.snapshots

			d, _ := driver.NewTestDriver(fc)

			resp, err := d.ListSnapshots(context.Background(), tt.req)

			if tt.expectedError != nil {
				assert.Equal(t, tt.expectedError, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expectedResp, resp)
			}
		})
	}
}

func TestConvertSnapshot(t *testing.T) {
	creationTime := "2024-02-12T10:00:00Z"
	expectedTime, _ := driver.ParseTimeToProtoTimestamp(creationTime)
	tests := []struct {
		name          string
		input         *civogo.VolumeSnapshot
		expected      *csi.ListSnapshotsResponse_Entry
		expectedError bool
	}{
		{
			name: "Valid Snapshot Conversion",
			input: &civogo.VolumeSnapshot{
				SnapshotID:       "snap-123",
				VolumeID:         "vol-123",
				SourceVolumeName: "vol1",
				RestoreSize:      1024,
				State:            "Available",
				CreationTime:     creationTime,
			},
			expected: &csi.ListSnapshotsResponse_Entry{
				Snapshot: &csi.Snapshot{
					SnapshotId:     "snap-123",
					SourceVolumeId: "vol-123",
					SizeBytes:      1024,
					ReadyToUse:     true, 
					CreationTime: expectedTime,
				},
			},
			expectedError: false,
		},
		{
			name: "Invalid Creation Time",
			input: &civogo.VolumeSnapshot{
				SnapshotID:       "snap-456",
				VolumeID:         "vol-456",
				RestoreSize:      2048,
				State:            "creating",
				CreationTime:     "invalid-time",
			},
			expected:      nil,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := driver.ConvertSnapshot(tt.input)

			if (err != nil) != tt.expectedError {
				t.Errorf("convertSnapshot() error = %v, expectedError %v", err, tt.expectedError)
			}

			if err == nil && !assert.Equal(t, result, tt.expected) {
				t.Errorf("Got:\n%v\n\n, expected:\n%v\n\n", result, tt.expected)
			}
		})
	}
}
