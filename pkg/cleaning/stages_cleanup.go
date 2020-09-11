package cleaning

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/werf/lockgate"
	"github.com/werf/logboek"
	"github.com/werf/logboek/pkg/style"
	"github.com/werf/logboek/pkg/types"

	"github.com/werf/werf/pkg/docker_registry"
	"github.com/werf/werf/pkg/image"
	"github.com/werf/werf/pkg/stages_manager"
	"github.com/werf/werf/pkg/storage"
	"github.com/werf/werf/pkg/werf"
)

const stagesCleanupDefaultIgnorePeriodPolicy = 2 * 60 * 60

type StagesCleanupOptions struct {
	DryRun bool
}

func StagesCleanup(ctx context.Context, projectName string, imagesRepo storage.ImagesRepo, stagesManager *stages_manager.StagesManager, storageLockManager storage.LockManager, options StagesCleanupOptions) error {
	m := newStagesCleanupManager(projectName, imagesRepo, stagesManager, options)

	if lock, err := storageLockManager.LockStagesAndImages(ctx, projectName, storage.LockStagesAndImagesOptions{GetOrCreateImagesOnly: false}); err != nil {
		return fmt.Errorf("unable to lock stages and images: %s", err)
	} else {
		defer storageLockManager.Unlock(ctx, lock)
	}

	return logboek.Context(ctx).Default().LogProcess("Running stages cleanup").
		Options(func(options types.LogProcessOptionsInterface) {
			options.Style(style.Highlight())
		}).
		DoError(func() error {
			return m.run(ctx)
		})
}

func newStagesCleanupManager(projectName string, stagesManager *stages_manager.StagesManager, options StagesCleanupOptions) *stagesCleanupManager {
	return &stagesCleanupManager{
		StagesManager: stagesManager,
		ProjectName:   projectName,
		DryRun:        options.DryRun,
	}
}

type stagesCleanupManager struct {
	imagesRepoImageList *[]*image.Info

	StagesManager *stages_manager.StagesManager
	ProjectName   string
	DryRun        bool
}

func (m *stagesCleanupManager) initImagesRepoImageList(ctx context.Context) error {
	repoImages, err := selectRepoImagesFromImagesRepo(ctx, m.ImagesRepo, m.ImageNameList)
	if err != nil {
		return err
	}

	m.setImagesRepoImageList(flattenRepoImages(repoImages))

	return nil
}

func (m *stagesCleanupManager) setImagesRepoImageList(repoImageList []*image.Info) {
	m.imagesRepoImageList = &repoImageList
}

func (m *stagesCleanupManager) getOrInitImagesRepoImageList(ctx context.Context) ([]*image.Info, error) {
	if m.imagesRepoImageList == nil {
		if err := m.initImagesRepoImageList(ctx); err != nil {
			return nil, err
		}
	}

	return *m.imagesRepoImageList, nil
}

func (m *stagesCleanupManager) run(ctx context.Context) error {
	deleteImageOptions := storage.DeleteImageOptions{
		RmiForce:      false,
		SkipUsedImage: true,
		RmForce:       false,
	}

	lockName := fmt.Sprintf("stages-cleanup.%s-%s", m.StagesManager.StagesStorage.String(), m.ProjectName)
	return werf.WithHostLock(ctx, lockName, lockgate.AcquireOptions{Timeout: time.Second * 600}, func() error {
		var stagesImageList []*image.Info
		stagesByImageName := map[string]*image.StageDescription{}

		if err := logboek.Context(ctx).Default().LogProcess("Fetching stages").DoError(func() error {
			stages, err := m.StagesManager.GetAllStages(ctx)
			if err != nil {
				return err
			}

			for _, stageDesc := range stages {
				stagesImageList = append(stagesImageList, stageDesc.Info)
				stagesByImageName[stageDesc.Info.Name] = stageDesc
			}

			return nil
		}); err != nil {
			return err
		}

		var repoImageList []*image.Info
		var err error

		if err := logboek.Context(ctx).Default().LogProcess("Fetching repo images").DoError(func() error {
			repoImageList, err = m.getOrInitImagesRepoImageList(ctx)
			return err
		}); err != nil {
			return err
		}

		for _, repoImage := range repoImageList {
			stagesImageList = exceptRepoImageAndRelativesByImageID(stagesImageList, repoImage.ParentID)
		}

		var repoImageListToExcept []*image.Info
		if os.Getenv("WERF_DISABLE_STAGES_CLEANUP_DATE_PERIOD_POLICY") == "" {
			for _, repoImage := range stagesImageList {
				if time.Now().Unix()-repoImage.GetCreatedAt().Unix() < stagesCleanupDefaultIgnorePeriodPolicy {
					repoImageListToExcept = append(repoImageListToExcept, repoImage)
				}
			}
		}

		stagesImageList = exceptRepoImageList(stagesImageList, repoImageListToExcept...)

		var stagesToDeleteList []*image.StageDescription
		for _, imgInfo := range stagesImageList {
			if stagesByImageName[imgInfo.Name] == nil || stagesByImageName[imgInfo.Name].Info != imgInfo {
				panic(fmt.Sprintf("inconsistent state detected: %#v != %#v", stagesByImageName[imgInfo.Name].Info, imgInfo))
			}
			stagesToDeleteList = append(stagesToDeleteList, stagesByImageName[imgInfo.Name])
		}

		return logboek.Context(ctx).Default().LogProcess("Deleting stages tags").DoError(func() error {
			return deleteStageInStagesStorage(ctx, m.StagesManager, deleteImageOptions, m.DryRun, stagesToDeleteList...)
		})
	})
}

func exceptRepoImageAndRelativesByImageID(repoImageList []*image.Info, imageID string) []*image.Info {
	repoImage := findRepoImageByImageID(repoImageList, imageID)
	if repoImage == nil {
		return repoImageList
	}

	return exceptRepoImageAndRelativesByRepoImage(repoImageList, repoImage)
}

func findRepoImageByImageID(repoImageList []*image.Info, imageID string) *image.Info {
	for _, repoImage := range repoImageList {
		if repoImage.ID == imageID {
			return repoImage
		}
	}

	return nil
}

func exceptRepoImageAndRelativesByRepoImage(repoImageList []*image.Info, repoImage *image.Info) []*image.Info {
	for label, imageID := range repoImage.Labels {
		if strings.HasPrefix(label, image.WerfImportLabelPrefix) {
			repoImageList = exceptRepoImageAndRelativesByImageID(repoImageList, imageID)
		}
	}

	currentRepoImage := repoImage
	for {
		repoImageList = exceptRepoImageList(repoImageList, currentRepoImage)
		currentRepoImage = findRepoImageByImageID(repoImageList, currentRepoImage.ParentID)
		if currentRepoImage == nil {
			break
		}
	}

	return repoImageList
}

func exceptRepoImageList(repoImageList []*image.Info, repoImageListToExcept ...*image.Info) []*image.Info {
	var updatedRepoImageList []*image.Info

loop:
	for _, repoImage := range repoImageList {
		for _, repoImageToExcept := range repoImageListToExcept {
			if repoImage == repoImageToExcept {
				continue loop
			}
		}

		updatedRepoImageList = append(updatedRepoImageList, repoImage)
	}

	return updatedRepoImageList
}

func flattenRepoImages(repoImages map[string][]*image.Info) (repoImageList []*image.Info) {
	for imageName := range repoImages {
		repoImageList = append(repoImageList, repoImages[imageName]...)
	}

	return
}

func deleteStageInStagesStorage(ctx context.Context, stagesManager *stages_manager.StagesManager, options storage.DeleteImageOptions, dryRun bool, stages ...*image.StageDescription) error {
	for _, stageDesc := range stages {
		if !dryRun {
			if err := stagesManager.DeleteStages(ctx, options, stageDesc); err != nil {
				if err := handleDeleteStageOrImageError(ctx, err, stageDesc.Info.Name); err != nil {
					return err
				}

				continue
			}
		}

		logboek.Context(ctx).Default().LogFDetails("  tag: %s\n", stageDesc.Info.Tag)
		logboek.Context(ctx).LogOptionalLn()
	}

	return nil
}

func handleDeleteStageOrImageError(ctx context.Context, err error, imageName string) error {
	switch err.(type) {
	case docker_registry.DockerHubUnauthorizedError:
		return fmt.Errorf(`%s
You should specify Docker Hub token or username and password to remove tags with Docker Hub API.
Check --repo-docker-hub-token/username/password --stages-storage-repo-docker-hub-token/username/password options.
Be aware that access to the resource is forbidden with personal access token.
Read more details here https://werf.io/documentation/reference/working_with_docker_registries.html#docker-hub`, err)
	case docker_registry.GitHubPackagesUnauthorizedError:
		return fmt.Errorf(`%s
You should specify a token with the read:packages, write:packages, delete:packages and repo scopes to remove package versions.
Check --repo-github-token and --stages-storage-repo-github-token options.
Read more details here https://werf.io/documentation/reference/working_with_docker_registries.html#github-packages`, err)
	default:
		if storage.IsImageDeletionFailedDueToUsingByContainerError(err) {
			return err
		} else if strings.Contains(err.Error(), "UNAUTHORIZED") || strings.Contains(err.Error(), "UNSUPPORTED") {
			return err
		}

		logboek.Context(ctx).Warn().LogF("WARNING: Image %s deletion failed: %s\n", imageName, err)
		return nil
	}
}
