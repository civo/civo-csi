package e2e

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Test_OfflineResize tests that we can do an offline resize of a Volume
// 1. Create a Volume
// 2. Create a Deployment
// 3. Wait for the deployment to be ready
// 4. Increase the size of the volume
// 5. Scale the Deployment to 0
// 6. Scale the Deployment to 1
// 7. Wait for the deployment to be ready
// 8. Check the size of the new volume
func Test_OfflineResize(t *testing.T) {
	g := NewGomegaWithT(t)

	t.Log("Creating a PVC")
	pvc := pvcSpec()
	err := e2eTest.tenantClient.Create(context.TODO(), pvc)
	g.Expect(err).ShouldNot(HaveOccurred())

	defer e2eTest.cleanUp(pvc)

	t.Log("Creating a Deployment Using the PVC")
	deployment := deploymentSpec(pvc.Name)

	err = e2eTest.tenantClient.Create(context.TODO(), deployment)
	g.Expect(err).ShouldNot(HaveOccurred())

	defer e2eTest.cleanUp(deployment)

	t.Log("Wait for deployment to become ready")
	g.Eventually(deployStateFunc(context.TODO(), e2eTest.tenantClient, g, deployment), "3m", "5s").Should(Equal("ready"))

	err = e2eTest.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(pvc), pvc)
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Log("Check Size of Volume within Civo")
	pv := &v1.PersistentVolume{}
	err = e2eTest.tenantClient.Get(context.TODO(), client.ObjectKey{Name: pvc.Spec.VolumeName}, pv)
	g.Expect(err).ShouldNot(HaveOccurred())
	volID := string(pv.Spec.PersistentVolumeSource.CSI.VolumeHandle)
	vol, err := e2eTest.civo.GetVolume(volID)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(vol.SizeGigabytes).Should(Equal(10))

	t.Log("Resize PVC")
	e2eTest.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(pvc), pvc)
	pvc.Spec.Resources.Requests["storage"] = resource.MustParse("20Gi")
	err = e2eTest.tenantClient.Update(context.TODO(), pvc)
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Log("Scale Down Deployment")
	err = e2eTest.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(deployment), deployment)
	g.Expect(err).ShouldNot(HaveOccurred())

	replicas := int32(0)
	deployment.Spec.Replicas = &replicas

	err = e2eTest.tenantClient.Update(context.TODO(), deployment)
	g.Expect(err).ShouldNot(HaveOccurred())

	civoVolStatus := func() string {
		vol, _ := e2eTest.civo.GetVolume(volID)
		return vol.Status
	}
	t.Log("Wait for Volume to start resizing within Civo")
	g.Eventually(civoVolStatus, "10m", "2s").Should(Equal("resizing"))

	t.Log("Scale Up Deployment")
	err = e2eTest.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(deployment), deployment)
	g.Expect(err).ShouldNot(HaveOccurred())

	replicas = int32(1)
	deployment.Spec.Replicas = &replicas

	err = e2eTest.tenantClient.Update(context.TODO(), deployment)
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Log("Wait for Deployment to become ready")
	g.Eventually(deployStateFunc(context.TODO(), e2eTest.tenantClient, g, deployment), "3m", "5s").Should(Equal("ready"))

	t.Log("Confirm the volume has been resized")
	vol, err = e2eTest.civo.GetVolume(volID)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(vol.SizeGigabytes).Should(Equal(20))
}
