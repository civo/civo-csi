package e2e

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/civo/civogo"
	"github.com/joho/godotenv"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const Image = "dmajrekar/civo-csi"
const TestClusterName = "csi-e2e-test"

var CivoRegion, CivoURL string

var e2eTest *E2ETest

var retainClusters bool

type E2ETest struct {
	civo         *civogo.Client
	cluster      *civogo.KubernetesCluster
	tenantClient client.Client
}

func init() {
	flag.BoolVar(&retainClusters, "retain", false, "Retain the created cluster(s) on failure. (Clusters are always cleaned up on success.) Ignored if -kubeconfig is specified.")
}
func TestMain(m *testing.M) {
	flag.Parse()

	fmt.Println("Starting E2E tests")

	e2eTest = &E2ETest{}

	// Recover from a panic
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(r)
		}
		// Ensure that we clean up the cluster after test tests have run
		e2eTest.cleanUpCluster()
	}()

	// Recover from a SIGINT
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		e2eTest.cleanUpCluster()
	}()

	// Load .env from the project root
	godotenv.Load("../.env")

	// Provision a new cluster
	e2eTest.provisionCluster()
	defer e2eTest.cleanUpCluster()

	// 2. Wait for the cluster to be provisioned
	retry(30, 10*time.Second, func() error {
		cluster, err := e2eTest.civo.GetKubernetesCluster(e2eTest.cluster.ID)
		if err != nil {
			return err
		}
		if cluster.Status != "ACTIVE" {
			return fmt.Errorf("Cluster is not available yet: %s", cluster.Status)
		}
		return nil
	})

	fmt.Println("Cluster is now ACTIVE")

	var err error
	e2eTest.cluster, err = e2eTest.civo.GetKubernetesCluster(e2eTest.cluster.ID)
	if err != nil {
		log.Panicf("Unable to fetch ACTIVE Cluster: %s", err.Error())
	}
	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(e2eTest.cluster.KubeConfig))
	if err != nil {
		log.Panic(err)
	}

	// Connect to the cluster
	cl, err := client.New(config, client.Options{})
	if err != nil {
		log.Panic(err)
	}
	e2eTest.tenantClient = cl

	// Update CSI Controller Image and wait for ready
	controller := &v1.StatefulSet{}

	err = e2eTest.tenantClient.Get(context.TODO(), client.ObjectKey{Namespace: "kube-system", Name: "civo-csi-controller"}, controller)
	if err != nil {
		log.Panic(err)
	}

	controller.Spec.Template.Spec.Containers[3].Image = Image
	err = e2eTest.tenantClient.Update(context.TODO(), controller)
	if err != nil {
		log.Panic(err)
	}

	// Update DS and wait for ready
	csiDS := &v1.DaemonSet{}

	err = e2eTest.tenantClient.Get(context.TODO(), client.ObjectKey{Namespace: "kube-system", Name: "civo-csi-node"}, csiDS)
	if err != nil {
		log.Panic(err)
	}

	err = e2eTest.tenantClient.Update(context.TODO(), csiDS)
	if err != nil {
		log.Panic(err)
	}

	exitCode := m.Run()

	// Clean up cluster
	e2eTest.cleanUpCluster()

	os.Exit(exitCode)

}

func (e *E2ETest) provisionCluster() {
	APIKey := os.Getenv("CIVO_API_KEY")
	if APIKey == "" {
		log.Panic("CIVO_API_KEY env variable not provided")
	}

	CivoRegion = os.Getenv("CIVO_REGION")
	if CivoRegion == "" {
		CivoRegion = "FRA1"
	}
	CivoURL := os.Getenv("CIVO_URL")
	if CivoURL == "" {
		CivoURL = "https://api.civo.com"
	}

	var err error
	e.civo, err = civogo.NewClientWithURL(APIKey, CivoURL, CivoRegion)
	if err != nil {
		log.Panicf("Unable to initialise Civo Client: %s", err.Error())
	}

	// List Clusters
	list, err := e.civo.ListKubernetesClusters()
	if err != nil {
		log.Panicf("Unable to list Clusters: %s", err.Error())
	}
	for _, cluster := range list.Items {
		if cluster.Name == TestClusterName {
			e.cluster = &cluster
			return
		}
	}

	// List Networks
	network, err := e.civo.GetDefaultNetwork()
	if err != nil {
		log.Panicf("Unable to get Default Network: %s", err.Error())
	}

	clusterConfig := &civogo.KubernetesClusterConfig{
		Name:      TestClusterName,
		Region:    CivoRegion,
		NetworkID: network.ID,
		Pools: []civogo.KubernetesClusterPoolConfig{
			{
				Count: 2,
				Size:  "g4s.kube.small",
			},
		},
	}

	e.cluster, err = e.civo.NewKubernetesClusters(clusterConfig)
	if err != nil {
		log.Panicf("Unable to provision new cluster: %s", err.Error())
	}
}

func (e *E2ETest) cleanUpCluster() {
	if retainClusters {
		return
	}
	fmt.Println("Attempting Test Cleanup")
	if e.cluster != nil {
		fmt.Printf("Deleting Cluster: %s\n", e.cluster.ID)
		e.civo.DeleteKubernetesCluster(e.cluster.ID)
	}
}

func retry(attempts int, sleep time.Duration, f func() error) (err error) {
	for i := 0; i < attempts; i++ {
		if i > 0 {
			log.Println("retrying after error:", err)
			time.Sleep(sleep)
			sleep *= 2
		}
		err = f()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("after %d attempts, last error: %s", attempts, err)
}

func (e *E2ETest) cleanUp(obj client.Object) {
	e.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(obj), obj)
	e.tenantClient.Delete(context.TODO(), obj)
}
