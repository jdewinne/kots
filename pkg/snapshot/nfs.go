package snapshot

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	kotsadmtypes "github.com/replicatedhq/kots/pkg/kotsadm/types"
	kotsadmversion "github.com/replicatedhq/kots/pkg/kotsadm/version"
	"github.com/replicatedhq/kots/pkg/kotsutil"
	"github.com/replicatedhq/kots/pkg/util"
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
	NFSMinioProvider, NFSMinioBucketName, NFSMinioRegion            = "aws", "velero", "us-east-1"
	NFSMinioServicePort                                             = 9000
)

type NFSDeployOptions struct {
	// TODO NOW: move namespace here?
	IsOpenShift bool
	ForceReset  bool
	NFSOptions  NFSOptions
}

type NFSOptions struct {
	Path    string
	Server  string
	Storage string
}

type ResetNFSError struct {
	Message string
}

func (e ResetNFSError) Error() string {
	return e.Message
}

func DeployNFSMinio(ctx context.Context, clientset kubernetes.Interface, namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions) error {
	shouldReset, err := shouldResetNFSMount(ctx, clientset, namespace, deployOptions, registryOptions)
	if err != nil {
		return errors.Wrap(err, "failed to check if should reset nfs mount")
	}
	if shouldReset {
		if !deployOptions.ForceReset {
			return &ResetNFSError{Message: "nfs mount is already configured with a different minio instance"}
		}
		err := resetNFSMount(ctx, clientset, namespace, deployOptions, registryOptions)
		if err != nil {
			return errors.Wrap(err, "failed to reset nfs mount")
		}
	}

	// deploy resources
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

	// no patch needed

	return existingSecret, nil
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
					Port:       NFSMinioServicePort,
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

func WaitForNFSMinio(ctx context.Context, clientset *kubernetes.Clientset, namespace string, timeout time.Duration) (string, error) {
	start := time.Now()

	for {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=kotsadm-nfs-minio"})
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

func CreateNFSBucket(ctx context.Context, clientset *kubernetes.Clientset, namespace string) error {
	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, NFSMinioSecretName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to get nfs minio secret")
	}

	service, err := clientset.CoreV1().Services(namespace).Get(ctx, NFSMinioServiceName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to get nfs minio service")
	}

	endpoint := fmt.Sprintf("http://%s:%d", service.Spec.ClusterIP, service.Spec.Ports[0].Port)
	accessKeyID := string(secret.Data["MINIO_ACCESS_KEY"])
	secretAccessKey := string(secret.Data["MINIO_SECRET_KEY"])

	s3Config := &aws.Config{
		Region:           aws.String(NFSMinioRegion),
		Endpoint:         aws.String(endpoint),
		DisableSSL:       aws.Bool(true), // TODO: this needs to be configurable
		S3ForcePathStyle: aws.Bool(true),
	}

	if accessKeyID != "" && secretAccessKey != "" {
		s3Config.Credentials = credentials.NewStaticCredentials(accessKeyID, secretAccessKey, "")
	}

	newSession := session.New(s3Config)
	s3Client := s3.New(newSession)

	_, err = s3Client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(NFSMinioBucketName),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "NotFound" {
				_, err = s3Client.CreateBucket(&s3.CreateBucketInput{
					Bucket: aws.String(NFSMinioBucketName),
				})
				if err != nil {
					return errors.Wrap(err, "failed to create bucket")
				}
			}
		}
		return errors.Wrap(err, "failed to check if bucket exists")
	}

	return nil
}

func shouldResetNFSMount(ctx context.Context, clientset kubernetes.Interface, namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions) (bool, error) {
	checkPod, err := createNFSMinioCheckPod(ctx, clientset, namespace, deployOptions, registryOptions)
	if err != nil {
		return false, errors.Wrap(err, "failed to create nfs minio check pod")
	}

	if err := waitForPodCompleted(ctx, clientset, namespace, checkPod.Name, time.Minute*2); err != nil {
		return false, errors.Wrap(err, "failed to wait for nfs minio check pod to complete")
	}

	defer func() {
		// clean up
		err := clientset.CoreV1().Pods(namespace).Delete(ctx, checkPod.Name, metav1.DeleteOptions{})
		if err != nil {
			// TODO NOW (log error)
		}
	}()

	logs, err := getPodLogs(ctx, clientset, checkPod)
	if err != nil {
		return false, errors.Wrap(err, "failed to get nfs minio check pod logs")
	}
	if len(logs) == 0 {
		return false, errors.New("no logs found")
	}

	type NFSMinioCheckPodOutput struct {
		HasMinioConfig bool   `json:"hasMinioConfig"`
		MinioKeysSHA   string `json:"minioKeysSHA"`
	}

	checkPodOutput := NFSMinioCheckPodOutput{}

	scanner := bufio.NewScanner(bytes.NewReader(logs))
	for scanner.Scan() {
		line := scanner.Text()

		if err := json.Unmarshal([]byte(line), &checkPodOutput); err != nil {
			continue
		}

		break
	}

	if !checkPodOutput.HasMinioConfig {
		return false, nil
	}

	if checkPodOutput.MinioKeysSHA == "" {
		return true, nil
	}

	minioSecret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, NFSMinioSecretName, metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return false, errors.Wrap(err, "failed to get existing minio secret")
		}

		return true, nil
	}

	newMinioKeysSHA := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s,%s", string(minioSecret.Data["MINIO_ACCESS_KEY"]), string(minioSecret.Data["MINIO_SECRET_KEY"])))))
	if newMinioKeysSHA == checkPodOutput.MinioKeysSHA {
		return false, nil
	}

	return true, nil
}

func resetNFSMount(ctx context.Context, clientset kubernetes.Interface, namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions) error {
	resetPod, err := createNFSMinioResetPod(ctx, clientset, namespace, deployOptions, registryOptions)
	if err != nil {
		return errors.Wrap(err, "failed to create nfs minio reset pod")
	}

	if err := waitForPodCompleted(ctx, clientset, namespace, resetPod.Name, time.Minute*2); err != nil {
		return errors.Wrap(err, "failed to wait for nfs minio reset pod to complete")
	}

	defer func() {
		// clean up
		err := clientset.CoreV1().Pods(namespace).Delete(ctx, resetPod.Name, metav1.DeleteOptions{})
		if err != nil {
			// TODO NOW (log error)
		}
	}()

	// TODO NOW: write keys sha file

	return nil
}

func createNFSMinioCheckPod(ctx context.Context, clientset kubernetes.Interface, namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions) (*corev1.Pod, error) {
	pod := nfsMinioCheckPod(ctx, clientset, namespace, deployOptions, registryOptions)
	p, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create pod")
	}

	return p, nil
}

func createNFSMinioResetPod(ctx context.Context, clientset kubernetes.Interface, namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions) (*corev1.Pod, error) {
	pod := nfsMinioResetPod(ctx, clientset, namespace, deployOptions, registryOptions)
	p, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create pod")
	}

	return p, nil
}

func nfsMinioCheckPod(ctx context.Context, clientset kubernetes.Interface, namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions) *corev1.Pod {
	podName := fmt.Sprintf("kotsadm-nfs-minio-check-%d", time.Now().Unix())
	// TODO NOW move to .sh file?
	command := `if [ ! -d /nfs/.minio.sys/config ]; then echo '{"hasMinioConfig": false}'; elif [ ! -f /nfs/.kots/minio-keys-sha.txt ]; then echo '{"hasMinioConfig": true}'; else SHA=$(cat /nfs/.kots/minio-keys-sha.txt); echo '{"hasMinioConfig": true, "minioKeysSHA":"'"$SHA"'"}'; fi`
	return nfsMinioConfigPod(namespace, deployOptions, registryOptions, podName, command)
}

func nfsMinioResetPod(ctx context.Context, clientset kubernetes.Interface, namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions) *corev1.Pod {
	podName := fmt.Sprintf("kotsadm-nfs-minio-reset-%d", time.Now().Unix())
	command := `rm -rf /nfs/.minio.sys/config`
	return nfsMinioConfigPod(namespace, deployOptions, registryOptions, podName, command)
}

func nfsMinioConfigPod(namespace string, deployOptions NFSDeployOptions, registryOptions *kotsadmtypes.KotsadmOptions, podName string, command string) *corev1.Pod {
	var securityContext corev1.PodSecurityContext
	if !deployOptions.IsOpenShift {
		securityContext = corev1.PodSecurityContext{
			RunAsUser: util.IntPointer(1001),
			FSGroup:   util.IntPointer(1001),
		}
	}

	var pullSecrets []corev1.LocalObjectReference
	if s := kotsadmversion.KotsadmPullSecret(namespace, *registryOptions); s != nil {
		pullSecrets = []corev1.LocalObjectReference{
			{
				Name: s.ObjectMeta.Name,
			},
		}
	}

	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			SecurityContext:  &securityContext,
			RestartPolicy:    corev1.RestartPolicyOnFailure,
			ImagePullSecrets: pullSecrets,
			Volumes: []corev1.Volume{
				{
					Name: "nfs",
					VolumeSource: corev1.VolumeSource{
						NFS: &corev1.NFSVolumeSource{
							Path:   deployOptions.NFSOptions.Path,
							Server: deployOptions.NFSOptions.Server,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Image:           fmt.Sprintf("%s/kotsadm:%s", kotsadmversion.KotsadmRegistry(*registryOptions), kotsadmversion.KotsadmTag(*registryOptions)),
					ImagePullPolicy: corev1.PullIfNotPresent,
					Name:            "minio-check",
					Command: []string{
						"/bin/sh",
						"-c",
						command,
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "nfs",
							MountPath: "/nfs",
							ReadOnly:  true,
						},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"cpu":    resource.MustParse("100m"),
							"memory": resource.MustParse("100Mi"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("50m"),
							"memory": resource.MustParse("50Mi"),
						},
					},
				},
			},
		},
	}

	return pod
}

func waitForPodCompleted(ctx context.Context, clientset kubernetes.Interface, namespace string, podName string, timeoutWaitingForPod time.Duration) error {
	start := time.Now()

	for {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to list pods")
		}

		if pod.Status.Phase == corev1.PodSucceeded {
			return nil
		}

		time.Sleep(time.Second)

		if time.Now().Sub(start) > timeoutWaitingForPod {
			return errors.New("timeout waiting for pod to complete")
		}
	}
}

func getPodLogs(ctx context.Context, clientset kubernetes.Interface, pod *corev1.Pod) ([]byte, error) {
	podLogOpts := corev1.PodLogOptions{
		Container: pod.Spec.Containers[0].Name,
	}

	req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get log stream")
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	errChan := make(chan error, 0)
	go func() {
		_, err := io.Copy(buf, podLogs)
		errChan <- err
	}()

	select {
	case resErr := <-errChan:
		if resErr != nil {
			return nil, errors.Wrap(resErr, "failed to copy logs")
		} else {
			return buf.Bytes(), nil
		}
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "context ended copying logs")
	}
}
