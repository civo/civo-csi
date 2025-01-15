package hook

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Hook defines the lifecycle methods for a hook, such as PreStop, PostStart, etc.
// Implementations of this interface can define actions to be performed at different lifecycle stages.
type Hook interface {
	PreStop(ctx context.Context) error
}

type hook struct {
	client        kubernetes.Interface
	nodeName      string
	clientCfgPath string
}

// NewHook creates a new Hook with the provided options. It returns an error if setup fails.
func NewHook(opts ...Option) (Hook, error) {
	h := &hook{}
	for _, opt := range append(defaultOpts, opts...) {
		opt(h)
	}
	if h.nodeName == "" {
		return nil, errors.New("node name not found")
	}
	if err := h.setupKubernetesClient(); err != nil {
		return nil, fmt.Errorf("failed to setup kubernetes API client: %w", err)
	}
	return h, nil
}

// setupKubernetesClient creates Kubernetes client based on the kubeconfig path.
// If kubeconfig path is not empty, the client will be created using that path.
// Otherwise, if the kubeconfig path is empty, the client will be created using the in-clustetr config.
func (h *hook) setupKubernetesClient() (err error) {
	if h.clientCfgPath != "" && h.client == nil {
		cfg, err := clientcmd.BuildConfigFromFlags("", h.clientCfgPath)
		if err != nil {
			return fmt.Errorf("failed to build kubeconfig from path %q: %w", h.clientCfgPath, err)
		}
		h.client, err = kubernetes.NewForConfig(cfg)
		if err != nil {
			return fmt.Errorf("failed to create kubernetes API client: %w", err)
		}
		return nil
	}

	if h.client == nil {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to load in-cluster kubeconfig: %w", err)
		}
		h.client, err = kubernetes.NewForConfig(cfg)
		if err != nil {
			return fmt.Errorf("failed to create kubernetes API client: %w", err)
		}
	}
	return nil
}
