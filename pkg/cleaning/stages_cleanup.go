package cleaning

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/werf/logboek"

	"github.com/werf/werf/pkg/docker_registry"
	"github.com/werf/werf/pkg/image"
	"github.com/werf/werf/pkg/storage"
)

const stagesCleanupDefaultIgnorePeriodPolicy = 2 * 60 * 60

func (m *cleanupManager) cleanupUnusedStages(ctx context.Context) error {
	stagesImageListToDelete := m.stageImageList

	for tag, _ := range m.tagCommitHashes {
		stagesImageListToDelete = exceptRepoImageAndRelativesByImageID(stagesImageListToDelete, m.mustGetStageImage(tag).Info.ParentID)
	}

	var repoImageListToExcept []*image.StageDescription
	if os.Getenv("WERF_DISABLE_STAGES_CLEANUP_DATE_PERIOD_POLICY") == "" {
		for _, repoImage := range stagesImageListToDelete {
			if time.Now().Unix()-repoImage.Info.GetCreatedAt().Unix() < stagesCleanupDefaultIgnorePeriodPolicy {
				repoImageListToExcept = append(repoImageListToExcept, repoImage)
			}
		}
	}

	stagesImageListToDelete = exceptRepoImageList(stagesImageListToDelete, repoImageListToExcept...)

	return logboek.Context(ctx).Default().LogProcess("Deleting stages tags").DoError(func() error {
		return m.deleteStageInStagesStorage(ctx, stagesImageListToDelete...)
	})
}

func exceptRepoImageAndRelativesByImageID(repoImageList []*image.StageDescription, imageID string) []*image.StageDescription {
	repoImage := findRepoImageByImageID(repoImageList, imageID)
	if repoImage == nil {
		return repoImageList
	}

	return exceptRepoImageAndRelativesByRepoImage(repoImageList, repoImage)
}

func findRepoImageByImageID(repoImageList []*image.StageDescription, imageID string) *image.StageDescription {
	for _, repoImage := range repoImageList {
		if repoImage.Info.ID == imageID {
			return repoImage
		}
	}

	return nil
}

func exceptRepoImageAndRelativesByRepoImage(repoImageList []*image.StageDescription, repoImage *image.StageDescription) []*image.StageDescription {
	for label, imageID := range repoImage.Info.Labels {
		if strings.HasPrefix(label, image.WerfImportLabelPrefix) {
			repoImageList = exceptRepoImageAndRelativesByImageID(repoImageList, imageID)
		}
	}

	currentRepoImage := repoImage
	for {
		repoImageList = exceptRepoImageList(repoImageList, currentRepoImage)
		currentRepoImage = findRepoImageByImageID(repoImageList, currentRepoImage.Info.ParentID)
		if currentRepoImage == nil {
			break
		}
	}

	return repoImageList
}

func exceptRepoImageList(repoImageList []*image.StageDescription, repoImageListToExcept ...*image.StageDescription) []*image.StageDescription {
	var updatedRepoImageList []*image.StageDescription

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

func (m *cleanupManager) deleteStageInStagesStorage(ctx context.Context, stages ...*image.StageDescription) error {
	options := storage.DeleteImageOptions{
		RmiForce:      false,
		SkipUsedImage: true,
		RmForce:       false,
	}

	for _, stageDesc := range stages {
		if !m.DryRun {
			if err := m.StagesManager.DeleteStages(ctx, options, stageDesc); err != nil {
				if err := handleDeleteStageOrImageError(ctx, err, stageDesc.Info.Name); err != nil {
					return err
				}

				continue
			}
		}

		logboek.Context(ctx).Default().LogFDetails("  tag: %s\n", stageDesc.Info.Tag)
		logboek.Context(ctx).LogOptionalLn()
	}

	m.deleteStageImageFromCache(stages...)

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
