package resources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	paperclipv1alpha1 "github.com/paperclipinc/paperclip-operator/api/v1alpha1"
)

const (
	// RedisContainerName is the name of the Redis container.
	RedisContainerName = "redis"
	// RedisPort is the default Redis port.
	RedisPort int32 = 6379
	// RedisVolumeName is the name of the Redis data volume.
	RedisVolumeName = "redis-data"
	// RedisMountPath is the mount path for the Redis data volume.
	RedisMountPath = "/data"
)

// RedisStatefulSetName returns the Redis StatefulSet name.
func RedisStatefulSetName(instance *paperclipv1alpha1.Instance) string {
	return instance.Name + "-redis"
}

// RedisServiceName returns the Redis Service name.
func RedisServiceName(instance *paperclipv1alpha1.Instance) string {
	return instance.Name + "-redis"
}

// RedisPVCName returns the Redis PVC name.
func RedisPVCName(instance *paperclipv1alpha1.Instance) string {
	return instance.Name + "-redis-data"
}

// RedisSelectorLabels returns the labels used for the Redis pod selector.
func RedisSelectorLabels(instance *paperclipv1alpha1.Instance) map[string]string {
	return map[string]string{
		LabelApp:       AppName,
		LabelInstance:  instance.Name,
		LabelComponent: "redis",
	}
}

// BuildRedisStatefulSet constructs the Redis StatefulSet for managed mode.
func BuildRedisStatefulSet(instance *paperclipv1alpha1.Instance) *appsv1.StatefulSet {
	labels := LabelsWithComponent(instance, "redis")
	selectorLabels := RedisSelectorLabels(instance)

	managed := instance.Spec.Redis.Managed
	image := managed.Image
	if image == "" {
		image = "redis:7-alpine"
	}

	storageSize := managed.StorageSize
	if storageSize.IsZero() {
		storageSize = resource.MustParse("1Gi")
	}

	maxMemory := redisMaxMemory(instance)

	container := corev1.Container{
		Name:    RedisContainerName,
		Image:   image,
		Command: []string{"redis-server", "--appendonly", "yes", "--maxmemory", maxMemory, "--maxmemory-policy", "allkeys-lru"},
		Ports: []corev1.ContainerPort{
			{
				Name:          "redis",
				ContainerPort: RedisPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Resources:                redisResources(instance),
		ImagePullPolicy:          corev1.PullIfNotPresent,
		TerminationMessagePath:   "/dev/termination-log",
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      RedisVolumeName,
				MountPath: RedisMountPath,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"redis-cli", "ping"},
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			FailureThreshold:    3,
			SuccessThreshold:    1,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"redis-cli", "ping"},
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
			TimeoutSeconds:      3,
			FailureThreshold:    3,
			SuccessThreshold:    1,
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: Ptr(false),
			RunAsNonRoot:             Ptr(true),
			RunAsUser:                Ptr(int64(999)), // redis user
			RunAsGroup:               Ptr(int64(999)),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	replicas := int32(1)

	sts := &appsv1.StatefulSet{
		ObjectMeta: ObjectMeta(instance, RedisStatefulSetName(instance)),
		Spec: appsv1.StatefulSetSpec{
			Replicas:             &replicas,
			ServiceName:          RedisServiceName(instance),
			RevisionHistoryLimit: Ptr(int32(10)),
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers:                    []corev1.Container{container},
					RestartPolicy:                 corev1.RestartPolicyAlways,
					AutomountServiceAccountToken:  Ptr(false),
					DNSPolicy:                     corev1.DNSClusterFirst,
					SchedulerName:                 "default-scheduler",
					TerminationGracePeriodSeconds: Ptr(int64(10)),
					NodeSelector:                  instance.Spec.Availability.NodeSelector,
					Tolerations:                   instance.Spec.Availability.Tolerations,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: Ptr(true),
						RunAsUser:    Ptr(int64(999)),
						RunAsGroup:   Ptr(int64(999)),
						FSGroup:      Ptr(int64(999)),
					},
					Volumes: []corev1.Volume{
						{
							Name: RedisVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: RedisPVCName(instance),
								},
							},
						},
					},
				},
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
		},
	}

	sts.Labels = labels
	return sts
}

func redisResources(instance *paperclipv1alpha1.Instance) corev1.ResourceRequirements {
	if instance.Spec.Redis == nil {
		return defaultRedisResources()
	}
	r := instance.Spec.Redis.Managed.Resources
	if len(r.Requests) == 0 && len(r.Limits) == 0 {
		return defaultRedisResources()
	}
	return r
}

func defaultRedisResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
}

// redisMaxMemory returns the Redis maxmemory value derived from the container memory limit.
// Uses 75% of the memory limit to leave headroom for Redis overhead (connections, buffers).
func redisMaxMemory(instance *paperclipv1alpha1.Instance) string {
	res := redisResources(instance)
	if memLimit, ok := res.Limits[corev1.ResourceMemory]; ok {
		bytes := memLimit.Value()
		mb := (bytes * 3 / 4) / (1024 * 1024)
		if mb < 1 {
			mb = 1
		}
		return fmt.Sprintf("%dmb", mb)
	}
	return "256mb"
}

// RedisURL returns the Redis connection URL for the managed instance.
func RedisURL(instance *paperclipv1alpha1.Instance) string {
	return "redis://" + RedisServiceName(instance) + "." + instance.Namespace + ".svc.cluster.local:6379"
}

// BuildRedisPVC constructs the PVC for the managed Redis instance.
func BuildRedisPVC(instance *paperclipv1alpha1.Instance) *corev1.PersistentVolumeClaim {
	size := instance.Spec.Redis.Managed.StorageSize
	if size.IsZero() {
		size = resource.MustParse("1Gi")
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: ObjectMeta(instance, RedisPVCName(instance)),
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: size,
				},
			},
		},
	}

	if instance.Spec.Redis.Managed.StorageClass != nil {
		pvc.Spec.StorageClassName = instance.Spec.Redis.Managed.StorageClass
	}

	pvc.Labels = LabelsWithComponent(instance, "redis")
	return pvc
}

// BuildRedisService constructs the Service for the managed Redis instance.
func BuildRedisService(instance *paperclipv1alpha1.Instance) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: ObjectMeta(instance, RedisServiceName(instance)),
		Spec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			Selector:        RedisSelectorLabels(instance),
			SessionAffinity: corev1.ServiceAffinityNone,
			Ports: []corev1.ServicePort{
				{
					Name:     "redis",
					Port:     RedisPort,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}
}
