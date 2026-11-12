package resources

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	paperclipv1alpha1 "github.com/paperclipinc/paperclip-operator/api/v1alpha1"
)

// SandboxRoleName returns the name of the sandbox RBAC Role.
func SandboxRoleName(instance *paperclipv1alpha1.Instance) string {
	return instance.Name + "-sandbox"
}

// SandboxClusterRoleName returns the name of the sandbox ClusterRole.
func SandboxClusterRoleName(instance *paperclipv1alpha1.Instance) string {
	return instance.Namespace + "-" + instance.Name + "-sandbox"
}

// sandboxBaseRules returns the base policy rules for sandbox RBAC.
func sandboxBaseRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{rbacVerbCreate, "get", "list", "watch", rbacVerbDelete, "patch"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{rbacVerbCreate, "get"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"pods/log"},
			Verbs:     []string{"get"},
		},
		{
			APIGroups: []string{"networking.k8s.io"},
			Resources: []string{"networkpolicies"},
			Verbs:     []string{rbacVerbCreate, "get", "update", "patch"},
		},
	}
}

// BuildSandboxRole creates a Role granting permissions to manage sandbox pods.
func BuildSandboxRole(instance *paperclipv1alpha1.Instance, namespace string) *rbacv1.Role {
	rules := sandboxBaseRules()

	// Add PVC permissions when persistence is enabled
	if cs := instance.Spec.Adapters.CloudSandbox; cs != nil && cs.Persistence != nil && cs.Persistence.Enabled {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
			Verbs:     []string{rbacVerbCreate, "get", "list", rbacVerbDelete},
		})
	}

	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SandboxRoleName(instance),
			Namespace: namespace,
			Labels:    Labels(instance),
		},
		Rules: rules,
	}
}

// BuildSandboxClusterRole creates a ClusterRole with additional namespace
// permissions for multi-namespace sandbox isolation.
func BuildSandboxClusterRole(instance *paperclipv1alpha1.Instance) *rbacv1.ClusterRole {
	rules := sandboxBaseRules()

	// Add PVC permissions when persistence is enabled
	if cs := instance.Spec.Adapters.CloudSandbox; cs != nil && cs.Persistence != nil && cs.Persistence.Enabled {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
			Verbs:     []string{rbacVerbCreate, "get", "list", rbacVerbDelete},
		})
	}

	// Namespace management for multi-namespace isolation
	rules = append(rules,
		rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{rbacVerbCreate, "get", "list", rbacVerbDelete},
		},
		rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"services", "serviceaccounts"},
			Verbs:     []string{"get", "list", "watch", rbacVerbCreate, "update", "patch", rbacVerbDelete},
		},
		rbacv1.PolicyRule{
			APIGroups: []string{rbacAPIGroup},
			Resources: []string{"roles", "rolebindings"},
			Verbs:     []string{"get", "list", rbacVerbCreate, "update", "patch", rbacVerbDelete},
		},
	)

	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   SandboxClusterRoleName(instance),
			Labels: Labels(instance),
		},
		Rules: rules,
	}
}

// BuildSandboxClusterRoleBinding binds the sandbox ClusterRole to the instance ServiceAccount.
func BuildSandboxClusterRoleBinding(instance *paperclipv1alpha1.Instance) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   SandboxClusterRoleName(instance),
			Labels: Labels(instance),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacAPIGroup,
			Kind:     "ClusterRole",
			Name:     SandboxClusterRoleName(instance),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      ServiceAccountName(instance),
				Namespace: instance.Namespace,
			},
		},
	}
}

// BuildSandboxRoleBinding binds the sandbox Role to the instance ServiceAccount.
func BuildSandboxRoleBinding(instance *paperclipv1alpha1.Instance, namespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SandboxRoleName(instance),
			Namespace: namespace,
			Labels:    Labels(instance),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacAPIGroup,
			Kind:     "Role",
			Name:     SandboxRoleName(instance),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      ServiceAccountName(instance),
				Namespace: instance.Namespace,
			},
		},
	}
}
