package cleaning

import (
	"context"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/rodaine/table"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/werf/kubedog/pkg/kube"
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

type CleanupOptions struct {
	LocalGit                                GitRepo
	KubernetesContextClients                []*kube.ContextClient
	KubernetesNamespaceRestrictionByContext map[string]string
	WithoutKube                             bool
	GitHistoryBasedCleanupOptions           config.MetaCleanup
	DryRun                                  bool
}

func Cleanup(ctx context.Context, projectName string, stagesManager *stages_manager.StagesManager, storageLockManager storage.LockManager, options CleanupOptions) error {
	m := newCleanupManager(projectName, stagesManager, options)

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

func newCleanupManager(projectName string, stagesManager *stages_manager.StagesManager, options CleanupOptions) *cleanupManager {
	return &cleanupManager{
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

type cleanupManager struct {
	stageImageList []*image.StageDescription

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

func (m *cleanupManager) init(ctx context.Context) error {
	if err := logboek.Context(ctx).Info().LogProcess("Fetching repo images manifests").DoError(func() error { // TODO
		return m.initStageImageList(ctx)
	}); err != nil {
		return err
	}

	if err := logboek.Context(ctx).Info().LogProcess("Fetching metadata").DoError(func() error { // TODO
		return m.initTagCommitHashes(ctx)
	}); err != nil {
		return err
	}

	return nil
}

func (m *cleanupManager) initStageImageList(ctx context.Context) error {
	stageImageList, err := m.StagesManager.GetAllStages(ctx)
	if err != nil {
		return err
	}

	m.stageImageList = stageImageList

	return nil
}

func (m *cleanupManager) isStageImageExist(tag string) bool {
	stageImage := m.getStageImage(tag)
	return stageImage != nil
}

func (m *cleanupManager) mustGetStageImage(tag string) *image.StageDescription {
	stageImage := m.getStageImage(tag)

	if stageImage == nil {
		panic(fmt.Sprintf("runtime error: stage was not found in memory by tag %s", tag))
	}

	return stageImage
}

func (m *cleanupManager) getStageImage(tag string) *image.StageDescription {
	for _, stageImage := range m.stageImageList {
		if tag == stageImage.Info.Tag {
			return stageImage
		}
	}

	return nil
}

func (m *cleanupManager) deleteStageImageFromCache(stageImageListToDelete ...*image.StageDescription) {
	var resultStageImageList []*image.StageDescription

outerLoop:
	for _, stageImage := range m.stageImageList {
		for _, stageImageToDelete := range stageImageListToDelete {
			if stageImage == stageImageToDelete {
				continue outerLoop
			}
		}

		resultStageImageList = append(resultStageImageList, stageImage)
	}

	m.stageImageList = resultStageImageList
}

func (m *cleanupManager) initTagCommitHashes(ctx context.Context) error {
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
				return fmt.Errorf("check commit %s in local git failed: %s", commit, err)
			}

			commitHash := plumbing.NewHash(commit)
			if exist {
				commitHashes = append(commitHashes, commitHash)
			} else {
				nonexistentCommitHashes = append(nonexistentCommitHashes, commitHash)
			}

		}

		if m.isStageImageExist(metaTag) {
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

func (m *cleanupManager) skipTag(tags ...string) {
	for _, tag := range tags {
		delete(m.tagToCleanupCommitHashes, tag)
	}
}

func (m *cleanupManager) deleteTagFromCache(tags ...string) {
	for _, tag := range tags {
		delete(m.tagCommitHashes, tag)
		delete(m.tagToCleanupCommitHashes, tag)
	}
}

func (m *cleanupManager) run(ctx context.Context) error {
	cleanupLockName := fmt.Sprintf("cleanup.%s", m.ProjectName)
	return werf.WithHostLock(ctx, cleanupLockName, lockgate.AcquireOptions{Timeout: time.Second * 600}, func() error {
		if err := logboek.Context(ctx).LogProcess("Fetching manifests from repo").DoError(func() error { // TODO
			return m.init(ctx)
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

		if err := m.cleanupUnusedStages(ctx); err != nil {
			return err
		}

		return nil
	})
}

func (m *cleanupManager) skipTagsThatAreUsedInKubernetes(ctx context.Context) error {
	deployedDockerImagesNames, err := m.deployedDockerImagesNames(ctx)
	if err != nil {
		return err
	}

Loop:
	for tag, _ := range m.tagCommitHashes {
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

func (m *cleanupManager) deployedDockerImagesNames(ctx context.Context) ([]string, error) {
	var deployedDockerImagesNames []string
	for _, contextClient := range m.KubernetesContextClients {
		if err := logboek.Context(ctx).LogProcessInline("Getting deployed docker images (context %s)", contextClient.ContextName).
			DoError(func() error {
				kubernetesClientDeployedDockerImagesNames, err := deployedDockerImages(contextClient.Client, m.KubernetesNamespaceRestrictionByContext[contextClient.ContextName])
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

func (m *cleanupManager) gitHistoryBasedCleanup(ctx context.Context) error {
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

	if logboek.Context(ctx).Info().IsAccepted() && logboek.Context(ctx).Streams().Width() > 90 {
		m.printTagCommitHashes(ctx)
	}

	return logboek.Context(ctx).Default().LogProcess("Git history based cleanup").
		Options(func(options types.LogProcessOptionsInterface) {
			options.Style(style.Highlight())
		}).
		DoError(func() error {
			var reachedTagList []string
			var tagHitCommitHashes map[string][]plumbing.Hash

			if err := logboek.Context(ctx).LogProcess("Scanning git references history").DoError(func() error {
				if len(m.tagToCleanupCommitHashes) != 0 {
					reachedTagList, tagHitCommitHashes, err = scanReferencesHistory(ctx, gitRepository, referencesToScan, m.tagToCleanupCommitHashes)
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
			var stageImageListToDelete []*image.StageDescription

		outerLoop:
			for tag, _ := range m.tagToCleanupCommitHashes {
				for _, reachedTag := range reachedTagList {
					if tag == reachedTag {
						continue outerLoop
					}
				}

				metaTagsToDelete = append(metaTagsToDelete, tag)

				stageImageListToDelete = append(stageImageListToDelete, m.mustGetStageImage(tag))
			}

			if len(stageImageListToDelete) != 0 {
				if err := logboek.Context(ctx).Default().LogProcess("Deleting tags").DoError(func() error {
					return m.deleteStageInStagesStorage(ctx, stageImageListToDelete...)
				}); err != nil {
					return err
				}
			}

			if len(tagHitCommitHashes) != 0 || len(metaTagsToDelete) != 0 || len(m.nonexistentTagCommitHashes) != 0 || len(m.tagNonexistentCommitHashes) != 0 {
				if err := logboek.Context(ctx).Default().LogProcess("Deleting meta images").DoError(func() error {
					return  m.deleteMetadata(ctx, metaTagsToDelete, tagHitCommitHashes)
				}); err != nil {
					return err
				}
			}

		return nil
	})
}

func (m *cleanupManager) printTagCommitHashes(ctx context.Context) {
	var rows [][]interface{}
	for tag, commitHashes := range m.tagToCleanupCommitHashes {
		if len(commitHashes) == 0 {
			continue
		}

		tagLength := len(tag)
		commitLength := len(commitHashes[0])

		contentWidth := logboek.Context(ctx).Streams().ContentWidth()
		if contentWidth < tagLength + commitLength + 1 {
			commitLength = contentWidth - tagLength - 1
		}

		maxInd := len(commitHashes)
		shortify := func(column string, maxLength int) string {
			if len(column) > maxLength {
				return fmt.Sprintf("%s..%s", column[:maxLength-5], column[maxLength-3:])
			} else {
				return column
			}
		}

		for ind := 0; ind < maxInd; ind++ {
			var columns []interface{}
			if ind == 0 {
				columns = append(columns, shortify(tag, tagLength))
			} else {
				columns = append(columns, "")
			}

			columns = append(columns, shortify(commitHashes[ind].String(), commitLength))

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
	}
}

func (m *cleanupManager) deleteMetadata(ctx context.Context, metaTagsToDelete []string, tagHitCommitHashes map[string][]plumbing.Hash) error {
	if len(metaTagsToDelete) != 0 {
		if err := logboek.Context(ctx).Info().LogProcess("Deleting metadata related to deleted tags").DoError(func() error {
		tagToCleanupCommitHashesLoop:
			for tag, commitHashes := range m.tagToCleanupCommitHashes {
				for _, metaTagToDelete := range metaTagsToDelete {
					if metaTagToDelete == tag {
						if err := m.deleteMetadataInStagesStorage(ctx, tag, commitHashes...); err != nil {
							return fmt.Errorf("delete metadata in stages storage failed: %s", err)
						}

						m.deleteTagFromCache(tag)

						continue tagToCleanupCommitHashesLoop
					}
				}
			}

			return nil
		}); err != nil {
			return err
		}
	}

	if len(tagHitCommitHashes) != 0 {
		if err := logboek.Context(ctx).Info().LogProcess("Cleaning up redundant metadata").DoError(func() error {
			for tag, commitHashes := range m.tagToCleanupCommitHashes {
				hitCommitHashes, ok := tagHitCommitHashes[tag]
				if !ok {
					continue
				}

			commitHashesLoop:
				for _, commitHash := range commitHashes {
					for _, hitCommitHash := range hitCommitHashes {
						if commitHash == hitCommitHash {
							continue commitHashesLoop
						}
					}

					if err := m.deleteMetadataInStagesStorage(ctx, tag, commitHash); err != nil {
						return fmt.Errorf("delete metadata in stages storage failed: %s", err)
					}
				}
			}

			return nil
		}); err != nil {
			return err
		}
	}

	if len(m.nonexistentTagCommitHashes) != 0 {
		if err := logboek.Context(ctx).Info().LogProcess("Deleted metadata related to nonexistent tags").DoError(func() error {
			for nonexistentTag, commitHashes := range m.nonexistentTagCommitHashes {
				if err := m.deleteMetadataInStagesStorage(ctx, nonexistentTag, commitHashes...); err != nil {
					return fmt.Errorf("delete metadata in stages storage failed: %s", err)
				}
			}

			m.nonexistentTagCommitHashes = map[string][]plumbing.Hash{}

			return nil
		}); err != nil {
			return err
		}
	}

	if len(m.tagNonexistentCommitHashes) != 0 {
		if err := logboek.Context(ctx).Info().LogProcess("Deleted metadata related to nonexistent commits").DoError(func() error {
			for tag, nonexistentCommitHashes := range m.tagNonexistentCommitHashes {
				if err := m.deleteMetadataInStagesStorage(ctx, tag, nonexistentCommitHashes...); err != nil {
					return fmt.Errorf("delete metadata in stages storage failed: %s", err)
				}
			}

			m.tagNonexistentCommitHashes = map[string][]plumbing.Hash{}

			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}

func (m *cleanupManager) deleteMetadataInStagesStorage(ctx context.Context, metaTag string, commitHashes ...plumbing.Hash) error {
	for _, commitHash := range commitHashes {
		if m.DryRun {
			logboek.Context(ctx).Info().LogLn(commitHash)
		} else {
			if err := m.StagesManager.StagesStorage.RmContentSignatureCommit(ctx, m.ProjectName, metaTag, commitHash.String()); err != nil {
				logboek.Context(ctx).Warn().LogF(
					"WARNING: Metadata image deletion (image %s, commit: %s) failed: %s\n",
					logging.ImageLogName(metaTag, false), commitHash.String(), err,
				)
				logboek.Context(ctx).LogOptionalLn()
			}
		}
	}

	return nil
}
