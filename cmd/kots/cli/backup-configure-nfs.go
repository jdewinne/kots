package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	"github.com/pkg/errors"
	"github.com/replicatedhq/kots/pkg/k8sutil"
	"github.com/replicatedhq/kots/pkg/logger"
	"github.com/replicatedhq/kots/pkg/snapshot"
	"github.com/replicatedhq/kots/pkg/snapshot/types"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/ini.v1"
	"k8s.io/client-go/kubernetes"
)

func BackupConfigureNFSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "configure-nfs",
		Short:         "Configure snapshots to use NFS as storage",
		Long:          ``,
		SilenceUsage:  true,
		SilenceErrors: false,
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			v := viper.GetViper()

			namespace := v.GetString("namespace")
			if err := validateNamespace(namespace); err != nil {
				return err
			}

			nfsPath := v.GetString("path")
			if nfsPath == "" {
				return errors.New("path is rquired")
			}

			nfsServer := v.GetString("server")
			if nfsServer == "" {
				return errors.New("server is rquired")
			}

			clientset, err := k8sutil.GetClientset(kubernetesConfigFlags)
			if err != nil {
				return errors.Wrap(err, "failed to get clientset")
			}

			registryOptions, err := getRegistryConfig(v)
			if err != nil {
				return errors.Wrap(err, "failed to get registry config")
			}

			log := logger.NewLogger()
			log.ActionWithSpinner("Setting up NFS Minio")

			deployOptions := snapshot.NFSDeployOptions{
				Namespace:   namespace,
				IsOpenShift: k8sutil.IsOpenShift(clientset),
				NFSConfig: types.NFSConfig{
					Path:   nfsPath,
					Server: nfsServer,
				},
			}
			if err := snapshot.DeployNFSMinio(cmd.Context(), clientset, deployOptions, *registryOptions); err != nil {
				if _, ok := err.(*snapshot.ResetNFSError); ok {
					log.FinishSpinner()
					forceReset := promptForNFSReset(nfsPath)
					if forceReset {
						log.ActionWithSpinner("Re-configuring NFS Minio")
						deployOptions.ForceReset = true
						if err := snapshot.DeployNFSMinio(cmd.Context(), clientset, deployOptions, *registryOptions); err != nil {
							log.FinishSpinnerWithError()
							return errors.Wrap(err, "failed to force deploy nfs minio")
						}
					}
				} else {
					log.FinishSpinnerWithError()
					return errors.Wrap(err, "failed to deploy nfs minio")
				}
			}

			log.FinishSpinner()
			log.ActionWithSpinner("Waiting for NFS Minio to be ready")

			err = snapshot.WaitForNFSMinioReady(cmd.Context(), clientset, namespace, time.Minute*5)
			if err != nil {
				log.FinishSpinnerWithError()
				return errors.Wrap(err, "failed to wait for nfs minio")
			}

			log.FinishSpinner()
			log.ActionWithSpinner("Creating Default Bucket")

			err = snapshot.CreateNFSBucket(cmd.Context(), clientset, namespace)
			if err != nil {
				log.FinishSpinnerWithError()
				return errors.Wrap(err, "failed to create default bucket")
			}

			veleroNamespace, err := snapshot.DetectVeleroNamespace()
			if err != nil {
				log.FinishSpinnerWithError()
				return errors.Wrap(err, "failed to detect velero namespace")
			}

			if veleroNamespace == "" {
				c, err := getNFSMinioVeleroConfig(clientset, namespace)
				if err != nil {
					log.FinishSpinnerWithError()
					return errors.Wrap(err, "failed to get nfs minio velero config")
				}
				log.FinishSpinner()
				log.ActionWithoutSpinner("NFS configuration for the Admin Console is successful, but no Velero installation has been detected. Use the following information to set up Velero:\n")
				log.Info("Credentials:\n\n%s", c.Credentials)
				log.Info("Velero flags:\n\n%s", c.VeleroBlcFlag)
				log.ActionWithoutSpinner("")
				return nil
			}

			log.FinishSpinner()
			log.ActionWithSpinner("Configuring Velero")

			_, err = snapshot.GetGlobalStore(nil)
			if err != nil {
				log.FinishSpinnerWithError()
				return errors.Wrap(err, "failed to get global store")
			}

			configureStoreOptions := snapshot.ConfigureStoreOptions{
				NFS:           true,
				KOTSNamespace: namespace,
			}
			_, err = snapshot.ConfigureStore(configureStoreOptions)
			if err != nil {
				return errors.Wrap(err, "failed to configure store")
			}

			log.FinishSpinner()

			return nil
		},
	}

	cmd.Flags().String("path", "", "path that is exported by the NFS server")
	cmd.Flags().String("server", "", "the hostname or IP address of the NFS server")
	cmd.Flags().StringP("namespace", "n", "", "the namespace in which kots/kotsadm is installed")
	cmd.Flags().Bool("airgap", false, "set to true to run in airgapped mode.")

	registryFlags(cmd.Flags())

	return cmd
}

type NFSMinioVeleroConfig struct {
	Credentials   string
	VeleroBlcFlag string
}

func getNFSMinioVeleroConfig(clientset kubernetes.Interface, namespace string) (*NFSMinioVeleroConfig, error) {
	nfsStore, err := snapshot.BuildNFSStore(clientset, namespace)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build nfs store")
	}

	credsStr, err := formatCredentials(nfsStore.AccessKeyID, nfsStore.SecretAccessKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to format credentials")
	}

	publicURL := fmt.Sprintf("http://%s:%d", nfsStore.ObjectStoreClusterIP, snapshot.NFSMinioServicePort)
	s3URL := nfsStore.Endpoint
	veleroBlcFlag := fmt.Sprintf(`--backup-location-config region=%s,s3ForcePathStyle="true",s3Url=%s,publicUrl=%s`, snapshot.NFSMinioRegion, s3URL, publicURL)

	return &NFSMinioVeleroConfig{
		Credentials:   credsStr,
		VeleroBlcFlag: veleroBlcFlag,
	}, nil
}

func promptForNFSReset(nfsPath string) bool {
	// TODO NOW: new lines
	label := snapshot.GetNFSResetWarningMsg(nfsPath)

	prompt := promptui.Prompt{
		Label:     label,
		IsConfirm: true,
	}

	for {
		// TODO NOW: don't print msg on each key stroke
		resp, err := prompt.Run()
		if err == promptui.ErrInterrupt {
			os.Exit(-1)
		}
		if strings.ToLower(resp) == "n" {
			os.Exit(-1)
		}
		if strings.ToLower(resp) == "y" {
			return true
		}
	}
}

func formatCredentials(accessKeyID, secretAccessKey string) (string, error) {
	awsCfg := ini.Empty()
	section, err := awsCfg.NewSection("default")
	if err != nil {
		return "", errors.Wrap(err, "failed to create default section in aws creds")
	}
	_, err = section.NewKey("aws_access_key_id", accessKeyID)
	if err != nil {
		return "", errors.Wrap(err, "failed to create access key")
	}

	_, err = section.NewKey("aws_secret_access_key", secretAccessKey)
	if err != nil {
		return "", errors.Wrap(err, "failed to create secret access key")
	}

	var awsCredentials bytes.Buffer
	writer := bufio.NewWriter(&awsCredentials)
	_, err = awsCfg.WriteTo(writer)
	if err != nil {
		return "", errors.Wrap(err, "failed to write ini")
	}
	if err := writer.Flush(); err != nil {
		return "", errors.Wrap(err, "failed to flush buffer")
	}

	return strings.TrimSpace(awsCredentials.String()), nil
}
