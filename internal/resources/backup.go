package resources

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	paperclipv1alpha1 "github.com/paperclipinc/paperclip-operator/api/v1alpha1"
)

// BackupCronJobName returns the CronJob name for database backups.
func BackupCronJobName(instance *paperclipv1alpha1.Instance) string {
	return instance.Name + "-db-backup"
}

// BuildBackupCronJob constructs a CronJob that runs pg_dump and uploads to S3.
// The pod uses an init container (postgres image) to run pg_dump into a shared
// emptyDir volume, then the main container (aws-cli image) uploads the dump to
// S3 and prunes old backups beyond the retention window.
// Returns nil if backup is not configured or no S3 destination is available.
func BuildBackupCronJob(instance *paperclipv1alpha1.Instance) *batchv1.CronJob {
	if instance.Spec.Backup == nil || instance.Spec.Backup.Schedule == "" {
		return nil
	}

	backup := instance.Spec.Backup

	// Resolve S3 config: prefer explicit backup S3 spec, fall back to ObjectStorage.
	bucket, path, region, endpoint := resolveBackupS3(instance)
	if bucket == "" {
		// No S3 destination configured — cannot create backup job.
		return nil
	}

	retentionDays := int32(30)
	if backup.RetentionDays != nil {
		retentionDays = *backup.RetentionDays
	}

	// Upload script for the main container (aws-cli).
	// The init container has already written the dump to /backup/dump.sql.gz.
	uploadScript := fmt.Sprintf(`set -e

TIMESTAMP=$(date +%%Y%%m%%dT%%H%%M%%S)
S3_BUCKET="%s"
S3_PATH="%s"
S3_REGION="%s"
S3_ENDPOINT="%s"
RETENTION_DAYS=%d
DUMP_FILE="/backup/dump.sql.gz"

echo "Starting S3 upload at ${TIMESTAMP}..."

if [ ! -f "${DUMP_FILE}" ]; then
  echo "ERROR: dump file not found at ${DUMP_FILE}"
  exit 1
fi

# Build optional flags
ENDPOINT_FLAG=""
if [ -n "${S3_ENDPOINT}" ]; then
  ENDPOINT_FLAG="--endpoint-url ${S3_ENDPOINT}"
fi

REGION_FLAG=""
if [ -n "${S3_REGION}" ]; then
  REGION_FLAG="--region ${S3_REGION}"
fi

# Upload to S3
S3_KEY="${S3_PATH}/${TIMESTAMP}.sql.gz"
echo "Uploading to s3://${S3_BUCKET}/${S3_KEY}..."
aws s3 cp ${ENDPOINT_FLAG} ${REGION_FLAG} "${DUMP_FILE}" "s3://${S3_BUCKET}/${S3_KEY}"
echo "Upload complete."

# Prune old backups beyond retention window
if [ "${RETENTION_DAYS}" -gt 0 ]; then
  echo "Pruning backups older than ${RETENTION_DAYS} days..."
  CUTOFF_DATE=$(date -d "-${RETENTION_DAYS} days" +%%Y%%m%%dT%%H%%M%%S 2>/dev/null || date -v-${RETENTION_DAYS}d +%%Y%%m%%dT%%H%%M%%S 2>/dev/null || echo "")
  if [ -n "${CUTOFF_DATE}" ]; then
    aws s3 ls ${ENDPOINT_FLAG} ${REGION_FLAG} "s3://${S3_BUCKET}/${S3_PATH}/" | while read -r line; do
      FILE_NAME=$(echo "${line}" | awk '{print $4}')
      if [ -n "${FILE_NAME}" ] && [ "${FILE_NAME}" \< "${CUTOFF_DATE}" ]; then
        echo "Deleting old backup: ${FILE_NAME}"
        aws s3 rm ${ENDPOINT_FLAG} ${REGION_FLAG} "s3://${S3_BUCKET}/${S3_PATH}/${FILE_NAME}"
      fi
    done
  fi
fi

echo "Backup completed successfully."
`,
		sanitizeJSONString(bucket),
		sanitizeJSONString(path),
		sanitizeJSONString(region),
		sanitizeJSONString(endpoint),
		retentionDays,
	)

	labels := LabelsWithComponent(instance, "backup")

	backoffLimit := int32(1)
	successfulJobsHistoryLimit := int32(3)
	failedJobsHistoryLimit := int32(3)

	backupVolume := corev1.Volume{
		Name: "backup-scratch",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	backupVolumeMount := corev1.VolumeMount{
		Name:      "backup-scratch",
		MountPath: "/backup",
	}

	securityContext := &corev1.SecurityContext{
		AllowPrivilegeEscalation: Ptr(false),
		RunAsNonRoot:             Ptr(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackupCronJobName(instance),
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   backup.Schedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: &successfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     &failedJobsHistoryLimit,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: batchv1.JobSpec{
					BackoffLimit: &backoffLimit,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: labels,
						},
						Spec: corev1.PodSpec{
							RestartPolicy:                corev1.RestartPolicyOnFailure,
							AutomountServiceAccountToken: Ptr(false),
							NodeSelector:                 instance.Spec.Availability.NodeSelector,
							Tolerations:                  instance.Spec.Availability.Tolerations,
							SecurityContext: &corev1.PodSecurityContext{
								RunAsNonRoot: Ptr(true),
								RunAsUser:    Ptr(int64(1000)),
								RunAsGroup:   Ptr(int64(1000)),
								FSGroup:      Ptr(int64(1000)),
							},
							Volumes: []corev1.Volume{backupVolume},
							// Init container: run pg_dump from the postgres image
							InitContainers: []corev1.Container{
								{
									Name:            "pg-dump",
									Image:           pgDumpImage(instance),
									ImagePullPolicy: corev1.PullIfNotPresent,
									Command:         []string{"/bin/sh", "-c"},
									Args: []string{
										`set -e; echo "Running pg_dump..."; pg_dump "${DATABASE_URL}" | gzip > /backup/dump.sql.gz; echo "pg_dump complete."`,
									},
									SecurityContext: securityContext,
									Env:             buildBackupDBEnvVars(instance),
									VolumeMounts:    []corev1.VolumeMount{backupVolumeMount},
								},
							},
							// Main container: upload to S3 using aws-cli
							Containers: []corev1.Container{
								{
									Name:            "s3-upload",
									Image:           "amazon/aws-cli:2.22.35",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Command:         []string{"/bin/sh", "-c"},
									Args:            []string{uploadScript},
									SecurityContext: securityContext,
									Env:             buildBackupS3EnvVars(instance),
									VolumeMounts:    []corev1.VolumeMount{backupVolumeMount},
								},
							},
						},
					},
				},
			},
		},
	}
}

// resolveBackupS3 resolves the S3 configuration for backups.
// It prefers the explicit BackupS3Spec, falling back to ObjectStorageSpec.
func resolveBackupS3(instance *paperclipv1alpha1.Instance) (bucket, path, region, endpoint string) {
	backup := instance.Spec.Backup
	if backup.S3 != nil {
		bucket = backup.S3.Bucket
		path = backup.S3.Path
		region = backup.S3.Region
		endpoint = backup.S3.Endpoint
	} else if instance.Spec.ObjectStorage != nil {
		bucket = instance.Spec.ObjectStorage.Bucket
		region = instance.Spec.ObjectStorage.Region
		endpoint = instance.Spec.ObjectStorage.Endpoint
		path = "backups/" + instance.Name
	}
	if path == "" && bucket != "" {
		path = "backups/" + instance.Name
	}
	return
}

// resolveBackupCredentialsSecret returns the secret reference containing AWS credentials.
func resolveBackupCredentialsSecret(instance *paperclipv1alpha1.Instance) *corev1.LocalObjectReference {
	backup := instance.Spec.Backup
	if backup.S3 != nil && backup.S3.CredentialsSecretRef != nil {
		return backup.S3.CredentialsSecretRef
	}
	if instance.Spec.ObjectStorage != nil && instance.Spec.ObjectStorage.CredentialsSecretRef != nil {
		return instance.Spec.ObjectStorage.CredentialsSecretRef
	}
	return nil
}

// buildBackupDBEnvVars returns the DATABASE_URL env var for the pg_dump init container.
func buildBackupDBEnvVars(instance *paperclipv1alpha1.Instance) []corev1.EnvVar {
	var vars []corev1.EnvVar

	switch instance.Spec.Database.Mode {
	case ModeExternal:
		if instance.Spec.Database.ExternalURLSecretRef != nil {
			vars = append(vars, corev1.EnvVar{
				Name: "DATABASE_URL",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: instance.Spec.Database.ExternalURLSecretRef,
				},
			})
		} else if instance.Spec.Database.ExternalURL != "" {
			vars = append(vars, corev1.EnvVar{Name: "DATABASE_URL", Value: instance.Spec.Database.ExternalURL})
		}
	default: // managed
		// DB_PASSWORD must be defined before DATABASE_URL for $(DB_PASSWORD) substitution.
		vars = append(vars, corev1.EnvVar{
			Name: "DB_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: DatabaseSecretName(instance),
					},
					Key: "password",
				},
			},
		})
		vars = append(vars, corev1.EnvVar{
			Name: "DATABASE_URL",
			Value: fmt.Sprintf("postgresql://paperclip:$(DB_PASSWORD)@%s-db.%s.svc.cluster.local:%d/paperclip",
				instance.Name, instance.Namespace, PostgreSQLPort),
		})
	}

	return vars
}

// buildBackupS3EnvVars returns the AWS credential env vars for the S3 upload container.
func buildBackupS3EnvVars(instance *paperclipv1alpha1.Instance) []corev1.EnvVar {
	var vars []corev1.EnvVar

	credSecret := resolveBackupCredentialsSecret(instance)
	if credSecret != nil {
		vars = append(vars,
			corev1.EnvVar{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: *credSecret,
						Key:                  "AWS_ACCESS_KEY_ID",
					},
				},
			},
			corev1.EnvVar{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: *credSecret,
						Key:                  "AWS_SECRET_ACCESS_KEY",
					},
				},
			},
		)
	}

	return vars
}

// pgDumpImage returns the postgres image to use for pg_dump.
// Uses the same image as the managed database, or falls back to a default.
func pgDumpImage(instance *paperclipv1alpha1.Instance) string {
	if instance.Spec.Database.Mode == "managed" || instance.Spec.Database.Mode == "" {
		img := instance.Spec.Database.Managed.Image
		if img != "" {
			return img
		}
	}
	return "postgres:17-alpine"
}
