package driver

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/civo/civogo"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

// CSIVersion is the version of the csi to set in the User-Agent header
var CSIVersion = "dev"

// Name is the name of the driver
const Name string = "Civo CSI Driver"

// Version is the current release of the driver
const Version string = "0.0.1"

// DefaultVolumeSizeGB is the default size in Gigabytes of an unspecified volume
const DefaultVolumeSizeGB int = 10

// DefaultSocketFilename is the location of the Unix domain socket for this driver
const DefaultSocketFilename string = "unix:///var/lib/kubelet/plugins/civo-csi/csi.sock"

// Driver implement the CSI endpoints for Identity, Node and Controller
type Driver struct {
	CivoClient     civogo.Clienter
	DiskHotPlugger DiskHotPlugger
	controller     bool
	SocketFilename string
	NodeInstanceID string
	Region         string
	Namespace      string
	ClusterID      string
	TestMode       bool
	grpcServer     *grpc.Server
}

// NewDriver returns a CSI driver that implements gRPC endpoints for CSI
func NewDriver(apiURL, apiKey, region, namespace, clusterID string) (*Driver, error) {
	var client *civogo.Client
	var err error

	if apiKey != "" {
		client, err = civogo.NewClientWithURL(apiKey, apiURL, region)
		if err != nil {
			return nil, err
		}
	}

	userAgent := &civogo.Component{
		ID:      clusterID,
		Name:    "civo-csi",
		Version: Version,
	}

	client.SetUserAgent(userAgent)

	socketFilename := os.Getenv("CSI_ENDPOINT")
	if socketFilename == "" {
		socketFilename = DefaultSocketFilename
	}

	log.Info().Str("api_url", apiURL).Str("region", region).Str("namespace", namespace).Str("cluster_id", clusterID).Str("socketFilename", socketFilename).Str("user_agent", userAgent.Name).Msg("Created a new driver")

	return &Driver{
		CivoClient:     client,
		Region:         region,
		Namespace:      namespace,
		ClusterID:      clusterID,
		DiskHotPlugger: &RealDiskHotPlugger{},
		controller:     (apiKey != ""),
		SocketFilename: socketFilename,
		grpcServer:     &grpc.Server{},
	}, nil
}

// NewTestDriver returns a new Civo CSI driver specifically setup to call a fake Civo API
func NewTestDriver(fc *civogo.FakeClient) (*Driver, error) {
	d, err := NewDriver("https://civo-api.example.com", "NO_API_KEY_NEEDED", "TEST1", "default", "12345678")
	d.SocketFilename = "unix:///tmp/civo-csi.sock"
	if fc != nil {
		d.CivoClient = fc
	} else {
		d.CivoClient, _ = civogo.NewFakeClient()
	}

	d.DiskHotPlugger = &FakeDiskHotPlugger{}
	d.TestMode = true // Just stops so much logging out of failures, as they are often expected during the tests

	zerolog.SetGlobalLevel(zerolog.PanicLevel)

	return d, err
}

// Run the driver's gRPC server
func (d *Driver) Run(ctx context.Context) error {
	log.Debug().Str("socketFilename", d.SocketFilename).Msg("Parsing the socket filename to make a gRPC server")
	urlParts, _ := url.Parse(d.SocketFilename)
	log.Debug().Msg("Parsed socket filename")

	grpcAddress := path.Join(urlParts.Host, filepath.FromSlash(urlParts.Path))
	if urlParts.Host == "" {
		grpcAddress = filepath.FromSlash(urlParts.Path)
	}
	log.Debug().Msg("Generated gRPC address")

	// remove any existing left-over socket
	if err := os.Remove(grpcAddress); err != nil && !os.IsNotExist(err) {
		log.Error().Msgf("failed to remove unix domain socket file %s, error: %s", grpcAddress, err)
		return fmt.Errorf("failed to remove unix domain socket file %s, error: %s", grpcAddress, err)
	}
	log.Debug().Msg("Removed any exsting old socket")

	grpcListener, err := net.Listen(urlParts.Scheme, grpcAddress)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	log.Debug().Msg("Created gRPC listener")

	// log gRPC response errors for better observability
	errHandler := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			log.Err(err).Str("method", info.FullMethod).Msg("method failed")
		}
		return resp, err
	}

	if d.TestMode {
		d.grpcServer = grpc.NewServer()
	} else {
		d.grpcServer = grpc.NewServer(grpc.UnaryInterceptor(errHandler))
	}
	log.Debug().Msg("Created new RPC server")

	csi.RegisterIdentityServer(d.grpcServer, d)
	log.Debug().Msg("Registered Identity server")
	csi.RegisterControllerServer(d.grpcServer, d)
	log.Debug().Msg("Registered Controller server")
	csi.RegisterNodeServer(d.grpcServer, d)
	log.Debug().Msg("Registered Node server")

	log.Debug().Str("grpc_address", grpcAddress).Msg("Starting gRPC server")

	var eg errgroup.Group

	eg.Go(func() error {
		go func() {
			<-ctx.Done()
			log.Debug().Msg("Stopping gRPC because the context was cancelled")
			d.grpcServer.GracefulStop()
		}()
		log.Debug().Msg("Awaiting gRPC requests")
		return d.grpcServer.Serve(grpcListener)
	})

	log.Debug().Str("grpc_address", grpcAddress).Msg("Running gRPC server, waiting for a signal to quit the process...")

	return eg.Wait()
}
