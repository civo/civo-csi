package driver

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
)

// GetPluginInfo returns the name and volume of our driver
func (d *Driver) GetPluginInfo(context.Context, *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	log.Debug().Msg("Plugin name/version requested")

	return &csi.GetPluginInfoResponse{
		Name:          "com.civo.csi",
		VendorVersion: Version,
	}, nil
}

// GetPluginCapabilities returns a list of the capabilities of this controller plugin
func (d *Driver) GetPluginCapabilities(context.Context, *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	log.Debug().Msg("Plugin capabilities requested")

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}

// Probe is a health check for the driver
func (d *Driver) Probe(context.Context, *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	// Not sure how to implement this probe health check the right way - check the Civo API is responsive?
	return &csi.ProbeResponse{}, nil
}
