package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"

	paperclipv1alpha1 "github.com/paperclipinc/paperclip-operator/api/v1alpha1"
)

func netpolTestInstance() *paperclipv1alpha1.Instance {
	return &paperclipv1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: "np", Namespace: "np-ns"},
	}
}

func TestNetworkPolicyExtraEgress(t *testing.T) {
	inst := netpolTestInstance()
	inst.Spec.Security.NetworkPolicy.ExtraEgress = []networkingv1.NetworkPolicyEgressRule{
		{To: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "minio"}}}}},
	}
	np := BuildNetworkPolicy(inst)
	last := np.Spec.Egress[len(np.Spec.Egress)-1]
	if len(last.To) != 1 || last.To[0].PodSelector == nil || last.To[0].PodSelector.MatchLabels["app"] != "minio" {
		t.Fatalf("extraEgress rule not appended verbatim: %+v", last)
	}
}

func TestNetworkPolicyEnabledPointerSemantics(t *testing.T) {
	inst := netpolTestInstance()
	if !NetworkPolicyEnabled(inst) {
		t.Fatal("nil Enabled must default to true")
	}
	f := false
	inst.Spec.Security.NetworkPolicy.Enabled = &f
	if NetworkPolicyEnabled(inst) {
		t.Fatal("explicit false must disable")
	}
}

func TestObjectStorageForcePathStyle(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		explicit *bool
		want     bool
	}{
		{name: "minio defaults to path style", provider: "minio", want: true},
		{name: "s3 defaults to virtual-hosted", provider: "s3", want: false},
		{name: "explicit false wins over minio default", provider: "minio", explicit: boolPtr(false), want: false},
		{name: "explicit true wins for s3", provider: "s3", explicit: boolPtr(true), want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := netpolTestInstance()
			inst.Spec.ObjectStorage = &paperclipv1alpha1.ObjectStorageSpec{
				Provider:       tc.provider,
				Bucket:         "b",
				ForcePathStyle: tc.explicit,
			}
			tmpl := BuildServerPodTemplate(inst, nil)
			got := false
			for _, env := range tmpl.Spec.Containers[0].Env {
				if env.Name == "PAPERCLIP_STORAGE_S3_FORCE_PATH_STYLE" && env.Value == "true" {
					got = true
				}
			}
			if got != tc.want {
				t.Fatalf("forcePathStyle env: got %v want %v", got, tc.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

// Regression for #138: the Jobs connect directly to PostgreSQL with their own
// component labels, so admitting only the server leaves them unable to connect.
func TestDatabaseNetworkPolicyDatabaseClients(t *testing.T) {
	for _, mode := range []string{"managed", ""} {
		t.Run("mode="+mode, func(t *testing.T) {
			inst := netpolTestInstance()
			inst.Spec.Database.Mode = mode
			inst.Spec.Auth.AdminUser = &paperclipv1alpha1.AdminUserSpec{
				Email: "admin@example.com",
				PasswordSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "admin-password"},
					Key:                  "password",
				},
			}
			inst.Spec.Backup = &paperclipv1alpha1.BackupSpec{
				Schedule: "0 2 * * *",
				S3:       &paperclipv1alpha1.BackupS3Spec{Bucket: "backups"},
			}
			bootstrap := BuildBootstrapJob(inst)
			backup := BuildBackupCronJob(inst)
			if bootstrap == nil || backup == nil {
				t.Fatal("expected configured bootstrap and backup resources")
			}
			policy := BuildDatabaseNetworkPolicy(inst)
			clients := []struct {
				name      string
				podLabels map[string]string
			}{
				{"server", BuildServerPodTemplate(inst, nil).Labels},
				{"bootstrap", bootstrap.Spec.Template.Labels},
				{"backup", backup.Spec.JobTemplate.Spec.Template.Labels},
			}
			for _, client := range clients {
				t.Run(client.name, func(t *testing.T) {
					if !databasePolicyAllowsPod(t, policy, inst.Namespace, client.podLabels, 5432, corev1.ProtocolTCP) {
						t.Error("database policy denies PostgreSQL traffic from generated pod labels")
					}
					otherInstance := make(map[string]string, len(client.podLabels))
					for key, value := range client.podLabels {
						otherInstance[key] = value
					}
					otherInstance["app.kubernetes.io/instance"] = "another-instance"
					if databasePolicyAllowsPod(t, policy, inst.Namespace, otherInstance, 5432, corev1.ProtocolTCP) {
						t.Error("database policy admits another instance")
					}
					if databasePolicyAllowsPod(t, policy, "another-namespace", client.podLabels, 5432, corev1.ProtocolTCP) {
						t.Error("database policy admits another namespace")
					}
					if databasePolicyAllowsPod(t, policy, inst.Namespace, client.podLabels, 5433, corev1.ProtocolTCP) {
						t.Error("database policy admits another TCP port")
					}
					if databasePolicyAllowsPod(t, policy, inst.Namespace, client.podLabels, 5432, corev1.ProtocolUDP) {
						t.Error("database policy admits UDP")
					}
				})
			}
			for _, component := range []string{"database", "sandbox", "unrelated"} {
				podLabels := LabelsWithComponent(inst, component)
				if databasePolicyAllowsPod(t, policy, inst.Namespace, podLabels, 5432, corev1.ProtocolTCP) {
					t.Errorf("database policy admits unrelated component %q", component)
				}
			}
			if len(policy.Spec.Egress) != 0 {
				t.Error("database policy must continue to deny outbound connections")
			}
			if len(policy.Spec.PolicyTypes) != 2 || policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress || policy.Spec.PolicyTypes[1] != networkingv1.PolicyTypeEgress {
				t.Error("database policy must retain ingress and egress isolation")
			}
			selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
			if err != nil {
				t.Fatal(err)
			}
			if !selector.Matches(labels.Set(LabelsWithComponent(inst, "database"))) || selector.Matches(labels.Set(LabelsWithComponent(inst, "server"))) {
				t.Error("database policy must select only database pods")
			}
		})
	}
}

// Evaluate the pod-to-pod ingress rules emitted for the database. Reject broad
// namespace or IP peers explicitly: this policy must stay within its namespace.
func databasePolicyAllowsPod(t *testing.T, policy *networkingv1.NetworkPolicy, namespace string, podLabels map[string]string, port int32, protocol corev1.Protocol) bool {
	t.Helper()
	allowed := false
	for _, rule := range policy.Spec.Ingress {
		if len(rule.Ports) == 0 || len(rule.From) == 0 {
			t.Fatal("database ingress must constrain both peers and ports")
		}
		portMatches := false
		for _, policyPort := range rule.Ports {
			if policyPort.Port == nil || policyPort.Port.Type != intstr.Int || policyPort.Port.IntVal != 5432 || policyPort.EndPort != nil || policyPort.Protocol == nil || *policyPort.Protocol != corev1.ProtocolTCP {
				t.Fatal("database ingress must allow only TCP 5432")
			}
			if policyPort.Port.IntVal == port && *policyPort.Protocol == protocol {
				portMatches = true
			}
		}
		for _, peer := range rule.From {
			if peer.PodSelector == nil || peer.NamespaceSelector != nil || peer.IPBlock != nil {
				t.Fatal("database ingress must use pod selectors scoped to the policy namespace")
			}
			selector, err := metav1.LabelSelectorAsSelector(peer.PodSelector)
			if err != nil {
				t.Fatal(err)
			}
			if namespace == policy.Namespace && selector.Matches(labels.Set(podLabels)) && portMatches {
				allowed = true
			}
		}
	}
	return allowed
}
