package hooks

import (
	"os"

	"k8s.io/client-go/kubernetes"
)

type Option func(*hook)

var defaultOpts = []Option{
	WithNodeName(os.Getenv("KUBE_NODE_NAME")),
}

// WithKubernetesClient returns Option to set Kubernetes API client.
func WithKubernetesClient(client kubernetes.Interface) Option {
	return func(h *hook) {
		if client != nil {
			h.client = client
		}
	}
}

func WithKubernetesClientConfigPath(path string) Option {
	return func(h *hook) {
		if path != "" {
			h.clientCfgPath = path
		}
	}
}

// WithNodeName returns Option to set node node name.
func WithNodeName(name string) Option {
	return func(h *hook) {
		if name != "" {
			h.nodeName = name
		}
	}
}
