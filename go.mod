module github.com/civo/civo-csi

require (
	github.com/BurntSushi/toml v0.3.1
	github.com/civo/civogo v0.2.60
	github.com/container-storage-interface/spec v1.3.0
	github.com/google/uuid v1.2.0 // indirect
	github.com/joho/godotenv v1.4.0
	github.com/kubernetes-csi/csi-test/v4 v4.0.2
	github.com/rs/zerolog v1.20.0
	github.com/stretchr/testify v1.7.0
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	google.golang.org/grpc v1.40.0
	k8s.io/api v0.24.3 // indirect
	k8s.io/apimachinery v0.24.3 // indirect
	k8s.io/client-go v0.24.2
	sigs.k8s.io/controller-runtime v0.12.3
)

go 1.15
