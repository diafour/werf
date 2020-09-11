package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"

	"github.com/werf/logboek"

	"github.com/spf13/cobra"

	"github.com/werf/werf/cmd/werf/converge"
	"github.com/werf/werf/cmd/werf/diff"
	"github.com/werf/werf/cmd/werf/helm_v3"

	"github.com/werf/werf/cmd/werf/build"
	"github.com/werf/werf/cmd/werf/build_and_publish"
	"github.com/werf/werf/cmd/werf/cleanup"
	"github.com/werf/werf/cmd/werf/deploy"
	"github.com/werf/werf/cmd/werf/dismiss"
	"github.com/werf/werf/cmd/werf/publish"
	"github.com/werf/werf/cmd/werf/purge"
	"github.com/werf/werf/cmd/werf/run"
	"github.com/werf/werf/cmd/werf/synchronization"

	helm_secret_decrypt "github.com/werf/werf/cmd/werf/helm/secret/decrypt"
	helm_secret_encrypt "github.com/werf/werf/cmd/werf/helm/secret/encrypt"
	helm_secret_file_decrypt "github.com/werf/werf/cmd/werf/helm/secret/file/decrypt"
	helm_secret_file_edit "github.com/werf/werf/cmd/werf/helm/secret/file/edit"
	helm_secret_file_encrypt "github.com/werf/werf/cmd/werf/helm/secret/file/encrypt"
	helm_secret_generate_secret_key "github.com/werf/werf/cmd/werf/helm/secret/generate_secret_key"
	helm_secret_rotate_secret_key "github.com/werf/werf/cmd/werf/helm/secret/rotate_secret_key"
	helm_secret_values_decrypt "github.com/werf/werf/cmd/werf/helm/secret/values/decrypt"
	helm_secret_values_edit "github.com/werf/werf/cmd/werf/helm/secret/values/edit"
	helm_secret_values_encrypt "github.com/werf/werf/cmd/werf/helm/secret/values/encrypt"

	"github.com/werf/werf/cmd/werf/ci_env"
	"github.com/werf/werf/cmd/werf/slugify"

	managed_images_add "github.com/werf/werf/cmd/werf/managed_images/add"
	managed_images_ls "github.com/werf/werf/cmd/werf/managed_images/ls"
	managed_images_rm "github.com/werf/werf/cmd/werf/managed_images/rm"

	images_publish "github.com/werf/werf/cmd/werf/images/publish"

	stages_build "github.com/werf/werf/cmd/werf/stages/build"
	stages_switch "github.com/werf/werf/cmd/werf/stages/switch_from_local"
	stages_sync "github.com/werf/werf/cmd/werf/stages/sync"

	stage_image "github.com/werf/werf/cmd/werf/stage/image"

	host_cleanup "github.com/werf/werf/cmd/werf/host/cleanup"
	host_project_list "github.com/werf/werf/cmd/werf/host/project/list"
	host_project_purge "github.com/werf/werf/cmd/werf/host/project/purge"
	host_purge "github.com/werf/werf/cmd/werf/host/purge"

	helm_delete "github.com/werf/werf/cmd/werf/helm/delete"
	helm_dependency "github.com/werf/werf/cmd/werf/helm/dependency"
	helm_deploy_chart "github.com/werf/werf/cmd/werf/helm/deploy_chart"
	helm_get "github.com/werf/werf/cmd/werf/helm/get"
	helm_get_autogenerated_values "github.com/werf/werf/cmd/werf/helm/get_autogenerated_values"
	helm_get_namespace "github.com/werf/werf/cmd/werf/helm/get_namespace"
	helm_get_release "github.com/werf/werf/cmd/werf/helm/get_release"
	helm_history "github.com/werf/werf/cmd/werf/helm/history"
	helm_lint "github.com/werf/werf/cmd/werf/helm/lint"
	helm_list "github.com/werf/werf/cmd/werf/helm/list"
	helm_render "github.com/werf/werf/cmd/werf/helm/render"
	helm_repo "github.com/werf/werf/cmd/werf/helm/repo"
	helm_rollback "github.com/werf/werf/cmd/werf/helm/rollback"

	config_list "github.com/werf/werf/cmd/werf/config/list"
	config_render "github.com/werf/werf/cmd/werf/config/render"

	"github.com/werf/werf/cmd/werf/completion"
	"github.com/werf/werf/cmd/werf/docs"
	"github.com/werf/werf/cmd/werf/version"

	"github.com/werf/werf/cmd/werf/common"
	"github.com/werf/werf/cmd/werf/common/templates"
	"github.com/werf/werf/pkg/process_exterminator"
)

func main() {
	common.EnableTerminationSignalsTrap()
	log.SetOutput(logboek.ProxyOutStream())
	logrus.StandardLogger().SetOutput(logboek.ProxyOutStream())

	if err := process_exterminator.Init(); err != nil {
		common.TerminateWithError(fmt.Sprintf("process exterminator initialization failed: %s", err), 1)
	}

	rootCmd := constructRootCmd()

	if err := rootCmd.Execute(); err != nil {
		common.TerminateWithError(err.Error(), 1)
	}
}

func constructRootCmd() *cobra.Command {
	if filepath.Base(os.Args[0]) == "helm" || os.Getenv("WERF_HELM3_MODE") == "1" {
		return helm_v3.NewCmd()
	}

	rootCmd := &cobra.Command{
		Use:   "werf",
		Short: "werf helps to implement and support Continuous Integration and Continuous Delivery",
		Long: common.GetLongCommandDescription(`werf helps to implement and support Continuous Integration and Continuous Delivery.

Find more information at https://werf.io`),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	groups := templates.CommandGroups{
		{
			Message: "Main Commands:",
			Commands: []*cobra.Command{
				converge.NewCmd(),
				diff.NewCmd(),
				build.NewCmd(),
				publish.NewCmd(),
				build_and_publish.NewCmd(),
				run.NewCmd(),
				deploy.NewCmd(),
				dismiss.NewCmd(),
				cleanup.NewCmd(),
				purge.NewCmd(),
				synchronization.NewCmd(),
			},
		},
		{
			Message: "Toolbox:",
			Commands: []*cobra.Command{
				slugify.NewCmd(),
				ci_env.NewCmd(),
			},
		},
		{
			Message: "Lowlevel Management Commands:",
			Commands: []*cobra.Command{
				configCmd(),
				stagesCmd(),
				imagesCmd(),
				managedImagesCmd(),
				helmCmd(),
				helm_v3.NewCmd(),
				hostCmd(),
			},
		},
	}
	groups.Add(rootCmd)

	templates.ActsAsRootCommand(rootCmd, groups...)

	rootCmd.AddCommand(
		completion.NewCmd(rootCmd),
		version.NewCmd(),
		docs.NewCmd(),
		stageCmd(),
	)

	return rootCmd
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Work with werf.yaml",
	}
	cmd.AddCommand(
		config_render.NewCmd(),
		config_list.NewCmd(),
	)

	return cmd
}

func managedImagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "managed-images",
		Short: "Work with managed images which will be preserved during cleanup procedure",
	}
	cmd.AddCommand(
		managed_images_add.NewCmd(),
		managed_images_ls.NewCmd(),
		managed_images_rm.NewCmd(),
	)

	return cmd
}

func imagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Work with images",
	}
	cmd.AddCommand(
		images_publish.NewCmd(),
	)

	return cmd
}

func stagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stages",
		Short: "Work with stages, which are cache for images",
	}
	cmd.AddCommand(
		stages_build.NewCmd(),
		stages_switch.NewCmd(),
		stages_sync.NewCmd(),
	)

	return cmd
}

func stageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "stage",
		Hidden: true,
	}
	cmd.AddCommand(
		stage_image.NewCmd(),
	)

	return cmd
}

func helmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "helm",
		Short: "Manage application deployment with helm",
	}
	cmd.AddCommand(
		helm_get_namespace.NewCmd(),
		helm_get_release.NewCmd(),
		helm_get_autogenerated_values.NewCmd(),
		helm_deploy_chart.NewCmd(),
		helm_lint.NewCmd(),
		helm_render.NewCmd(),
		helm_list.NewCmd(),
		helm_delete.NewCmd(),
		helm_rollback.NewCmd(),
		helm_get.NewCmd(),
		helm_history.NewCmd(),
		secretCmd(),
		helm_repo.NewRepoCmd(),
		helm_dependency.NewDependencyCmd(),
	)

	return cmd
}

func hostCmd() *cobra.Command {
	hostCmd := &cobra.Command{
		Use:   "host",
		Short: "Work with werf cache and data of all projects on the host machine",
	}

	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Work with projects",
	}

	projectCmd.AddCommand(
		host_project_list.NewCmd(),
		host_project_purge.NewCmd(),
	)

	hostCmd.AddCommand(
		host_cleanup.NewCmd(),
		host_purge.NewCmd(),
		projectCmd,
	)

	return hostCmd
}

func secretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Work with secrets",
	}

	fileCmd := &cobra.Command{
		Use:   "file",
		Short: "Work with secret files",
	}

	fileCmd.AddCommand(
		helm_secret_file_encrypt.NewCmd(),
		helm_secret_file_decrypt.NewCmd(),
		helm_secret_file_edit.NewCmd(),
	)

	valuesCmd := &cobra.Command{
		Use:   "values",
		Short: "Work with secret values files",
	}

	valuesCmd.AddCommand(
		helm_secret_values_encrypt.NewCmd(),
		helm_secret_values_decrypt.NewCmd(),
		helm_secret_values_edit.NewCmd(),
	)

	cmd.AddCommand(
		fileCmd,
		valuesCmd,
		helm_secret_generate_secret_key.NewCmd(),
		helm_secret_encrypt.NewCmd(),
		helm_secret_decrypt.NewCmd(),
		helm_secret_rotate_secret_key.NewCmd(),
	)

	return cmd
}
