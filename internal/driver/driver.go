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
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

// Name is the name of the driver
const Name string = "Civo CSI Driver"

// Version is the current release of the driver
const Version string = "0.0.1"

// SocketFilename is the location of the Unix domain socket for this driver
const SocketFilename string = "unix:///var/lib/kubelet/plugins/civo-csi/csi.sock"

// Driver implement the CSI endpoints for Identity, Node and Controller
type Driver struct {
	civoClient *civogo.Client
	controller bool

	grpcServer *grpc.Server
	log        *logrus.Entry
}

// NewDriver returns a CSI driver that implements gRPC endpoints for CSI
func NewDriver(apiURL, apiKey, region string) (*Driver, error) {
	log := logrus.New().WithFields(logrus.Fields{
		"region":  region,
		"version": Version,
	})

	client, err := civogo.NewClientWithURL(apiKey, apiURL, region)
	if err != nil {
		return nil, err
	}

	return &Driver{
		civoClient: client,
		controller: (apiKey != ""),
		grpcServer: &grpc.Server{},
		log:        log,
	}, nil
}

// Run the driver's gRPC server
func (d *Driver) Run(ctx context.Context) error {
	urlParts, _ := url.Parse(SocketFilename)

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
			d.log.WithError(err).WithField("method", info.FullMethod).Error("method failed")
		}
		return resp, err
	}

	d.grpcServer = grpc.NewServer(grpc.UnaryInterceptor(errHandler))
	csi.RegisterIdentityServer(d.grpcServer, d)
	csi.RegisterControllerServer(d.grpcServer, d)
	csi.RegisterNodeServer(d.grpcServer, d)

	d.log.WithFields(logrus.Fields{
		"grpc_address": grpcAddress,
	}).Info("starting server")

	return d.grpcServer.Serve(grpcListener)
}
