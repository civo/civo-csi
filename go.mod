module github.com/civo/civo-csi

require (
	github.com/BurntSushi/toml v0.3.1
	github.com/civo/civogo v0.2.60
	github.com/container-storage-interface/spec v1.6.0
	github.com/joho/godotenv v1.4.0
	github.com/kubernetes-csi/csi-test/v4 v4.4.0
	github.com/onsi/gomega v1.19.0
	github.com/robertkrimen/otto v0.0.0-20191219234010-c382bd3c16ff // indirect
	github.com/rs/zerolog v1.20.0
	github.com/stretchr/testify v1.7.0
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	google.golang.org/grpc v1.47.0
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
	k8s.io/api v0.24.3
	k8s.io/apimachinery v0.24.3
	k8s.io/client-go v0.24.2
	k8s.io/klog v1.0.0 // indirect
	k8s.io/mount-utils v0.24.3 // indirect
	k8s.io/utils v0.0.0-20220210201930-3a6ce19ff2f9 // indirect
	sigs.k8s.io/controller-runtime v0.12.3
)

go 1.15
