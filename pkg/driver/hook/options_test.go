package hook

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes"
)

func TestWithKubernetesClient(t *testing.T) {
	type test struct {
		name       string
		client     kubernetes.Interface
		beforeFunc func(*hook)
		wantClient kubernetes.Interface
	}

	tests := []test{
		{
			name:       "Succeeds to apply option",
			client:     &kubernetes.Clientset{},
			wantClient: &kubernetes.Clientset{},
		},
		{
			name: "Does nothing when client is nil",
			beforeFunc: func(h *hook) {
				h.client = &kubernetes.Clientset{}
			},
			wantClient: &kubernetes.Clientset{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			h := &hook{}

			if test.beforeFunc != nil {
				test.beforeFunc(h)
			}

			WithKubernetesClient(test.client)(h)

			assert.Equal(tt, test.wantClient, h.client)
		})
	}
}

func TestWithKubernetesClientConfigPath(t *testing.T) {
	type test struct {
		name       string
		path       string
		beforeFunc func(*hook)
		wantPath   string
	}

	tests := []test{
		{
			name:     "Succeeds to apply option",
			path:     "kubeconfig.yaml",
			wantPath: "kubeconfig.yaml",
		},
		{
			name: "Do nothing when path is empty",
			beforeFunc: func(h *hook) {
				h.clientCfgPath = "kubeconfig.yaml"
			},
			wantPath: "kubeconfig.yaml",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			h := &hook{}

			if test.beforeFunc != nil {
				test.beforeFunc(h)
			}

			WithKubernetesClientConfigPath(test.path)(h)

			assert.Equal(tt, test.wantPath, h.clientCfgPath)
		})
	}
}

func TestWithNodeName(t *testing.T) {
	type test struct {
		name         string
		nodeName     string
		beforeFunc   func(*hook)
		wantNodeName string
	}

	tests := []test{
		{
			name:         "Succeeds to apply option",
			nodeName:     "node-01",
			wantNodeName: "node-01",
		},
		{
			name: "Do nothing when Node name is empty",
			beforeFunc: func(h *hook) {
				h.nodeName = "node-01"
			},
			wantNodeName: "node-01",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			h := &hook{}

			if test.beforeFunc != nil {
				test.beforeFunc(h)
			}

			WithNodeName(test.nodeName)(h)

			assert.Equal(tt, test.wantNodeName, h.nodeName)
		})
	}
}
