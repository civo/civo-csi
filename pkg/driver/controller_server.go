package driver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/civo/civogo"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BytesInGigabyte describes how many bytes are in a gigabyte
const BytesInGigabyte int64 = 1024 * 1024 * 1024

// CivoVolumeAvailableRetries is the number of times we will retry to check if a volume is available
const CivoVolumeAvailableRetries int = 20

var supportedAccessModes = []csi.VolumeCapability_AccessMode_Mode{
	csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
}

// CreateVolume is the first step when a PVC tries to create a dynamic volume
func (d *Driver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	log.Info().Msg("Request: CreateVolume")

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Name must be provided")
	}

	if req.VolumeCapabilities == nil || len(req.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Volume capabilities must be provided")
	}

	log.Info().Str("name", req.Name).Interface("capabilities", req.VolumeCapabilities).Msg("Creating volume")

	// Check capabilities
	for _, cap := range req.VolumeCapabilities {
		modeSupported := false
		for _, mode := range supportedAccessModes {
			if cap.GetAccessMode().GetMode() == mode {
				modeSupported = true
			}
		}

		if !modeSupported {
			return nil, status.Error(codes.InvalidArgument, "CreateVolume access mode isn't supported")
		}

		if _, ok := cap.GetAccessType().(*csi.VolumeCapability_Block); ok {
			return nil, status.Error(codes.InvalidArgument, "CreateVolume block types aren't supported, only mount types")
		}
	}

	// Determine required size
	bytes, err := getVolSizeInBytes(req.GetCapacityRange())
	if err != nil {
		return nil, err
	}

	desiredSize := bytes / BytesInGigabyte
	if (bytes % BytesInGigabyte) != 0 {
		desiredSize++
	}

	log.Debug().Int64("size_gb", desiredSize).Msg("Volume size determined")

	log.Debug().Msg("Listing current volumes in Civo API")
	volumes, err := d.CivoClient.ListVolumes()
	if err != nil {
		log.Error().Err(err).Msg("Unable to list volumes in Civo API")
		return nil, err
	}
	for _, v := range volumes {
		if v.Name == req.Name {
			log.Debug().Str("volume_id", v.ID).Msg("Volume already exists")
			if v.SizeGigabytes != int(desiredSize) {
				return nil, status.Error(codes.AlreadyExists, "Volume already exists with a differnt size")

			}

			available, err := d.waitForVolumeStatus(&v, "available", CivoVolumeAvailableRetries)
			if err != nil {
				log.Error().Err(err).Msg("Unable to wait for volume availability in Civo API")
				return nil, err
			}

			if available {
				return &csi.CreateVolumeResponse{
					Volume: &csi.Volume{
						VolumeId:      v.ID,
						CapacityBytes: int64(v.SizeGigabytes) * BytesInGigabyte,
					},
				}, nil
			}

			log.Error().Str("status", v.Status).Msg("Civo Volume is not 'available'")
			return nil, status.Errorf(codes.Unavailable, "Volume isn't available to be attached, state is currently %s", v.Status)
		}
	}
	log.Debug().Msg("Volume doesn't currently exist, will need creating")

	log.Debug().Msg("Requesting available capacity in client's quota from the Civo API")
	quota, err := d.CivoClient.GetQuota()
	if err != nil {
		log.Error().Err(err).Msg("Unable to get quota from Civo API")
		return nil, err
	}
	availableSize := int64(quota.DiskGigabytesLimit - quota.DiskGigabytesUsage)
	if availableSize < desiredSize {
		log.Error().Msg("Requested volume would exceed storage quota available")
		return nil, status.Error(codes.OutOfRange, fmt.Sprintf("Requested volume would exceed volume space quota by %d GB", desiredSize-availableSize))
	} else if quota.DiskVolumeCountUsage >= quota.DiskVolumeCountLimit {
		log.Error().Msg("Requested volume would exceed volume quota available")
		return nil, status.Error(codes.OutOfRange, fmt.Sprintf("Requested volume would exceed volume count limit quota of %d", quota.DiskVolumeCountLimit))
	}

	log.Debug().Int("disk_gb_limit", quota.DiskGigabytesLimit).Int("disk_gb_usage", quota.DiskGigabytesUsage).Msg("Quota has sufficient capacity remaining")

	v := &civogo.VolumeConfig{
		Name:          req.Name,
		Region:        d.Region,
		Namespace:     d.Namespace,
		ClusterID:     d.ClusterID,
		SizeGigabytes: int(desiredSize),
	}
	log.Debug().Msg("Creating volume in Civo API")
	result, err := d.CivoClient.NewVolume(v)
	if err != nil {
		log.Error().Err(err).Msg("Unable to create volume in Civo API")
		return nil, err
	}

	log.Info().Str("volume_id", result.ID).Msg("Volume created in Civo API")

	volume, err := d.CivoClient.GetVolume(result.ID)
	if err != nil {
		log.Error().Err(err).Msg("Unable to get volume updates in Civo API")
		return nil, err
	}

	log.Debug().Str("volume_id", result.ID).Msg("Waiting for volume to become available in Civo API")
	available, err := d.waitForVolumeStatus(volume, "available", CivoVolumeAvailableRetries)
	if err != nil {
		log.Error().Err(err).Msg("Volume availability never completed successfully in Civo API")
		return nil, err
	}

	if available {
		return &csi.CreateVolumeResponse{
			Volume: &csi.Volume{
				VolumeId:      volume.ID,
				CapacityBytes: int64(v.SizeGigabytes) * BytesInGigabyte,
			},
		}, nil
	}

	log.Error().Err(err).Msg("Civo Volume is not 'available'")
	return nil, status.Errorf(codes.Unavailable, "Civo Volume is not 'available'")
}

// waitForVolumeAvailable will just sleep/loop waiting for Civo's API to report it's available, or hit a defined
// number of retries
func (d *Driver) waitForVolumeStatus(vol *civogo.Volume, desiredStatus string, retries int) (bool, error) {
	log.Info().Str("volume_id", vol.ID).Str("desired_state", desiredStatus).Msg("Waiting for Volume to entered desired state")
	var v *civogo.Volume
	var err error

	if d.TestMode {
		return true, nil
	}

	for i := 0; i < retries; i++ {
		time.Sleep(5 * time.Second)

		v, err = d.CivoClient.GetVolume(vol.ID)
		if err != nil {
			log.Error().Err(err).Msg("Unable to get volume updates in Civo API")
			return false, err
		}

		if v.Status == desiredStatus {
			return true, nil
		}
	}
	return false, fmt.Errorf("volume isn't %s, state is currently %s", desiredStatus, v.Status)
}

// DeleteVolume is used once a volume is unused and therefore unmounted, to stop the resources being used and subsequent billing
func (d *Driver) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Info().Str("volume_id", req.VolumeId).Msg("Request: DeleteVolume")

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to DeleteVolume")
	}

	log.Debug().Msg("Deleting volume in Civo API")
	_, err := d.CivoClient.DeleteVolume(req.VolumeId)
	if err != nil {
		if strings.Contains(err.Error(), "DatabaseVolumeNotFoundError") {
			log.Info().Str("volume_id", req.VolumeId).Msg("Volume already deleted from Civo API")
			return &csi.DeleteVolumeResponse{}, nil
		}

		log.Error().Err(err).Msg("Unable to delete volume in Civo API")
		return nil, err
	}

	log.Info().Str("volume_id", req.VolumeId).Msg("Volume deleted from Civo API")

	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume is used to mount an underlying volume to required k3s node
func (d *Driver) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	log.Info().Str("volume_id", req.VolumeId).Str("node_id", req.NodeId).Msg("Request: ControllerPublishVolume")

	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeCapability to ControllerPublishVolume")
	}

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to ControllerPublishVolume")
	}

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a NodeId to ControllerPublishVolume")
	}

	log.Debug().Msg("Check if Node exits")
	cluster, err := d.CivoClient.GetKubernetesCluster(d.ClusterID)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("unable to connect to Civo Api. error: %s", err.Error()))
	}
	found := false
	for _, instance := range cluster.Instances {
		if instance.ID == req.NodeId {
			found = true
			break
		}
	}
	if !found {
		return nil, status.Error(codes.NotFound, "Unable to find instance to attach volume to")
	}

	log.Debug().Msg("Finding volume in Civo API")
	volume, err := d.CivoClient.GetVolume(req.VolumeId)
	if err != nil {
		log.Error().Err(err).Msg("Unable to find volume for publishing in Civo API")
		return nil, err
	}
	log.Debug().Str("volume_id", volume.ID).Msg("Volume found for publishing in Civo API")

	// Check if the volume is already attached to the requested node
	if volume.InstanceID == req.NodeId && volume.Status == "attached" {
		log.Info().Str("volume_id", volume.ID).Str("instance_id", req.NodeId).Msg("Volume is already attached to the requested instance")
		return &csi.ControllerPublishVolumeResponse{}, nil
	}

	// if the volume is not available, we can't attach it, so error out
	if volume.Status != "available" && volume.InstanceID != req.NodeId {
		log.Error().
			Str("volume_id", volume.ID).
			Str("status", volume.Status).
			Str("requested_instance_id", req.NodeId).
			Str("current_instance_id", volume.InstanceID).
			Msg("Volume is not available to be attached")
		return nil, status.Errorf(codes.Unavailable, "Volume is not available to be attached")
	}

	// Check if the volume is attaching to this node
	if volume.InstanceID == req.NodeId && volume.Status != "attaching" {
		// Do nothing, the volume is already attaching
		log.Debug().Str("volume_id", volume.ID).Str("status", volume.Status).Msg("Volume is already attaching")
	} else {
		// Call the CivoAPI to attach it to a node/instance
		log.Debug().
			Str("volume_id", volume.ID).
			Str("volume_status", volume.Status).
			Str("reqested_instance_id", req.NodeId).
			Msg("Requesting volume to be attached in Civo API")
		_, err = d.CivoClient.AttachVolume(req.VolumeId, req.NodeId)
		if err != nil {
			log.Error().Err(err).Msg("Unable to attach volume in Civo API")
			return nil, err
		}
		log.Info().Str("volume_id", volume.ID).Str("instance_id", req.NodeId).Msg("Volume successfully requested to be attached in Civo API")
	}

	time.Sleep(5 * time.Second)
	// refetch the volume
	log.Info().Str("volume_id", volume.ID).Msg("Fetching volume again to check status after attaching")
	volume, err = d.CivoClient.GetVolume(req.VolumeId)
	if err != nil {
		log.Error().Err(err).Msg("Unable to fetch volume from Civo API")
		return nil, err
	}
	if volume.Status != "attached" {
		log.Error().Str("volume_id", volume.ID).Str("status", volume.Status).Msg("Volume is not in the attached state")
		return nil, status.Errorf(codes.Unavailable, "Volume is not attached to the requested instance")
	}

	if volume.InstanceID != req.NodeId {
		log.Error().Str("volume_id", volume.ID).Str("instance_id", req.NodeId).Msg("Volume is not attached to the requested instance")
		return nil, status.Errorf(codes.Unavailable, "Volume is not attached to the requested instance")
	}

	log.Debug().Str("volume_id", volume.ID).Msg("Volume successfully attached in Civo API")
	return &csi.ControllerPublishVolumeResponse{}, nil
}

// ControllerUnpublishVolume detaches the volume from the k3s node it was connected
func (d *Driver) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	log.Info().Str("volume_id", req.VolumeId).Msg("Request: ControllerUnpublishVolume")

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to ControllerUnpublishVolume")
	}

	log.Debug().Msg("Finding volume in Civo API")
	volume, err := d.CivoClient.GetVolume(req.VolumeId)
	if err != nil {
		if strings.Contains(err.Error(), "DatabaseVolumeNotFoundError") || strings.Contains(err.Error(), "ZeroMatchesError") {
			log.Info().Str("volume_id", req.VolumeId).Msg("Volume already deleted from Civo API, pretend it's unmounted")
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		log.Debug().Str("message", err.Error()).Msg("Error didn't match DatabaseVolumeNotFoundError")

		log.Error().Err(err).Msg("Unable to find volume for unpublishing in Civo API")
		return nil, err
	}

	log.Debug().Str("volume_id", volume.ID).Msg("Volume found for unpublishing in Civo API")

	// If the volume is currently available, it's not attached to anything to return success
	if volume.Status == "available" {
		log.Info().Str("volume_id", volume.ID).Msg("Volume is already available, no need to unpublish")
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// If requeseted node doesn't match the current volume instance, return success
	if volume.InstanceID != req.NodeId {
		log.Info().Str("volume_id", volume.ID).Str("instance_id", volume.InstanceID).Str("requested_instance_id", req.NodeId).Msg("Volume is not attached to the requested instance")
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	if volume.Status != "detaching" {
		// The volume is either attached to the requested node or the requested node is empty
		// and the volume is attached, so we need to detach the volume
		log.Info().
			Str("volume_id", volume.ID).
			Str("current_instance_id", volume.InstanceID).
			Str("requested_instance_id", req.NodeId).
			Str("status", volume.Status).
			Msg("Requesting volume to be detached")

		_, err = d.CivoClient.DetachVolume(req.VolumeId)
		if err != nil {
			log.Error().Err(err).Msg("Unable to detach volume in Civo API")
			return nil, err
		}

		log.Info().Str("volume_id", volume.ID).Msg("Volume sucessfully requested to be detached in Civo API")
	}

	// Fetch the new state after 5 seconds
	time.Sleep(5 * time.Second)
	volume, err = d.CivoClient.GetVolume(req.VolumeId)
	if err != nil {
		log.Error().Err(err).Msg("Unable to find volume for unpublishing in Civo API")
		return nil, err
	}

	if volume.Status == "available" {
		log.Debug().Str("volume_id", volume.ID).Msg("Volume is now available again")
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// err that the the volume is not available
	log.Error().Msg("Civo Volume did not go back to 'available' status")
	return nil, status.Errorf(codes.Unavailable, "Civo Volume did not go back to 'available'")
}

// ControllerExpandVolume allows for offline expansion of Volumes
func (d *Driver) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	volID := req.GetVolumeId()

	log.Info().Str("volume_id", volID).Msg("Request: ControllerExpandVolume")

	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to ControllerExpandVolume")
	}

	// Get the volume from the Civo API
	volume, err := d.CivoClient.GetVolume(volID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ControllerExpandVolume could not retrieve existing volume: %v", err)
	}

	if req.CapacityRange == nil {
		return nil, status.Error(codes.InvalidArgument, "must provide a capacity range to ControllerExpandVolume")
	}
	bytes, err := getVolSizeInBytes(req.GetCapacityRange())
	if err != nil {
		return nil, err
	}
	desiredSize := bytes / BytesInGigabyte
	if (bytes % BytesInGigabyte) != 0 {
		desiredSize++
	}
	log.Debug().Int("current_size", volume.SizeGigabytes).Int64("desired_size", desiredSize).Str("state", volume.Status).Msg("Volume found")

	if volume.Status == "resizing" {
		return nil, status.Error(codes.Aborted, "volume is already being resized")
	}

	if desiredSize <= int64(volume.SizeGigabytes) {
		log.Info().Str("volume_id", volID).Msg("Volume is currently larger that desired Size")
		return &csi.ControllerExpandVolumeResponse{CapacityBytes: int64(volume.SizeGigabytes) * BytesInGigabyte, NodeExpansionRequired: true}, nil
	}

	if volume.Status != "available" {
		return nil, status.Error(codes.FailedPrecondition, "volume is not in an availble state for OFFLINE expansion")
	}

	log.Info().Int64("size_gb", desiredSize).Str("volume_id", volID).Msg("Volume resize request sent")
	d.CivoClient.ResizeVolume(volID, int(desiredSize))

	// Resizes can take a while, double the number of normal retries
	available, err := d.waitForVolumeStatus(volume, "available", CivoVolumeAvailableRetries*2)
	if err != nil {
		log.Error().Err(err).Msg("Unable to wait for volume availability in Civo API")
		return nil, err
	}

	if !available {
		return nil, status.Error(codes.Internal, "failed to wait for volume to be in an available state")
	}

	volume, _ = d.CivoClient.GetVolume(volID)
	log.Info().Int64("size_gb", int64(volume.SizeGigabytes)).Str("volume_id", volID).Msg("Volume succesfully resized")
	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         int64(volume.SizeGigabytes) * BytesInGigabyte,
		NodeExpansionRequired: true,
	}, nil

}

// ControllerGetVolume is for optional Kubernetes health checking of volumes and we don't support it yet
func (d *Driver) ControllerGetVolume(context.Context, *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ValidateVolumeCapabilities returns the features of the volume, e.g. RW, RO, RWX
func (d *Driver) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	log.Info().Str("volume_id", req.VolumeId).Msg("Request: ValidateVolumeCapabilities")

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to ValidateVolumeCapabilities")
	}

	if req.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "must provide VolumeCapabilities to ValidateVolumeCapabilities")
	}

	_, err := d.CivoClient.GetVolume(req.VolumeId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "Unable to fetch volume from Civo API")
	}

	accessModeSupported := false
	for _, cap := range req.VolumeCapabilities {
		for _, m := range supportedAccessModes {
			if m == cap.AccessMode.Mode {
				accessModeSupported = true
			}
		}
	}

	if !accessModeSupported {
		return nil, status.Errorf(codes.NotFound, "%v not supported", req.GetVolumeCapabilities())
	}

	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.VolumeCapabilities,
		},
	}

	return resp, nil
}

// ListVolumes returns the existing Civo volumes for this customer
func (d *Driver) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	if req.StartingToken != "" {
		return &csi.ListVolumesResponse{}, status.Errorf(codes.Aborted, "%v not supported", "starting-token")
	}

	log.Info().Msg("Request: ListVolumes")

	log.Debug().Msg("Listing all volume in Civo API")
	volumes, err := d.CivoClient.ListVolumes()
	if err != nil {
		log.Error().Err(err).Msg("Unable to list volumes in Civo API")
		return nil, err
	}
	log.Debug().Msg("Successfully retrieved all volumes from the Civo API")

	resp := &csi.ListVolumesResponse{
		Entries: []*csi.ListVolumesResponse_Entry{},
	}

	for _, v := range volumes {
		resp.Entries = append(resp.Entries, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				CapacityBytes: int64(v.SizeGigabytes) * BytesInGigabyte,
				VolumeId:      v.ID,
				ContentSource: &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Volume{},
				},
			},
			Status: &csi.ListVolumesResponse_VolumeStatus{},
		})
	}

	return resp, nil
}

// GetCapacity calls the Civo API to determine the user's available quota
func (d *Driver) GetCapacity(context.Context, *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	log.Info().Msg("Request: GetCapacity")

	log.Debug().Msg("Requesting available capacity in client's quota from the Civo API")
	quota, err := d.CivoClient.GetQuota()
	if err != nil {
		log.Error().Err(err).Msg("Unable to get quota in Civo API")
		return nil, err
	}
	log.Debug().Msg("Successfully retrieved quota from the Civo API")

	availableBytes := int64(quota.DiskGigabytesLimit-quota.DiskGigabytesUsage) * BytesInGigabyte
	log.Debug().Int64("available_gb", availableBytes/BytesInGigabyte).Msg("Available capacity determined")
	if availableBytes < BytesInGigabyte {
		log.Error().Int64("available_bytes", availableBytes).Msg("Available capacity is less than 1GB, volumes can't be launched")
	}

	if quota.DiskVolumeCountUsage >= quota.DiskVolumeCountLimit {
		log.Error().Msg("Number of volumes is at the quota limit, no capacity left")
		availableBytes = 0
	}

	resp := &csi.GetCapacityResponse{
		AvailableCapacity: availableBytes,
	}

	return resp, nil
}

// ControllerGetCapabilities returns the capabilities of the controller, what features it implements
func (d *Driver) ControllerGetCapabilities(context.Context, *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	log.Info().Msg("Request: ControllerGetCapabilities")

	rawCapabilities := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_GET_CAPACITY,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	}

	var csc []*csi.ControllerServiceCapability

	for _, cap := range rawCapabilities {
		csc = append(csc, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		})
	}

	log.Debug().Interface("capabilities", csc).Msg("Capabilities for controller requested")

	resp := &csi.ControllerGetCapabilitiesResponse{
		Capabilities: csc,
	}

	return resp, nil
}

// CreateSnapshot is part of implementing Snapshot & Restore functionality, but we don't support that
func (d *Driver) CreateSnapshot(context.Context, *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// DeleteSnapshot is part of implementing Snapshot & Restore functionality, but we don't support that
func (d *Driver) DeleteSnapshot(context.Context, *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ListSnapshots is part of implementing Snapshot & Restore functionality, but we don't support that
func (d *Driver) ListSnapshots(context.Context, *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func getVolSizeInBytes(capRange *csi.CapacityRange) (int64, error) {
	var bytes int64

	if capRange == nil {
		return int64(DefaultVolumeSizeGB) * BytesInGigabyte, nil
	}

	// Volumes can be of a flexible size, but they must specify one of the fields, so we'll use that
	bytes = capRange.GetRequiredBytes()
	if bytes == 0 {
		bytes = capRange.GetLimitBytes()
	}

	return bytes, nil
}
