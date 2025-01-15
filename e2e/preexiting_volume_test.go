package e2e

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/civo/civogo"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test_ExistingCivoVolume checks if a volume that already exists in a Civo account
// can be used as the source of a PVC
//
// 1. Create a Civo Volume
// 2. Create a PVC referencing the volume
// 3. Create a Deployment using the PVC
// 4. Check that the Deployment becomes ready
func Test_ExistingCivoVolume(t *testing.T) {
	t.Log("Create a Civo Volume")
	g := NewGomegaWithT(t)

	t.Logf("Cluster network: %s", e2eTest.cluster.NetworkID)
	res, err := e2eTest.civo.NewVolume(&civogo.VolumeConfig{
		Name:          "volume-test",
		NetworkID:     e2eTest.cluster.NetworkID,
		Region:        e2eTest.civo.Region,
		SizeGigabytes: 1,
	})
	t.Logf("New Vol res: %v", res)
	g.Expect(err).ShouldNot(HaveOccurred())
	// TODO: API returns an emptry string here
	// g.Expect(res.Result).Should(Equal("success"))
	g.Expect(res.ID).ShouldNot(BeEmpty())

	defer func() {
		t.Log("Clean up volume")
		e2eTest.civo.DeleteVolume(res.ID)
	}()

	// volume, err := e2eTest.civo.GetVolume(res.ID)

	pv := &corev1.PersistentVolume{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-volume",
		},
		Spec: corev1.PersistentVolumeSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Capacity: corev1.ResourceList{
				"storage": resource.MustParse("10Gi"),
			},
			StorageClassName: "civo-volume",
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "csi.civo.com",
					VolumeHandle: res.ID,
					FSType:       "ext4",
				},
			},
		},
	}

	t.Log("Create PV")
	err = e2eTest.tenantClient.Create(context.TODO(), pv)
	g.Expect(err).ShouldNot(HaveOccurred())
	defer func() {
		t.Log("Clean up PV")
		e2eTest.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(pv), pv)
		e2eTest.tenantClient.Delete(context.TODO(), pv)
	}()

	t.Log("Create PVC")
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-volume",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  pv.Name,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					"storage": resource.MustParse("10Gi"),
				},
			},
		},
	}
	err = e2eTest.tenantClient.Create(context.TODO(), pvc)
	g.Expect(err).ShouldNot(HaveOccurred())
	defer func() {
		t.Log("Clean up PVC")
		e2eTest.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(pvc), pvc)
		e2eTest.tenantClient.Delete(context.TODO(), pvc)
	}()

	t.Log("Creating a Deployment Using the PVC")
	deployment := deploymentSpec(pvc.Name)

	err = e2eTest.tenantClient.Create(context.TODO(), deployment)
	g.Expect(err).ShouldNot(HaveOccurred())

	defer func() {
		t.Log("Clean up Deployment")
		e2eTest.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(deployment), deployment)
		e2eTest.tenantClient.Delete(context.TODO(), deployment)
	}()

	t.Log("Wait for deployment to become ready")
	g.Eventually(deployStateFunc(context.TODO(), e2eTest.tenantClient, g, deployment), "3m", "5s").Should(Equal("ready"))

}
