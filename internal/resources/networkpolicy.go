package resources

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	paperclipv1alpha1 "github.com/paperclipinc/paperclip-operator/api/v1alpha1"
)

// BuildNetworkPolicy constructs the NetworkPolicy for a Instance.
const managedMode = "managed"

func BuildNetworkPolicy(instance *paperclipv1alpha1.Instance) *networkingv1.NetworkPolicy {
	port := servicePort(instance)
	dnsPort := intstr.FromInt32(53)

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(instance, NetworkPolicyName(instance)),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: SelectorLabels(instance),
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Port:     Ptr(intstr.FromInt32(port)),
							Protocol: Ptr(corev1.ProtocolTCP),
						},
					},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// Allow DNS
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Port:     &dnsPort,
							Protocol: Ptr(corev1.ProtocolUDP),
						},
						{
							Port:     &dnsPort,
							Protocol: Ptr(corev1.ProtocolTCP),
						},
					},
				},
				// Allow HTTPS outbound (for LLM API calls)
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Port:     Ptr(intstr.FromInt32(443)),
							Protocol: Ptr(corev1.ProtocolTCP),
						},
					},
				},
			},
		},
	}

	// Allow egress to K8s API server when cloud sandbox is enabled.
	// The server needs to create/manage sandbox pods via the K8s API.
	// An explicit rule is needed because some CNIs (k3s Flannel, Calico)
	// do not match host-network destinations with portOnly egress rules.
	if instance.Spec.Adapters.CloudSandbox != nil && instance.Spec.Adapters.CloudSandbox.Enabled {
		np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Port:     Ptr(intstr.FromInt32(6443)),
					Protocol: Ptr(corev1.ProtocolTCP),
				},
			},
		})
	}

	// Allow egress to managed database if applicable
	if instance.Spec.Database.Mode == managedMode || instance.Spec.Database.Mode == "" {
		np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: DatabaseSelectorLabels(instance),
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Port:     Ptr(intstr.FromInt32(PostgreSQLPort)),
					Protocol: Ptr(corev1.ProtocolTCP),
				},
			},
		})
	}

	// Allow egress to managed Redis if applicable
	if instance.Spec.Redis != nil && (instance.Spec.Redis.Mode == managedMode || instance.Spec.Redis.Mode == "") {
		np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: RedisSelectorLabels(instance),
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Port:     Ptr(intstr.FromInt32(RedisPort)),
					Protocol: Ptr(corev1.ProtocolTCP),
				},
			},
		})
	}

	// Custom ingress CIDRs
	for _, cidr := range instance.Spec.Security.NetworkPolicy.AllowIngressCIDRs {
		np.Spec.Ingress = append(np.Spec.Ingress, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{
					IPBlock: &networkingv1.IPBlock{
						CIDR: cidr,
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Port:     Ptr(intstr.FromInt32(port)),
					Protocol: Ptr(corev1.ProtocolTCP),
				},
			},
		})
	}

	// Custom egress CIDRs
	for _, cidr := range instance.Spec.Security.NetworkPolicy.AllowEgressCIDRs {
		np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					IPBlock: &networkingv1.IPBlock{
						CIDR: cidr,
					},
				},
			},
		})
	}

	return np
}

// BuildDatabaseNetworkPolicy constructs a NetworkPolicy restricting ingress to the managed database.
// Only allows traffic from Paperclip server pods on the PostgreSQL port.
func BuildDatabaseNetworkPolicy(instance *paperclipv1alpha1.Instance) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(instance, instance.Name+"-db"),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: DatabaseSelectorLabels(instance),
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: SelectorLabels(instance),
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Port:     Ptr(intstr.FromInt32(PostgreSQLPort)),
							Protocol: Ptr(corev1.ProtocolTCP),
						},
					},
				},
			},
			// Deny all egress - database does not need outbound access
			Egress: []networkingv1.NetworkPolicyEgressRule{},
		},
	}
}

// BuildRedisNetworkPolicy constructs a NetworkPolicy restricting ingress to the managed Redis.
// Only allows traffic from Paperclip server pods on the Redis port.
func BuildRedisNetworkPolicy(instance *paperclipv1alpha1.Instance) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(instance, instance.Name+"-redis"),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: RedisSelectorLabels(instance),
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: SelectorLabels(instance),
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Port:     Ptr(intstr.FromInt32(RedisPort)),
							Protocol: Ptr(corev1.ProtocolTCP),
						},
					},
				},
			},
			// Deny all egress - Redis does not need outbound access
			Egress: []networkingv1.NetworkPolicyEgressRule{},
		},
	}
}
