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

// Name is the name of the driver
const Name string = "Civo CSI Driver"

// Version is the current release of the driver
const Version string = "0.0.1"

// DefaultVolumeSizeGB is the default size in Gigabytes of an unspecified volume
const DefaultVolumeSizeGB int = 10

// SocketFilename is the location of the Unix domain socket for this driver
const SocketFilename string = "unix:///var/lib/kubelet/plugins/civo-csi/csi.sock"

// Driver implement the CSI endpoints for Identity, Node and Controller
type Driver struct {
	CivoClient     civogo.Clienter
	DiskHotPlugger DiskHotPlugger
	controller     bool
	SocketFilename string
	NodeInstanceID string
	Region         string
	TestMode       bool
	grpcServer     *grpc.Server
}

// NewDriver returns a CSI driver that implements gRPC endpoints for CSI
func NewDriver(apiURL, apiKey, region string) (*Driver, error) {
	client, err := civogo.NewClientWithURL(apiKey, apiURL, region)
	if err != nil {
		return nil, err
	}

	return &Driver{
		CivoClient:     client,
		Region:         region,
		DiskHotPlugger: &RealDiskHotPlugger{},
		controller:     (apiKey != ""),
		SocketFilename: SocketFilename,
		grpcServer:     &grpc.Server{},
	}, nil
}

// NewTestDriver returns a new Civo CSI driver specifically setup to call a fake Civo API
func NewTestDriver() (*Driver, error) {
	d, err := NewDriver("https://civo-api.example.com", "NO_API_KEY_NEEDED", "TEST1")
	d.SocketFilename = "unix:///tmp/civo-csi.sock"
	d.CivoClient, _ = civogo.NewFakeClient()
	d.DiskHotPlugger = &FakeDiskHotPlugger{}
	d.TestMode = true // Just stops so much logging out of failures, as they are often expected during the tests

	zerolog.SetGlobalLevel(zerolog.PanicLevel)

	return d, err
}

// Run the driver's gRPC server
func (d *Driver) Run(ctx context.Context) error {
	urlParts, _ := url.Parse(d.SocketFilename)

	grpcAddress := path.Join(urlParts.Host, filepath.FromSlash(urlParts.Path))
	if urlParts.Host == "" {
		grpcAddress = filepath.FromSlash(urlParts.Path)
	}

	// remove any existing left-over socket
	if err := os.Remove(grpcAddress); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unix domain socket file %s, error: %s", grpcAddress, err)
	}

	grpcListener, err := net.Listen(urlParts.Scheme, grpcAddress)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

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

	csi.RegisterIdentityServer(d.grpcServer, d)
	csi.RegisterControllerServer(d.grpcServer, d)
	csi.RegisterNodeServer(d.grpcServer, d)

	log.Info().Str("grpc_address", grpcAddress).Msg("starting server")

	var eg errgroup.Group

	eg.Go(func() error {
		go func() {
			<-ctx.Done()
			d.grpcServer.GracefulStop()
		}()
		return d.grpcServer.Serve(grpcListener)
	})

	return eg.Wait()
}
