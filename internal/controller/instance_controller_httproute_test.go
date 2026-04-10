package controller

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	paperclipv1alpha1 "github.com/paperclipinc/paperclip-operator/api/v1alpha1"
)

func TestReconcileHTTPRouteLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)

	instance := &paperclipv1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "default",
		},
		Spec: paperclipv1alpha1.InstanceSpec{
			Networking: paperclipv1alpha1.NetworkingSpec{
				Service: paperclipv1alpha1.ServiceSpec{
					Port: 3100,
				},
				HTTPRoute: &paperclipv1alpha1.HTTPRouteSpec{
					Enabled: true,
					ParentRefs: []paperclipv1alpha1.HTTPRouteParentRef{
						{
							Name:      "external-https",
							Namespace: stringPtr("infra"),
						},
					},
					Hostnames: []string{"paperclip.example.com"},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	reconciler := &InstanceReconciler{Client: fakeClient, Scheme: scheme}

	if err := reconciler.reconcileHTTPRoute(context.Background(), instance); err != nil {
		t.Fatalf("reconcileHTTPRoute(create) returned error: %v", err)
	}

	route := &gatewayv1.HTTPRoute{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "example", Namespace: "default"}, route); err != nil {
		t.Fatalf("expected HTTPRoute to be created: %v", err)
	}
	if len(route.OwnerReferences) != 1 || route.OwnerReferences[0].Name != "example" {
		t.Fatalf("expected HTTPRoute to have Instance owner reference, got %+v", route.OwnerReferences)
	}
	if instance.Status.ManagedResources.HTTPRoute != "example" {
		t.Fatalf("expected status managed HTTPRoute to be recorded, got %q", instance.Status.ManagedResources.HTTPRoute)
	}

	instance.Spec.Networking.HTTPRoute = nil
	if err := reconciler.reconcileHTTPRoute(context.Background(), instance); err != nil {
		t.Fatalf("reconcileHTTPRoute(delete) returned error: %v", err)
	}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "example", Namespace: "default"}, route)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected HTTPRoute to be deleted, got err=%v", err)
	}
	if instance.Status.ManagedResources.HTTPRoute != "" {
		t.Fatalf("expected managed HTTPRoute status to be cleared, got %q", instance.Status.ManagedResources.HTTPRoute)
	}
}

func TestReconcileIngressDeletesWhenDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)

	ingressClass := "nginx"
	instance := &paperclipv1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "default",
		},
		Spec: paperclipv1alpha1.InstanceSpec{
			Networking: paperclipv1alpha1.NetworkingSpec{
				Ingress: &paperclipv1alpha1.IngressSpec{
					Enabled:          true,
					IngressClassName: &ingressClass,
					Hosts:            []string{"paperclip.example.com"},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	reconciler := &InstanceReconciler{Client: fakeClient, Scheme: scheme}

	if err := reconciler.reconcileIngress(context.Background(), instance); err != nil {
		t.Fatalf("reconcileIngress(create) returned error: %v", err)
	}

	ingress := &networkingv1.Ingress{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "example", Namespace: "default"}, ingress); err != nil {
		t.Fatalf("expected Ingress to be created: %v", err)
	}

	instance.Spec.Networking.Ingress = nil
	if err := reconciler.reconcileIngress(context.Background(), instance); err != nil {
		t.Fatalf("reconcileIngress(delete) returned error: %v", err)
	}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "example", Namespace: "default"}, ingress)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected Ingress to be deleted, got err=%v", err)
	}
	if instance.Status.ManagedResources.Ingress != "" {
		t.Fatalf("expected managed Ingress status to be cleared, got %q", instance.Status.ManagedResources.Ingress)
	}
}

func mustAddToScheme(t *testing.T, scheme *runtime.Scheme) {
	t.Helper()

	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway-api scheme: %v", err)
	}
	if err := paperclipv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add paperclip scheme: %v", err)
	}
}

func stringPtr(v string) *string {
	return &v
}
