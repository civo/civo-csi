package driver

import (
	"context"

	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FixHangingVolume cleans up civovolumes which do not have a corresponding PV
func (d Driver) FixHangingVolume() error {
	log.Info().Msg("Fixing hanging volumes")
	volumes, err := d.CivoClient.ListVolumes()
	if err != nil {
		log.Error().Err(err).Msg("Failed to list civo volumes")
		return err
	}

	pvs, err := d.KubeClient.CoreV1().PersistentVolumes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to list kubernetes persistent volumes")
		return err
	}

	volumeIDsToDelete := make([]string, 0)
	if len(pvs.Items) != len(volumes) {
		log.Info().Msg("Number of civo volumes and persistent volumes are not the same")
		// Check if there are any civo volumes that are not in the list of PVs
		for _, volume := range volumes {
			var found bool
			for _, pv := range pvs.Items {
				if pv.Name == volume.Name {
					found = true
					break
				}
			}

			// Check if volume has a cluster ID and belongs to the cluster CSI is managing
			if !found && volume.ClusterID == d.ClusterID {
				volumeIDsToDelete = append(volumeIDsToDelete, volume.ID)
			}

		}
	}

	for _, volumeID := range volumeIDsToDelete {
		log.Info().Msgf("Deleting volume %s", volumeID)
		_, err = d.CivoClient.DeleteVolume(volumeID)
		if err != nil {
			log.Error().Err(err).Msg("Failed to delete volume")
			return err
		}
	}

	return nil
}
