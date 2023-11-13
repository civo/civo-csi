module github.com/civo/civo-csi

require (
	github.com/BurntSushi/toml v0.3.1
	github.com/civo/civogo v0.3.19
	github.com/container-storage-interface/spec v1.6.0
	github.com/joho/godotenv v1.4.0
	github.com/kubernetes-csi/csi-test/v4 v4.4.0
	github.com/onsi/gomega v1.19.0
	github.com/rs/zerolog v1.20.0
	github.com/stretchr/testify v1.8.3
	golang.org/x/sync v0.1.0
	golang.org/x/sys v0.7.0
	google.golang.org/grpc v1.56.3
	k8s.io/api v0.24.3
	k8s.io/apimachinery v0.24.3
	k8s.io/client-go v0.24.2
	k8s.io/mount-utils v0.24.3
	sigs.k8s.io/controller-runtime v0.12.3
)

go 1.15
