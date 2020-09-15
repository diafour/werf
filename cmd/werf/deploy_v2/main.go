package deploy_v2

import (
	"context"
	"fmt"
	"time"

	"github.com/werf/werf/pkg/deploy_v2/helm_v3"

	"github.com/werf/werf/pkg/deploy"
	"github.com/werf/werf/pkg/deploy_v2/lock_manager"

	"helm.sh/helm/v3/pkg/action"

	cmd_helm "helm.sh/helm/v3/cmd/helm"

	"github.com/werf/werf/pkg/deploy_v2/werf_chart"

	"github.com/spf13/cobra"

	"github.com/werf/kubedog/pkg/kube"
	"github.com/werf/logboek"
	"github.com/werf/werf/cmd/werf/common"
	"github.com/werf/werf/pkg/build"
	"github.com/werf/werf/pkg/container_runtime"
	"github.com/werf/werf/pkg/docker"
	"github.com/werf/werf/pkg/image"
	"github.com/werf/werf/pkg/images_manager"
	"github.com/werf/werf/pkg/ssh_agent"
	"github.com/werf/werf/pkg/stages_manager"
	"github.com/werf/werf/pkg/tag_strategy"
	"github.com/werf/werf/pkg/tmp_manager"
	"github.com/werf/werf/pkg/true_git"
	"github.com/werf/werf/pkg/werf"
)

var cmdData struct {
	Timeout int
}

var commonCmdData common.CmdData

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy application into Kubernetes",
		Long: common.GetLongCommandDescription(`Deploy application into Kubernetes.

Command will create Helm Release and wait until all resources of the release are become ready.

Deploy needs the same parameters as push to construct image names: repo and tags. Docker images names are constructed from parameters as IMAGES_REPO/IMAGE_NAME:TAG. Deploy will fetch built image ids from Docker repo. So images should be published prior running deploy.

Helm chart directory .helm should exists and contain valid Helm chart.

Environment is a required param for the deploy by default, because it is needed to construct Helm Release name and Kubernetes Namespace. Either --env or $WERF_ENV should be specified for command.

Read more info about Helm chart structure, Helm Release name, Kubernetes Namespace and how to change it: https://werf.io/documentation/reference/deploy_process/deploy_into_kubernetes.html`),
		Example: `  # Deploy project named 'myproject' into 'dev' environment using images from registry.mydomain.com/myproject tagged as mytag with git-tag tagging strategy; helm release name and namespace will be named as 'myproject-dev'
  $ werf deploy --stages-storage :local --env dev --images-repo registry.mydomain.com/myproject --tag-git-tag mytag

  # Deploy project using specified helm release name and namespace using images from registry.mydomain.com/myproject tagged with docker tag 'myversion'
  $ werf deploy --stages-storage :local --release myrelease --namespace myns --images-repo registry.mydomain.com/myproject --tag-custom myversion`,
		DisableFlagsInUseLine: true,
		Annotations: map[string]string{
			common.CmdEnvAnno: common.EnvsDescription(common.WerfSecretKey),
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			defer werf.PrintGlobalWarnings(common.BackgroundContext())

			if err := common.ProcessLogOptions(&commonCmdData); err != nil {
				common.PrintHelp(cmd)
				return err
			}
			common.LogVersion()

			return common.LogRunningTime(func() error {
				return runDeploy()
			})
		},
	}

	common.SetupDir(&commonCmdData, cmd)
	common.SetupConfigPath(&commonCmdData, cmd)
	common.SetupConfigTemplatesDir(&commonCmdData, cmd)
	common.SetupTmpDir(&commonCmdData, cmd)
	common.SetupHomeDir(&commonCmdData, cmd)
	common.SetupSSHKey(&commonCmdData, cmd)

	common.SetupTag(&commonCmdData, cmd)
	common.SetupEnvironment(&commonCmdData, cmd)
	common.SetupRelease(&commonCmdData, cmd)
	common.SetupNamespace(&commonCmdData, cmd)
	common.SetupAddAnnotations(&commonCmdData, cmd)
	common.SetupAddLabels(&commonCmdData, cmd)

	common.SetupHelmChartDir(&commonCmdData, cmd)
	common.SetupKubeConfig(&commonCmdData, cmd)
	common.SetupKubeConfigBase64(&commonCmdData, cmd)
	common.SetupKubeContext(&commonCmdData, cmd)
	common.SetupHelmReleaseStorageNamespace(&commonCmdData, cmd)
	common.SetupHelmReleaseStorageType(&commonCmdData, cmd)
	common.SetupStatusProgressPeriod(&commonCmdData, cmd)
	common.SetupHooksStatusProgressPeriod(&commonCmdData, cmd)
	common.SetupReleasesHistoryMax(&commonCmdData, cmd)

	common.SetupStagesStorageOptions(&commonCmdData, cmd)
	common.SetupImagesRepoOptions(&commonCmdData, cmd)

	common.SetupSynchronization(&commonCmdData, cmd)

	common.SetupDockerConfig(&commonCmdData, cmd, "Command needs granted permissions to read and pull images from the specified stages storage and images repo")
	common.SetupInsecureRegistry(&commonCmdData, cmd)
	common.SetupSkipTlsVerifyRegistry(&commonCmdData, cmd)

	common.SetupLogOptions(&commonCmdData, cmd)
	common.SetupLogProjectDir(&commonCmdData, cmd)

	common.SetupSet(&commonCmdData, cmd)
	common.SetupSetString(&commonCmdData, cmd)
	common.SetupValues(&commonCmdData, cmd)
	common.SetupSecretValues(&commonCmdData, cmd)
	common.SetupIgnoreSecretKey(&commonCmdData, cmd)

	common.SetupVirtualMerge(&commonCmdData, cmd)
	common.SetupVirtualMergeFromCommit(&commonCmdData, cmd)
	common.SetupVirtualMergeIntoCommit(&commonCmdData, cmd)

	common.SetupGitUnshallow(&commonCmdData, cmd)
	common.SetupAllowGitShallowClone(&commonCmdData, cmd)

	cmd.Flags().IntVarP(&cmdData.Timeout, "timeout", "t", 0, "Resources tracking timeout in seconds")

	return cmd
}

func runDeploy() error {
	tmp_manager.AutoGCEnabled = true
	ctx := common.BackgroundContext()

	if err := werf.Init(*commonCmdData.TmpDir, *commonCmdData.HomeDir); err != nil {
		return fmt.Errorf("initialization error: %s", err)
	}

	if err := image.Init(); err != nil {
		return err
	}

	if err := true_git.Init(true_git.Options{LiveGitOutput: *commonCmdData.LogVerbose || *commonCmdData.LogDebug}); err != nil {
		return err
	}

	if err := common.DockerRegistryInit(&commonCmdData); err != nil {
		return err
	}

	if err := docker.Init(ctx, *commonCmdData.DockerConfig, *commonCmdData.LogVerbose, *commonCmdData.LogDebug); err != nil {
		return err
	}

	if ctxWithDockerCli, err := docker.NewContext(ctx); err != nil {
		return err
	} else {
		ctx = ctxWithDockerCli
	}

	if err := kube.Init(kube.InitOptions{KubeConfigOptions: kube.KubeConfigOptions{
		Context:          *commonCmdData.KubeContext,
		ConfigPath:       *commonCmdData.KubeConfig,
		ConfigDataBase64: *commonCmdData.KubeConfigBase64,
	}}); err != nil {
		return fmt.Errorf("cannot initialize kube: %s", err)
	}

	if err := common.InitKubedog(ctx); err != nil {
		return fmt.Errorf("cannot init kubedog: %s", err)
	}

	projectDir, err := common.GetProjectDir(&commonCmdData)
	if err != nil {
		return fmt.Errorf("getting project dir failed: %s", err)
	}

	common.ProcessLogProjectDir(&commonCmdData, projectDir)

	chartDir, err := common.GetHelmChartDir(projectDir, &commonCmdData)
	if err != nil {
		return fmt.Errorf("getting helm chart dir failed: %s", err)
	}

	projectTmpDir, err := tmp_manager.CreateProjectDir(ctx)
	if err != nil {
		return fmt.Errorf("getting project tmp dir failed: %s", err)
	}
	defer tmp_manager.ReleaseProjectDir(projectTmpDir)

	werfConfig, err := common.GetRequiredWerfConfig(ctx, projectDir, &commonCmdData, true)
	if err != nil {
		return fmt.Errorf("unable to load werf config: %s", err)
	}

	var imagesRepository string
	var tag string
	var tagStrategy tag_strategy.TagStrategy
	var imagesInfoGetters []images_manager.ImageInfoGetter

	projectName := werfConfig.Meta.Project

	if len(werfConfig.StapelImages) != 0 || len(werfConfig.ImagesFromDockerfile) != 0 {
		containerRuntime := &container_runtime.LocalDockerServerRuntime{} // TODO

		stagesStorage, err := common.GetStagesStorage(containerRuntime, &commonCmdData)
		if err != nil {
			return err
		}
		synchronization, err := common.GetSynchronization(ctx, &commonCmdData, projectName, stagesStorage)
		if err != nil {
			return err
		}
		storageLockManager, err := common.GetStorageLockManager(ctx, synchronization)
		if err != nil {
			return err
		}
		stagesStorageCache, err := common.GetStagesStorageCache(synchronization)
		if err != nil {
			return err
		}

		stagesManager := stages_manager.NewStagesManager(projectName, storageLockManager, stagesStorageCache)
		if err := stagesManager.UseStagesStorage(ctx, stagesStorage); err != nil {
			return err
		}

		imagesRepo, err := common.GetImagesRepo(ctx, projectName, &commonCmdData)
		if err != nil {
			return err
		}

		imagesRepository = imagesRepo.String()

		tag, tagStrategy, err = common.GetDeployTag(&commonCmdData, common.TagOptionsGetterOptions{})
		if err != nil {
			return err
		}

		if err := ssh_agent.Init(ctx, *commonCmdData.SSHKeys); err != nil {
			return fmt.Errorf("cannot initialize ssh agent: %s", err)
		}
		defer func() {
			err := ssh_agent.Terminate()
			if err != nil {
				logboek.Warn().LogF("WARNING: ssh agent termination failed: %s\n", err)
			}
		}()

		logboek.LogOptionalLn()

		conveyorWithRetry := build.NewConveyorWithRetryWrapper(werfConfig, []string{}, projectDir, projectTmpDir, ssh_agent.SSHAuthSock, containerRuntime, stagesManager, imagesRepo, storageLockManager, common.GetConveyorOptions(&commonCmdData))
		defer conveyorWithRetry.Terminate()

		if err := conveyorWithRetry.WithRetryBlock(ctx, func(c *build.Conveyor) error {
			if err := c.ShouldBeBuilt(ctx, build.ShouldBeBuiltOptions{}); err != nil {
				return err
			}

			imagesInfoGetters = c.GetImageInfoGetters(werfConfig.StapelImages, werfConfig.ImagesFromDockerfile, tag, tagStrategy, false)
			return nil
		}); err != nil {
			return err
		}
	}

	releaseName, err := common.GetHelmRelease(*commonCmdData.Release, *commonCmdData.Environment, werfConfig)
	if err != nil {
		return err
	}

	namespace, err := common.GetKubernetesNamespace(*commonCmdData.Namespace, *commonCmdData.Environment, werfConfig)
	if err != nil {
		return err
	}

	userExtraAnnotations, err := common.GetUserExtraAnnotations(&commonCmdData)
	if err != nil {
		return err
	}

	userExtraLabels, err := common.GetUserExtraLabels(&commonCmdData)
	if err != nil {
		return err
	}

	logboek.LogOptionalLn()

	wc := werf_chart.NewWerfChart()
	wc.ValueOpts.ValueFiles = append(wc.ValueOpts.ValueFiles, *commonCmdData.Values...)
	wc.ValueOpts.StringValues = append(wc.ValueOpts.StringValues, *commonCmdData.SetString...)
	wc.ValueOpts.Values = append(wc.ValueOpts.Values, *commonCmdData.Set...)

	actionConfig := new(action.Configuration)

	*cmd_helm.Settings.GetNamespaceP() = namespace

	helmUpgradeCmd, _ := cmd_helm.NewUpgradeCmd(actionConfig, logboek.ProxyOutStream(), cmd_helm.UpgradeCmdOptions{
		PostRenderer:    wc.ExtraAnnotationsAndLabelsPostRenderer,
		ValueOpts:       wc.ValueOpts,
		CreateNamespace: NewBool(true),
		Install:         NewBool(true),
		Wait:            NewBool(true),
		Atomic:          NewBool(false),
	})

	if err := helm_v3.InitActionConfig(ctx, cmd_helm.Settings, actionConfig, helm_v3.InitActionConfigOptions{
		StatusProgressPeriod:      time.Duration(*commonCmdData.StatusProgressPeriodSeconds) * time.Second,
		HooksStatusProgressPeriod: time.Duration(*commonCmdData.HooksStatusProgressPeriodSeconds) * time.Second,
	}); err != nil {
		return err
	}

	werfChartInitOpts := werf_chart.WerfChartInitOptions{
		ReleaseName:       releaseName,
		ChartDir:          chartDir,
		ExtraAnnotations:  userExtraAnnotations,
		ExtraLabels:       userExtraLabels,
		SecretValuesFiles: *commonCmdData.SecretValues,
	}

	if m, err := deploy.GetSafeSecretManager(context.Background(), projectDir, werfChartInitOpts.ChartDir, *commonCmdData.SecretValues, *commonCmdData.IgnoreSecretKey); err != nil {
		return err
	} else {
		werfChartInitOpts.SecretsManager = m
	}

	if m, err := lock_manager.NewLockManager(namespace); err != nil {
		return fmt.Errorf("unable to create lock manager: %s", err)
	} else {
		werfChartInitOpts.LockManager = m
	}

	if err := wc.Init(werfChartInitOpts); err != nil {
		return fmt.Errorf("unable to init werf chart: %s", err)
	}

	_ = imagesRepository
	_ = *commonCmdData.Environment
	_ = cmdData.Timeout

	return wc.WrapUpgrade(context.Background(), func() error {
		return helmUpgradeCmd.RunE(helmUpgradeCmd, []string{releaseName, chartDir})
	})
}

func NewBool(value bool) *bool {
	res := new(bool)
	*res = value
	return res
}
