package snapshot

import (
	"context"
	"crypto/md5"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	kotsadmtypes "github.com/replicatedhq/kots/pkg/kotsadm/types"
	kotsadmversion "github.com/replicatedhq/kots/pkg/kotsadm/version"
	"github.com/replicatedhq/kots/pkg/kotsutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kuberneteserrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
	k8syaml "sigs.k8s.io/yaml"
)

const (
	NFSMinioPVName, NFSMinioPVCName                                 = "kotsadm-nfs-minio", "kotsadm-nfs-minio"
	NFSMinioSecretName, NFSMinioDeploymentName, NFSMinioServiceName = "kotsadm-nfs-minio-creds", "kotsadm-nfs-minio", "kotsadm-nfs-minio"
)

type NFSDeployOptions struct {
	IsOpenShift bool
	NFSOptions  NFSOptions
}

type NFSOptions struct {
	Path    string
	Server  string
	Storage string
}

func DeployNFSMinio(ctx context.Context, clientset kubernetes.Interface, namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions) error {
	if err := ensurePV(ctx, clientset, namespace, deployOptions.NFSOptions); err != nil {
		return errors.Wrap(err, "failed to ensure nfs minio pv")
	}
	if err := ensurePVC(ctx, clientset, namespace, deployOptions.NFSOptions); err != nil {
		return errors.Wrap(err, "failed to ensure nfs minio pvc")
	}

	secret, err := ensureSecret(ctx, clientset, namespace)
	if err != nil {
		return errors.Wrap(err, "failed to ensure nfs minio secret")
	}
	marshalledSecret, err := k8syaml.Marshal(secret)
	if err != nil {
		return errors.Wrap(err, "failed to marshal nfs minio secret")
	}

	if err := ensureDeployment(ctx, clientset, namespace, deployOptions, registryOptions, marshalledSecret); err != nil {
		return errors.Wrap(err, "failed to ensure nfs minio deployment")
	}
	if err := ensureService(ctx, clientset, namespace); err != nil {
		return errors.Wrap(err, "failed to ensure service")
	}
	return nil
}

func ensurePV(ctx context.Context, clientset kubernetes.Interface, namespace string, nfsOptions NFSOptions) error {
	pv := pvResource(namespace, nfsOptions)

	_, err := clientset.CoreV1().PersistentVolumes().Get(ctx, pv.Name, metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing pv")
		}

		_, err = clientset.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to create pv")
		}

		return nil
	}

	// TODO NOW: no patch needed?

	return nil
}

func pvResource(namespace string, nfsOptions NFSOptions) *corev1.PersistentVolume {
	size := resource.MustParse("10Gi")
	if nfsOptions.Storage != "" {
		size = resource.MustParse(nfsOptions.Storage)
	}

	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: NFSMinioPVName,
		},
		Spec: corev1.PersistentVolumeSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ClaimRef: &corev1.ObjectReference{
				APIVersion: "v1",
				Kind:       "PersistentVolumeClaim",
				Name:       NFSMinioPVCName,
				Namespace:  namespace,
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceName(corev1.ResourceStorage): size,
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				NFS: &corev1.NFSVolumeSource{
					Path:   nfsOptions.Path,
					Server: nfsOptions.Server,
				},
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
		},
	}
}

func ensurePVC(ctx context.Context, clientset kubernetes.Interface, namespace string, nfsOptions NFSOptions) error {
	pvc := pvcResource(namespace, nfsOptions)

	_, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvc.Name, metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing pvc")
		}

		_, err = clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to create pvc")
		}

		return nil
	}

	// TODO NOW: no patch needed?

	return nil
}

func pvcResource(namespace string, nfsOptions NFSOptions) *corev1.PersistentVolumeClaim {
	size := resource.MustParse("10Gi")
	if nfsOptions.Storage != "" {
		size = resource.MustParse(nfsOptions.Storage)
	}

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: NFSMinioPVCName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName:  NFSMinioPVName,
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceStorage): size,
				},
			},
		},
	}
}

func ensureSecret(ctx context.Context, clientset kubernetes.Interface, namespace string) (*corev1.Secret, error) {
	secret := secretResource()

	existingSecret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return nil, errors.Wrap(err, "failed to get existing secret")
		}

		s, err := clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return nil, errors.Wrap(err, "failed to create secret")
		}

		return s, nil
	}

	existingSecret = updateSecret(existingSecret, secret)

	s, err := clientset.CoreV1().Secrets(namespace).Update(ctx, existingSecret, metav1.UpdateOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to update secret")
	}

	return s, nil
}

func updateSecret(existingSecret, desiredSecret *corev1.Secret) *corev1.Secret {
	existingSecret.Data = desiredSecret.Data
	return existingSecret
}

func secretResource() *corev1.Secret {
	accessKey := "kotsadm"
	secretKey := uuid.New().String()

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: NFSMinioSecretName,
		},
		Data: map[string][]byte{
			"MINIO_ACCESS_KEY": []byte(accessKey),
			"MINIO_SECRET_KEY": []byte(secretKey),
		},
	}
}

func ensureDeployment(ctx context.Context, clientset kubernetes.Interface, namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions, marshalledSecret []byte) error {
	secretChecksum := fmt.Sprintf("%x", md5.Sum(marshalledSecret))

	deployment, err := deploymentResource(clientset, namespace, secretChecksum, deployOptions, registryOptions)
	if err != nil {
		return errors.Wrap(err, "failed to get deployment resource")
	}

	existingDeployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deployment.Name, metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing deployment")
		}

		_, err = clientset.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to create deployment")
		}

		return nil
	}

	existingDeployment = updateDeployment(existingDeployment, deployment)

	_, err = clientset.AppsV1().Deployments(namespace).Update(ctx, existingDeployment, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to update deployment")
	}

	return nil
}

func deploymentResource(clientset kubernetes.Interface, namespace string, secretChecksum string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions) (*appsv1.Deployment, error) {
	image := "minio/minio:RELEASE.2020-01-25T02-50-51Z"
	imagePullSecrets := []corev1.LocalObjectReference{}

	if !kotsutil.IsKurl(clientset) || namespace != metav1.NamespaceDefault {
		var err error
		imageRewriteFn := kotsadmversion.ImageRewriteKotsadmRegistry(namespace, registryOptions)
		image, imagePullSecrets, err = imageRewriteFn(image, false)
		if err != nil {
			return nil, errors.Wrap(err, "failed to rewrite image")
		}
	}

	var securityContext corev1.PodSecurityContext
	if !deployOptions.IsOpenShift {
		securityContext = corev1.PodSecurityContext{
			RunAsUser: pointer.Int64Ptr(1001),
		}
	}

	env := []corev1.EnvVar{
		{
			Name:  "MINIO_UPDATE",
			Value: "off",
		},
		{
			Name: "MINIO_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: NFSMinioSecretName,
					},
					Key: "MINIO_ACCESS_KEY",
				},
			},
		},
		{
			Name: "MINIO_SECRET_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: NFSMinioSecretName,
					},
					Key: "MINIO_SECRET_KEY",
				},
			},
		},
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: NFSMinioDeploymentName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "kotsadm-nfs-minio",
				},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "kotsadm-nfs-minio",
					},
					Annotations: map[string]string{
						"kots.io/nfs-minio-creds-secret-checksum": secretChecksum,
					},
				},
				Spec: corev1.PodSpec{
					SecurityContext:  &securityContext,
					ImagePullSecrets: imagePullSecrets,
					Containers: []corev1.Container{
						{
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Name:            "minio",
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 9000},
							},
							Env: env,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name: "data", MountPath: "/data",
								},
							},
							Args: []string{"--quiet", "server", "data"},
							LivenessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/minio/health/live",
										Port: intstr.FromInt(9000),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       20,
							},
							ReadinessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/minio/health/ready",
										Port: intstr.FromInt(9000),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       20,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: NFSMinioPVCName,
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func updateDeployment(existingDeployment, desiredDeployment *appsv1.Deployment) *appsv1.Deployment {
	if len(existingDeployment.Spec.Template.Spec.Containers) == 0 {
		// hmm
		return desiredDeployment
	}

	if existingDeployment.Spec.Template.Annotations == nil {
		existingDeployment.Spec.Template.ObjectMeta.Annotations = map[string]string{}
	}
	existingDeployment.Spec.Template.ObjectMeta.Annotations["kots.io/nfs-minio-creds-secret-checksum"] = desiredDeployment.Spec.Template.ObjectMeta.Annotations["kots.io/nfs-minio-creds-secret-checksum"]

	existingDeployment.Spec.Template.Spec.Containers[0].Image = desiredDeployment.Spec.Template.Spec.Containers[0].Image
	existingDeployment.Spec.Template.Spec.Containers[0].LivenessProbe = desiredDeployment.Spec.Template.Spec.Containers[0].LivenessProbe
	existingDeployment.Spec.Template.Spec.Containers[0].ReadinessProbe = desiredDeployment.Spec.Template.Spec.Containers[0].ReadinessProbe
	existingDeployment.Spec.Template.Spec.Containers[0].Env = desiredDeployment.Spec.Template.Spec.Containers[0].Env

	return existingDeployment
}

func ensureService(ctx context.Context, clientset kubernetes.Interface, namespace string) error {
	service := serviceResource()

	existingService, err := clientset.CoreV1().Services(namespace).Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing service")
		}

		_, err = clientset.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to create service")
		}

		return nil
	}

	existingService = updateService(existingService, service)

	_, err = clientset.CoreV1().Services(namespace).Update(ctx, existingService, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to update service")
	}

	return nil
}

func serviceResource() *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: NFSMinioServiceName,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": "kotsadm-nfs-minio",
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       9000,
					TargetPort: intstr.FromInt(9000),
				},
			},
		},
	}
}

func updateService(existingService, desiredService *corev1.Service) *corev1.Service {
	existingService.Spec.Ports = desiredService.Spec.Ports

	return existingService
}

func WaitForNFSMinio(clientset *kubernetes.Clientset, namespace string, timeout time.Duration) (string, error) {
	start := time.Now()

	for {
		pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: "app=kotsadm-nfs-minio"})
		if err != nil {
			return "", errors.Wrap(err, "failed to list pods")
		}

		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				if pod.Status.ContainerStatuses[0].Ready == true {
					return pod.Name, nil
				}
			}
		}

		time.Sleep(time.Second)

		if time.Now().Sub(start) > timeout {
			return "", errors.New("timeout waiting for kotsadm-nfs-minio pod")
		}
	}
}
