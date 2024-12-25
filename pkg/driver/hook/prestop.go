package hook

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

var drainTaints = map[string]struct{}{
	v1.TaintNodeUnschedulable: {}, // Kubernetes common eviction taint (kubectl drain)
}

// PreStop handles the PreStop lifecycle event. It retrieves the node information
// from the Kubernetes API and checks if the node is being drained. If the node is not
// being drained, it skips the VolumeAttachment cleanup check. If the node is being drained,
// it waits for the cleanup of VolumeAttachments. If any errors occur during this process,
// they are logged and returned.
func (h *hook) PreStop(ctx context.Context) error {
	node, err := h.client.CoreV1().Nodes().Get(ctx, h.nodeName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		log.Info().
			Str("node_name", h.nodeName).
			Msg("Node does not found, assuming the termination event, the node might be in the process of being removed")
	}

	isDrained := true
	if !isNodeDrained(node) {
		isDrained = false
		log.Info().
			Str("node_name", h.nodeName).
			Msg("Node is not being drained, skipping VolumeAttachments cleanup check")
		return nil
	}

	log.Info().
		Str("node_name", h.nodeName).
		Bool("is_drained", isDrained).
		Msg("Node is being drained or removed, starting the wait for VolumeAttachments cleanup")

	if err := h.waitForVolumeAttachmentsCleanup(ctx); err != nil {
		log.Error().
			Err(err).
			Str("node_name", h.nodeName).
			Msg("Failed to wait for VolumeAttachments cleanup")
		return err
	}

	log.Info().
		Str("node_name", h.nodeName).
		Msg("Finished waiting for VolumeAttachments cleanup")

	return nil
}

func isNodeDrained(node *v1.Node) bool {
	for _, traint := range node.Spec.Taints {
		if _, ok := drainTaints[traint.Key]; ok {
			return true
		}
	}
	return false
}

func (h *hook) waitForVolumeAttachmentsCleanup(ctx context.Context) error {
	factory := informers.NewSharedInformerFactory(h.client, 0)
	informer := factory.Storage().V1().VolumeAttachments().Informer()
	informerCh := make(chan struct{})

	// Since the event is called asynchronously, there is a risk of duplicate closures, so we are using sync.Once just to be safe.
	var once sync.Once
	stopInformerFn := func() {
		once.Do(func() {
			close(informerCh)
		})
	}
	defer stopInformerFn()

	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			log.Warn().Msg("Received an object in DeleteFunc")
			if err := h.volumeAttachmentEventHandler(ctx, obj); err != nil {
				log.Error().
					Str("node_name", h.nodeName).
					Err(err).
					Msg("An error occurred while handling the VolumeAttachment event in DeleteFunc")
				return
			}
			stopInformerFn()
		},
		UpdateFunc: func(_, obj interface{}) {
			log.Warn().Msg("Received an object in UpdateFunc")
			if err := h.volumeAttachmentEventHandler(ctx, obj); err != nil {
				log.Error().
					Str("node_name", h.nodeName).
					Err(err).
					Msg("An error occurred while handling the VolumeAttachment event in UpdateFunc")
				return
			}
			stopInformerFn()
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create event handler for VolumeAttachment: %w", err)
	}
	go informer.Run(informerCh)

	ok, err := h.checkVolumeAttachmentsExist(ctx)
	if err == nil {
		return nil
	} else if !ok {
		log.Error().
			Str("node_name", h.nodeName).
			Err(err).
			Msg("Failed to check the existence of VolumeAttachments")
		return fmt.Errorf("failed to check the existence of VolumeAttachments: %w", err)
	}

	log.Error().
		Str("node_name", h.nodeName).
		Err(err).
		Msg("An error occurred while checking the existence of VolumeAttachments, waiting for VolumeAttachments to be deleted")

	select {
	case <-informerCh:
	case <-ctx.Done():
		log.Error().
			Err(ctx.Err()).
			Msg("Stopped waiting for VolumeAttachments, therefore some resources might still remain")
	}
	return nil
}

func (h *hook) volumeAttachmentEventHandler(ctx context.Context, obj interface{}) error {
	va, ok := obj.(*storagev1.VolumeAttachment)
	if !ok {
		return errors.New("received an object that is not a VolumeAttachment")
	}
	if va.Spec.NodeName == h.nodeName {
		if _, err := h.checkVolumeAttachmentsExist(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (h *hook) checkVolumeAttachmentsExist(ctx context.Context) (bool, error) {
	attachments, err := h.client.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to get VolumeAttachment resources")
		return false, err
	}
	for _, at := range attachments.Items {
		if at.Spec.NodeName == h.nodeName {
			log.Info().
				Str("name", at.ObjectMeta.Name).
				Str("node_name", h.nodeName).
				Msg("VolumeAttachment resource has not been deleted yet")
			return true, fmt.Errorf("VolumeAttachment resource %q has not been deleted yet", at.ObjectMeta.Name)
		}
	}
	return false, nil
}
