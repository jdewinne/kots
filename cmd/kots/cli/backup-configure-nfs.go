package cli

import (
	"time"

	"github.com/pkg/errors"
	"github.com/replicatedhq/kots/pkg/k8sutil"
	"github.com/replicatedhq/kots/pkg/kotsadm"
	"github.com/replicatedhq/kots/pkg/logger"
	"github.com/replicatedhq/kots/pkg/snapshot"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

			registryOptions, err := kotsadm.GetKotsadmOptionsFromCluster(namespace, clientset)
			if err != nil {
				return errors.Wrap(err, "failed to get kotsadm options from cluster")
			}

			log := logger.NewLogger()
			log.ActionWithSpinner("Setting up NFS Minio")

			deployOptions := snapshot.NFSDeployOptions{
				IsOpenShift: k8sutil.IsOpenShift(clientset),
				NFSOptions: snapshot.NFSOptions{
					Path:    nfsPath,
					Server:  nfsServer,
					Storage: v.GetString("storage"),
				},
			}
			if err := snapshot.DeployNFSMinio(cmd.Context(), clientset, namespace, deployOptions, &registryOptions); err != nil {
				log.FinishSpinnerWithError()
				return errors.Wrap(err, "failed to deploy nfs minio")
			}

			log.FinishSpinner()
			log.ActionWithSpinner("Waiting for NFS Minio to by ready")

			_, err = snapshot.WaitForNFSMinio(cmd.Context(), clientset, namespace, time.Minute*5)
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

			log.FinishSpinner()
			log.ActionWithSpinner("Configuring Velero")

			veleroNamespace, err := snapshot.DetectVeleroNamespace()
			if err != nil {
				log.FinishSpinnerWithError()
				return errors.Wrap(err, "failed to detect velero namespace")
			}

			if veleroNamespace == "" {
				log.FinishSpinner()
				log.ActionWithoutSpinner("")
				// TODO NOW: print info
				log.Info("NFS configuration for the Admin Console is successful, but no Velero installation has been detected. Use the following information to set up Velero:\n\n")
				log.Info("")
				log.ActionWithoutSpinner("")
				return nil
			}

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
	cmd.Flags().String("storage", "10Gi", "the storage capacity to be request")
	cmd.Flags().StringP("namespace", "n", "", "the namespace in which kots/kotsadm is installed")

	return cmd
}
