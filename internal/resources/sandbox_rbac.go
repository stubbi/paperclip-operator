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

// BuildSandboxRole creates a Role granting permissions to manage sandbox pods.
func BuildSandboxRole(instance *paperclipv1alpha1.Instance, namespace string) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SandboxRoleName(instance),
			Namespace: namespace,
			Labels:    Labels(instance),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"create", "get", "list", "watch", "delete", "patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/exec"},
				Verbs:     []string{"create"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/log"},
				Verbs:     []string{"get"},
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
			APIGroup: "rbac.authorization.k8s.io",
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
