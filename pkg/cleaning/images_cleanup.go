package cleaning

import (
	"context"
	"fmt"
	"time"

	"github.com/werf/kubedog/pkg/kube"

	"github.com/fatih/color"
	"github.com/rodaine/table"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/werf/lockgate"
	"github.com/werf/logboek"
	"github.com/werf/logboek/pkg/style"
	"github.com/werf/logboek/pkg/types"

	"github.com/werf/werf/pkg/config"
	"github.com/werf/werf/pkg/image"
	"github.com/werf/werf/pkg/logging"
	"github.com/werf/werf/pkg/stages_manager"
	"github.com/werf/werf/pkg/storage"
	"github.com/werf/werf/pkg/werf"
)

type ImagesCleanupOptions struct {
	LocalGit                                GitRepo
	KubernetesContextClients                []*kube.ContextClient
	KubernetesNamespaceRestrictionByContext map[string]string
	WithoutKube                             bool
	GitHistoryBasedCleanupOptions           config.MetaCleanup
	DryRun                                  bool
}

func ImagesCleanup(ctx context.Context, projectName string, stagesManager *stages_manager.StagesManager, storageLockManager storage.LockManager, options ImagesCleanupOptions) error {
	m := newImagesCleanupManager(projectName, stagesManager, options)

	if lock, err := storageLockManager.LockStagesAndImages(ctx, projectName, storage.LockStagesAndImagesOptions{GetOrCreateImagesOnly: false}); err != nil {
		return fmt.Errorf("unable to lock stages and images: %s", err)
	} else {
		defer storageLockManager.Unlock(ctx, lock)
	}

	return logboek.Context(ctx).Default().LogProcess("Running images cleanup").
		Options(func(options types.LogProcessOptionsInterface) {
			options.Style(style.Highlight())
		}).
		DoError(func() error {
			return m.run(ctx)
		})
}

func newImagesCleanupManager(projectName string, stagesManager *stages_manager.StagesManager, options ImagesCleanupOptions) *imagesCleanupManager {
	return &imagesCleanupManager{
		ProjectName:                             projectName,
		StagesManager:                           stagesManager,
		DryRun:                                  options.DryRun,
		LocalGit:                                options.LocalGit,
		KubernetesContextClients:                options.KubernetesContextClients,
		KubernetesNamespaceRestrictionByContext: options.KubernetesNamespaceRestrictionByContext,
		WithoutKube:                             options.WithoutKube,
		GitHistoryBasedCleanupOptions:           options.GitHistoryBasedCleanupOptions,
	}
}

type imagesCleanupManager struct {
	stages []*image.StageDescription

	tagCommitHashes            map[string][]plumbing.Hash
	tagToCleanupCommitHashes   map[string][]plumbing.Hash
	tagNonexistentCommitHashes map[string][]plumbing.Hash
	nonexistentTagCommitHashes map[string][]plumbing.Hash

	ProjectName                             string
	StagesManager                           *stages_manager.StagesManager
	LocalGit                                GitRepo
	KubernetesContextClients                []*kube.ContextClient
	KubernetesNamespaceRestrictionByContext map[string]string
	WithoutKube                             bool
	GitHistoryBasedCleanupOptions           config.MetaCleanup
	DryRun                                  bool
}

type GitRepo interface {
	PlainOpen() (*git.Repository, error)
	IsCommitExists(ctx context.Context, commit string) (bool, error)
}

func (m *imagesCleanupManager) initMetadata(ctx context.Context) error {
	if err := m.initStages(ctx); err != nil { // TODO
		return err
	}

	if err := logboek.Context(ctx).Info().LogProcess("Fetching images metadata").DoError(func() error {
		return m.initTagCommitHashes(ctx)
	}); err != nil {
		return err
	}

	return nil
}

func (m *imagesCleanupManager) initStages(ctx context.Context) error {
	stages, err := m.StagesManager.GetAllStages(ctx)
	if err != nil {
		return err
	}

	m.stages = stages
	return nil
}

func (m *imagesCleanupManager) deleteStage(stagesToDelete ...*image.StageDescription) {
	var resultStages []*image.StageDescription

	outerLoop:
	for _, stage := range m.stages {
		for _, stageToDelete := range stagesToDelete {
			if stage == stageToDelete {
			continue outerLoop
			}
		}

		resultStages = append(resultStages, stage)
	}

	m.stages = resultStages
}

func (m *imagesCleanupManager) isStageExist(tag string) bool {
	stage := m.getStage(tag)
	return stage != nil
}

func (m *imagesCleanupManager) getStage(tag string) *image.StageDescription  {
	for _, stage := range m.stages {
		if tag == stage.Info.Tag {
			return stage
		}
	}

	return nil
}

func (m *imagesCleanupManager) initTagCommitHashes(ctx context.Context) error {
	m.tagCommitHashes = map[string][]plumbing.Hash{}
	m.tagToCleanupCommitHashes = map[string][]plumbing.Hash{}
	m.tagNonexistentCommitHashes = map[string][]plumbing.Hash{}
	m.nonexistentTagCommitHashes = map[string][]plumbing.Hash{}

	metaTagCommits, err := m.StagesManager.StagesStorage.GetCommitsByMetaTag(ctx, m.ProjectName)
	if err != nil {
		return err
	}

	for metaTag, commits := range metaTagCommits {
		var commitHashes []plumbing.Hash
		var nonexistentCommitHashes []plumbing.Hash

		for _, commit := range commits {
			exist, err := m.LocalGit.IsCommitExists(ctx, commit)
			if err != nil {
				return fmt.Errorf("") // TODO
			}

			commitHash := plumbing.NewHash(commit)
			if exist {
				commitHashes = append(commitHashes, commitHash)
			} else {
				nonexistentCommitHashes = append(nonexistentCommitHashes, commitHash)
			}

		}

		if m.isStageExist(metaTag) {
			if len(commitHashes) != 0 {
				m.tagCommitHashes[metaTag] = commitHashes
				m.tagToCleanupCommitHashes[metaTag] = commitHashes
			}

			if len(nonexistentCommitHashes) != 0 {
				m.tagNonexistentCommitHashes[metaTag] = nonexistentCommitHashes
			}
		} else if len(commitHashes) != 0 {
			m.nonexistentTagCommitHashes[metaTag] = commitHashes
		}
	}

	return nil
}

func (m *imagesCleanupManager) getTagList() []string {
	var list []string
	for tag, _ := range m.tagCommitHashes {
		list = append(list, tag)
	}

	return list
}

func (m *imagesCleanupManager) skipTag(tags ...string) {
	for _, tag := range tags {
		delete(m.tagToCleanupCommitHashes, tag)
	}
}

func (m *imagesCleanupManager) setImageCommitHashImageMetadata(imageCommitHashImageMetadata map[string]map[plumbing.Hash]*storage.ImageMetadata) {
	m.imageCommitHashImageMetadata = &imageCommitHashImageMetadata
}

func (m *imagesCleanupManager) updateImageCommitImageMetadata(imageName string, commitHashesToExclude ...plumbing.Hash) {
	commitImageMetadata, ok := (*m.imageCommitHashImageMetadata)[imageName]
	if !ok {
		return
	}

	resultImage := map[plumbing.Hash]*storage.ImageMetadata{}
outerLoop:
	for commitHash, imageMetadata := range commitImageMetadata {
		for _, commitHashToExclude := range commitHashesToExclude {
			if commitHash == commitHashToExclude {
				continue outerLoop
			}
		}

		resultImage[commitHash] = imageMetadata
	}

	(*m.imageCommitHashImageMetadata)[imageName] = resultImage
}

func (m *imagesCleanupManager) run(ctx context.Context) error {
	imagesCleanupLockName := fmt.Sprintf("images-cleanup.%s", "trololo") // TODO
	return werf.WithHostLock(ctx, imagesCleanupLockName, lockgate.AcquireOptions{Timeout: time.Second * 600}, func() error {
		if err := logboek.Context(ctx).LogProcess("Fetching repo images data").DoError(func() error {
			return m.initMetadata(ctx)
		}); err != nil {
			return err
		}

		if m.LocalGit == nil {
			logboek.Context(ctx).Default().LogLnDetails("Images cleanup skipped due to local git repository was not detected")
			return nil
		}

		if !m.WithoutKube {
			if err := logboek.Context(ctx).LogProcess("Skipping repo images that are being used in Kubernetes").DoError(func() error {
				return m.skipTagsThatAreUsedInKubernetes(ctx)
			}); err != nil {
				return err
			}
		}

		if err := m.gitHistoryBasedCleanup(ctx); err != nil {
			return err
		}

		return nil
	})
}

func (m *imagesCleanupManager) skipTagsThatAreUsedInKubernetes(ctx context.Context) error {
	deployedDockerImagesNames, err := getDeployedDockerImagesNames(ctx, m.KubernetesContextClients, m.KubernetesNamespaceRestrictionByContext)
	if err != nil {
		return err
	}

Loop:
	for _, tag := range m.getTagList() {
		dockerImageName := fmt.Sprintf("%s:%s", m.StagesManager.StagesStorage.String(), tag)
		for _, deployedDockerImageName := range deployedDockerImagesNames {
			if deployedDockerImageName == dockerImageName {
				m.skipTag(tag)
				logboek.Context(ctx).Default().LogFDetails("  tag: %s\n", tag)
				logboek.Context(ctx).LogOptionalLn()
				continue Loop
			}
		}
	}

	return nil
}

func getDeployedDockerImagesNames(ctx context.Context, kubernetesContextClients []*kube.ContextClient, kubernetesNamespaceRestrictionByContext map[string]string) ([]string, error) {
	var deployedDockerImagesNames []string
	for _, contextClient := range kubernetesContextClients {
		if err := logboek.Context(ctx).LogProcessInline("Getting deployed docker images (context %s)", contextClient.ContextName).
			DoError(func() error {
				kubernetesClientDeployedDockerImagesNames, err := deployedDockerImages(contextClient.Client, kubernetesNamespaceRestrictionByContext[contextClient.ContextName])
				if err != nil {
					return fmt.Errorf("cannot get deployed imagesRepoImageList: %s", err)
				}

				deployedDockerImagesNames = append(deployedDockerImagesNames, kubernetesClientDeployedDockerImagesNames...)

				return nil
			}); err != nil {
			return nil, err
		}
	}

	return deployedDockerImagesNames, nil
}

func (m *imagesCleanupManager) gitHistoryBasedCleanup(ctx context.Context) error {
	gitRepository, err := m.LocalGit.PlainOpen()
	if err != nil {
		return fmt.Errorf("git plain open failed: %s", err)
	}

	var referencesToScan []*referenceToScan
	if err := logboek.Context(ctx).Default().LogProcess("Preparing references to scan").DoError(func() error {
		referencesToScan, err = getReferencesToScan(ctx, gitRepository, m.GitHistoryBasedCleanupOptions.KeepPolicies)
		return err
	}); err != nil {
		return err
	}

	if err := logboek.Context(ctx).Info().LogProcess("Grouping repo images tags by content signature").DoError(func() error { // TODO
		if logboek.Context(ctx).Info().IsAccepted() {
			var rows [][]interface{}
			for tag, commitHashes := range m.tagToCleanupCommitHashes {
				if len(commitHashes) == 0 {
					continue
				}

				maxInd := len(commitHashes)

				shortify := func(column string, maxLength int) string {
					if maxLength < 0 {
						maxLength = 5
					}

					if len(column) > 15 {
						return fmt.Sprintf("%s..%s", column[:10], column[len(column)-3:])
					} else {
						return column
					}
				}

				const commitLength = 40
				for ind := 0; ind < maxInd; ind++ {
					var columns []interface{}
					if ind == 0 {
						columns = append(columns, shortify(tag, logboek.Context(ctx).Streams().ContentWidth()-commitLength))
					} else {
						columns = append(columns, "")
					}

					if len(commitHashes) > ind {
						columns = append(columns, shortify(commitHashes[ind].String()))
					} else {
						columns = append(columns, "")
					}

					rows = append(rows, columns)
				}

				if len(rows) != 0 {
					tbl := table.New("Tag", "Commits")
					tbl.WithWriter(logboek.Context(ctx).ProxyOutStream())
					tbl.WithHeaderFormatter(color.New(color.Underline).SprintfFunc())
					for _, row := range rows {
						tbl.AddRow(row...)
					}
					tbl.Print()

					logboek.Context(ctx).LogOptionalLn()
				}

				for tag, repoImageListToCleanup := range tagRepoImageListToCleanup {
					commitHashes := imageTagExistingCommitHashes[imageName][tag]
					if len(commitHashes) == 0 && len(repoImageListToCleanup) != 0 {
						logBlockMessage := fmt.Sprintf("Content signature %s is associated with non-existing commits. The following tags will be deleted", tag)
						logboek.Context(ctx).Info().LogBlock(logBlockMessage).Do(func() {
							for _, repoImage := range repoImageListToCleanup {
								logboek.Context(ctx).Info().LogFDetails("  tag: %s\n", repoImage.Tag)
								logboek.Context(ctx).LogOptionalLn()
							}
						})
					}
				}

				logProcess.End()
			}
		}

		return nil
	}); err != nil {
		return err
	}

	if err := logboek.Context(ctx).Default().LogProcess("Git history based cleanup").
		Options(func(options types.LogProcessOptionsInterface) {
			options.Style(style.Highlight())
		}).
		DoError(func() error {
					var reachedTagList []string
					if err := logboek.Context(ctx).LogProcess("Scanning git references history").DoError(func() error {
						if len(m.tagToCleanupCommitHashes) != 0 {
							reachedTagList, err = scanReferencesHistory(ctx, gitRepository, referencesToScan, m.tagToCleanupCommitHashes)
							if err != nil {
								return err
							}
						} else {
							logboek.Context(ctx).LogLn("Scanning stopped due to nothing to seek")
						}

						return nil
					}); err != nil {
						return err
					}

					if len(reachedTagList) != 0 {
						logboek.Context(ctx).Default().LogBlock("Saved tags").Do(func() {
							for _, tag := range reachedTagList {
								logboek.Context(ctx).Default().LogFDetails("  tag: %s\n", tag)
								logboek.Context(ctx).LogOptionalLn()
							}
						})
					}

					var metaTagsToDelete []string
					var stagesToDelete []*image.StageDescription

				outerLoop:
					for tag, _ := range m.tagToCleanupCommitHashes {
						for _, reachedTag := range reachedTagList {
							if tag == reachedTag {
								continue outerLoop
							}
						}

						metaTagsToDelete = append(metaTagsToDelete, tag)

						stage := m.getStage(tag)
						if stage == nil {
							panic(fmt.Sprintf("runtime error: stage was not found in memory by tag %s", tag))
						}
						stagesToDelete = append(stagesToDelete, stage)
					}

					if len(stagesToDelete) != 0 {
						if err := logboek.Context(ctx).Default().LogProcess("Deleting tags").DoError(func() error {
							return m.deleteStageInStagesStorage(ctx, stagesToDelete...)
						}); err != nil {
							return err
						}
					}

					if len(metaTagsToDelete) != 0 || len(m.nonexistentTagCommitHashes) != 0 || len(m.tagNonexistentCommitHashes) != 0 {
						if err := logboek.Context(ctx).Default().LogProcess("Deleting meta images").DoError(func() error {
							for _, metaTag := range metaTagsToDelete {

							}


						}); err != nil {
							return err
						}
					}

					if len(commitHashesToCleanup) != 0 {
						logProcess := logboek.Context(ctx).Default().LogProcess("Cleaning up images metadata")
						logProcess.Start()

						if err := deleteMetaImagesInStagesStorage(ctx, m.StagesManager.StagesStorage, m.ProjectName, imageName, m.DryRun, commitHashesToCleanup...); err != nil {
							logProcess.Fail()
							return err
						}

						m.updateImageCommitImageMetadata(imageName, commitHashesToCleanup...)

						logProcess.End()
					}

					return nil
				}); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
		return nil, err
	}

	if err := logboek.Context(ctx).Default().LogProcess("Deleting unused images metadata").DoError(func() error {
		imageUnusedCommitHashes, err := m.getImageUnusedCommitHashes(resultTags)
		if err != nil {
			return err
		}

		for imageName, unusedCommitHashes := range imageUnusedCommitHashes {
			logProcess := logboek.Context(ctx).Default().LogProcess(logging.ImageLogProcessName(imageName, false))
			logProcess.Start()

			if err := deleteMetaImagesInStagesStorage(ctx, m.StagesManager.StagesStorage, m.ProjectName, imageName, m.DryRun, unusedCommitHashes...); err != nil {
				logProcess.Fail()
				return err
			}

			m.updateImageCommitImageMetadata(imageName, unusedCommitHashes...)

			logProcess.End()
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return resultTags, nil
}

// getImageTagRepoImageListToCleanup groups images, content signatures and repo images tags to clean up.
// The map has all content signatures and each value is related to repo images tags to clean up and can be empty.
// Repo images tags to clean up are all tags for particular werf.yaml image except for ones which are using in Kubernetes.
func (m *imagesCleanupManager) getImageTagRepoImageListToCleanup(repoImagesToCleanup map[string][]*image.Info) (map[string]map[string][]*image.Info, error) {
	imageTagRepoImageListToCleanup := map[string]map[string][]*image.Info{}

	imageCommitHashImageMetadata := m.getTagCommitHashes()
	for imageName, repoImageListToCleanup := range repoImagesToCleanup {
		imageTagRepoImageListToCleanup[imageName] = map[string][]*image.Info{}

		for _, imageMetadata := range imageCommitHashImageMetadata[imageName] {
			_, ok := imageTagRepoImageListToCleanup[imageName][imageMetadata.Tag]
			if ok {
				continue
			}

			var repoImageListToCleanupBySignature []*image.Info
			for _, repoImage := range repoImageListToCleanup {
				if repoImage.Labels[image.WerfTagLabel] == imageMetadata.Tag {
					repoImageListToCleanupBySignature = append(repoImageListToCleanupBySignature, repoImage)
				}
			}

			imageTagRepoImageListToCleanup[imageName][imageMetadata.Tag] = repoImageListToCleanupBySignature
		}
	}

	return imageTagRepoImageListToCleanup, nil
}

func (m *imagesCleanupManager) getImageUnusedCommitHashes(resultImageRepoImageList map[string][]*image.Info) (map[string][]plumbing.Hash, error) {
	unusedImageCommitHashes := map[string][]plumbing.Hash{}

	for imageName, commitHashImageMetadata := range m.getTagCommitHashes() {
		var unusedCommitHashes []plumbing.Hash
		repoImageList, ok := resultImageRepoImageList[imageName]
		if !ok {
			repoImageList = []*image.Info{}
		}

	outerLoop:
		for commitHash, imageMetadata := range commitHashImageMetadata {
			for _, repoImage := range repoImageList {
				if repoImage.Labels[image.WerfTagLabel] == imageMetadata.Tag {
					continue outerLoop
				}
			}

			unusedCommitHashes = append(unusedCommitHashes, commitHash)
		}

		unusedImageCommitHashes[imageName] = unusedCommitHashes
	}

	return unusedImageCommitHashes, nil
}

func (m *imagesCleanupManager) getImageRepoImageListWithoutRelatedImageMetadata(imageRepoImageListToCleanup map[string][]*image.Info, imageTagRepoImageListToCleanup map[string]map[string][]*image.Info) (map[string][]*image.Info, error) {
	imageRepoImageListWithoutRelatedCommit := map[string][]*image.Info{}

	for imageName, repoImageListToCleanup := range imageRepoImageListToCleanup {
		unusedRepoImageList := repoImageListToCleanup

		tagRepoImageListToCleanup, ok := imageTagRepoImageListToCleanup[imageName]
		if !ok {
			tagRepoImageListToCleanup = map[string][]*image.Info{}
		}

		for _, filteredRepoImageListToCleanup := range tagRepoImageListToCleanup {
			unusedRepoImageList = exceptRepoImageList(unusedRepoImageList, filteredRepoImageListToCleanup...)
		}

		imageRepoImageListWithoutRelatedCommit[imageName] = unusedRepoImageList
	}

	return imageRepoImageListWithoutRelatedCommit, nil
}

// getImageTagExistingCommitHashes groups images, content signatures and commit hashes which exist in the git repo.
func (m *imagesCleanupManager) getImageTagExistingCommitHashes(ctx context.Context) (map[string]map[string][]plumbing.Hash, error) {
	imageTagCommitHashes := map[string]map[string][]plumbing.Hash{}

	for _, imageName := range m.ImageNameList {
		imageTagCommitHashes[imageName] = map[string][]plumbing.Hash{}

		for commitHash, imageMetadata := range m.getTagCommitHashes()[imageName] {
			commitHashes, ok := imageTagCommitHashes[imageName][imageMetadata.Tag]
			if !ok {
				commitHashes = []plumbing.Hash{}
			}

			exist, err := m.LocalGit.IsCommitExists(ctx, commitHash.String())
			if err != nil {
				return nil, fmt.Errorf("check git commit existence failed: %s", err)
			}

			if exist {
				commitHashes = append(commitHashes, commitHash)
				imageTagCommitHashes[imageName][imageMetadata.Tag] = commitHashes
			}
		}
	}

	return imageTagCommitHashes, nil
}

func deployedDockerImages(kubernetesClient kubernetes.Interface, kubernetesNamespace string) ([]string, error) {
	var deployedDockerImages []string

	images, err := getPodsImages(kubernetesClient, kubernetesNamespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get Pods images: %s", err)
	}

	deployedDockerImages = append(deployedDockerImages, images...)

	images, err = getReplicationControllersImages(kubernetesClient, kubernetesNamespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get ReplicationControllers images: %s", err)
	}

	deployedDockerImages = append(deployedDockerImages, images...)

	images, err = getDeploymentsImages(kubernetesClient, kubernetesNamespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get Deployments images: %s", err)
	}

	deployedDockerImages = append(deployedDockerImages, images...)

	images, err = getStatefulSetsImages(kubernetesClient, kubernetesNamespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get StatefulSets images: %s", err)
	}

	deployedDockerImages = append(deployedDockerImages, images...)

	images, err = getDaemonSetsImages(kubernetesClient, kubernetesNamespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get DaemonSets images: %s", err)
	}

	deployedDockerImages = append(deployedDockerImages, images...)

	images, err = getReplicaSetsImages(kubernetesClient, kubernetesNamespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get ReplicaSets images: %s", err)
	}

	deployedDockerImages = append(deployedDockerImages, images...)

	images, err = getCronJobsImages(kubernetesClient, kubernetesNamespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get CronJobs images: %s", err)
	}

	deployedDockerImages = append(deployedDockerImages, images...)

	images, err = getJobsImages(kubernetesClient, kubernetesNamespace)
	if err != nil {
		return nil, fmt.Errorf("cannot get Jobs images: %s", err)
	}

	deployedDockerImages = append(deployedDockerImages, images...)

	return deployedDockerImages, nil
}

func getPodsImages(kubernetesClient kubernetes.Interface, kubernetesNamespace string) ([]string, error) {
	var images []string
	list, err := kubernetesClient.CoreV1().Pods(kubernetesNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, pod := range list.Items {
		for _, container := range pod.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return images, nil
}

func getReplicationControllersImages(kubernetesClient kubernetes.Interface, kubernetesNamespace string) ([]string, error) {
	var images []string
	list, err := kubernetesClient.CoreV1().ReplicationControllers(kubernetesNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, replicationController := range list.Items {
		for _, container := range replicationController.Spec.Template.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return images, nil
}

func getDeploymentsImages(kubernetesClient kubernetes.Interface, kubernetesNamespace string) ([]string, error) {
	var images []string
	list, err := kubernetesClient.AppsV1().Deployments(kubernetesNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, deployment := range list.Items {
		for _, container := range deployment.Spec.Template.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return images, nil
}

func getStatefulSetsImages(kubernetesClient kubernetes.Interface, kubernetesNamespace string) ([]string, error) {
	var images []string
	list, err := kubernetesClient.AppsV1().StatefulSets(kubernetesNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, statefulSet := range list.Items {
		for _, container := range statefulSet.Spec.Template.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return images, nil
}

func getDaemonSetsImages(kubernetesClient kubernetes.Interface, kubernetesNamespace string) ([]string, error) {
	var images []string
	list, err := kubernetesClient.AppsV1().DaemonSets(kubernetesNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, daemonSets := range list.Items {
		for _, container := range daemonSets.Spec.Template.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return images, nil
}

func getReplicaSetsImages(kubernetesClient kubernetes.Interface, kubernetesNamespace string) ([]string, error) {
	var images []string
	list, err := kubernetesClient.AppsV1().ReplicaSets(kubernetesNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, replicaSet := range list.Items {
		for _, container := range replicaSet.Spec.Template.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return images, nil
}

func getCronJobsImages(kubernetesClient kubernetes.Interface, kubernetesNamespace string) ([]string, error) {
	var images []string
	list, err := kubernetesClient.BatchV1beta1().CronJobs(kubernetesNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, cronJob := range list.Items {
		for _, container := range cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return images, nil
}

func getJobsImages(kubernetesClient kubernetes.Interface, kubernetesNamespace string) ([]string, error) {
	var images []string
	list, err := kubernetesClient.BatchV1().Jobs(kubernetesNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, job := range list.Items {
		for _, container := range job.Spec.Template.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return images, nil
}

func deleteMetaImagesInStagesStorage(ctx context.Context, stagesStorage storage.StagesStorage, projectName, imageName string, dryRun bool, commitHashes ...plumbing.Hash) error {
	for _, commitHash := range commitHashes {
		if dryRun {
			logboek.Context(ctx).Info().LogLn(commitHash)
		} else {
			if err := stagesStorage.RmImageCommit(ctx, projectName, imageName, commitHash.String()); err != nil {
				logboek.Context(ctx).Warn().LogF(
					"WARNING: Metadata image deletion (image %s, commit: %s) failed: %s\n",
					logging.ImageLogName(imageName, false), commitHash.String(), err,
				)
				logboek.Context(ctx).LogOptionalLn()
			}
		}
	}

	return nil
}
