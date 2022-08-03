package e2e

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Test_Basic tests the basic functionality of the CSI system:
// 1. Create a PVC
// 2. Create a Deployment with PVC Attached
// 3. Check that the pod is online
// 4. Cordon the node that the pod is running on
// 5. Delete the pod
// 6. Check that the pod is online
// 7. Delete the pod and pvc
// 8. Check that the pod and PVC have been deleted
func Test_Basic(t *testing.T) {
	g := NewGomegaWithT(t)

	t.Log("Creating a PVC")
	storageClassName := "civo-volume"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-volume",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"storage": resource.MustParse("10Gi"),
				},
			},
		},
	}
	err := e2eTest.tenantClient.Create(context.TODO(), pvc)
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

	t.Log("Check that the claim is bound")
	err = e2eTest.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(pvc), pvc)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(pvc.Status.Phase).Should(Equal(corev1.ClaimBound))

	t.Log("Cordoning the current deployment node")
	podList := &corev1.PodList{}
	l, _ := labels.Parse("app=volume-test")
	err = e2eTest.tenantClient.List(context.TODO(), podList, &client.ListOptions{LabelSelector: l})
	g.Expect(err).ShouldNot(HaveOccurred())
	pod := podList.Items[0]

	node := &corev1.Node{}
	err = e2eTest.tenantClient.Get(context.TODO(), client.ObjectKey{Name: pod.Spec.NodeName}, node)
	g.Expect(err).ShouldNot(HaveOccurred())

	node.Spec.Unschedulable = true
	err = e2eTest.tenantClient.Update(context.TODO(), node)
	g.Expect(err).ShouldNot(HaveOccurred())
	defer func() {
		t.Log("Removing cordon from node")
		err = e2eTest.tenantClient.Get(context.TODO(), client.ObjectKeyFromObject(node), node)
		g.Expect(err).ShouldNot(HaveOccurred())

		node.Spec.Unschedulable = false
		e2eTest.tenantClient.Update(context.TODO(), node)
	}()

	t.Log("Deleting the current pod")
	err = e2eTest.tenantClient.Delete(context.TODO(), &pod, &client.DeleteOptions{})
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Log("Wait for deployment to become ready")
	g.Eventually(deployStateFunc(context.TODO(), e2eTest.tenantClient, g, deployment), "3m", "5s").Should(Equal("ready"))

}

func deployStateFunc(ctx context.Context, c client.Client, g *WithT, deployment *appsv1.Deployment) func() string {
	return func() string {
		err := c.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
		if errors.IsNotFound(err) {
			return "pending"
		}

		g.Expect(err).ShouldNot(HaveOccurred())
		if deployment.Status.AvailableReplicas == *deployment.Spec.Replicas {
			return "ready"
		}
		return "pending"
	}
}
