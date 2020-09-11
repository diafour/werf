package storage

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/golang/example/stringutil"

	"github.com/werf/logboek"

	"github.com/werf/werf/pkg/container_runtime"
	"github.com/werf/werf/pkg/docker_registry"
	"github.com/werf/werf/pkg/image"
)

const (
	RepoStage_ImageFormat = "%s:%s-%d"

	RepoManagedImageRecord_ImageTagPrefix  = "managed-image-"
	RepoManagedImageRecord_ImageNameFormat = "%s:managed-image-%s"

	RepoImageMetadataByCommitRecord_ImageTagPrefix = "metadata-"
	RepoImageMetadataByCommitRecord_TagFormat      = "metadata-%s-%s"

	RepoClientIDRecrod_ImageTagPrefix  = "client-id-"
	RepoClientIDRecrod_ImageNameFormat = "%s:client-id-%s-%d"

	UnexpectedTagFormatErrorPrefix = "unexpected tag format"
)

func getSignatureAndUniqueIDFromRepoStageImageTag(repoStageImageTag string) (string, int64, error) {
	parts := strings.SplitN(repoStageImageTag, "-", 2)

	if len(parts) != 2 {
		return "", 0, fmt.Errorf("%s %s", UnexpectedTagFormatErrorPrefix, repoStageImageTag)
	}

	if uniqueID, err := image.ParseUniqueIDAsTimestamp(parts[1]); err != nil {
		return "", 0, fmt.Errorf("%s %s: unable to parse unique id %s as timestamp: %s", UnexpectedTagFormatErrorPrefix, repoStageImageTag, parts[1], err)
	} else {
		return parts[0], uniqueID, nil
	}
}

func isUnexpectedTagFormatError(err error) bool {
	return strings.HasPrefix(err.Error(), UnexpectedTagFormatErrorPrefix)
}

type RepoStagesStorage struct {
	RepoAddress      string
	DockerRegistry   docker_registry.DockerRegistry
	ContainerRuntime container_runtime.ContainerRuntime
}

type RepoStagesStorageOptions struct {
	docker_registry.DockerRegistryOptions
	Implementation string
}

func NewRepoStagesStorage(repoAddress string, containerRuntime container_runtime.ContainerRuntime, options RepoStagesStorageOptions) (*RepoStagesStorage, error) {
	implementation := options.Implementation

	dockerRegistry, err := docker_registry.NewDockerRegistry(repoAddress, implementation, options.DockerRegistryOptions)
	if err != nil {
		return nil, fmt.Errorf("error creating docker registry accessor for repo %q: %s", repoAddress, err)
	}

	return &RepoStagesStorage{
		RepoAddress:      repoAddress,
		DockerRegistry:   dockerRegistry,
		ContainerRuntime: containerRuntime,
	}, nil
}

func (storage *RepoStagesStorage) ConstructStageImageName(_, signature string, uniqueID int64) string {
	return fmt.Sprintf(RepoStage_ImageFormat, storage.RepoAddress, signature, uniqueID)
}

func (storage *RepoStagesStorage) GetAllStages(ctx context.Context, projectName string) ([]image.StageID, error) {
	var res []image.StageID

	if tags, err := storage.DockerRegistry.Tags(ctx, storage.RepoAddress); err != nil {
		return nil, fmt.Errorf("unable to fetch tags for repo %q: %s", storage.RepoAddress, err)
	} else {
		logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.GetRepoImagesBySignature fetched tags for %q: %#v\n", storage.RepoAddress, tags)

		for _, tag := range tags {
			if strings.HasPrefix(tag, RepoManagedImageRecord_ImageTagPrefix) || strings.HasPrefix(tag, RepoImageMetadataByCommitRecord_ImageTagPrefix) {
				continue
			}

			if signature, uniqueID, err := getSignatureAndUniqueIDFromRepoStageImageTag(tag); err != nil {
				if isUnexpectedTagFormatError(err) {
					logboek.Context(ctx).Debug().LogLn(err.Error())
					continue
				}
				return nil, err
			} else {
				res = append(res, image.StageID{Signature: signature, UniqueID: uniqueID})

				logboek.Context(ctx).Debug().LogF("Selected stage by signature %q uniqueID %d\n", signature, uniqueID)
			}
		}

		return res, nil
	}
}

func (storage *RepoStagesStorage) DeleteStages(ctx context.Context, options DeleteImageOptions, stages ...*image.StageDescription) error {
	var imageInfoList []*image.Info
	for _, stageDesc := range stages {
		imageInfoList = append(imageInfoList, stageDesc.Info)
	}
	return storage.DockerRegistry.DeleteRepoImage(ctx, imageInfoList...)
}

func (storage *RepoStagesStorage) CreateRepo(ctx context.Context) error {
	return storage.DockerRegistry.CreateRepo(ctx, storage.RepoAddress)
}

func (storage *RepoStagesStorage) DeleteRepo(ctx context.Context) error {
	return storage.DockerRegistry.DeleteRepo(ctx, storage.RepoAddress)
}

func (storage *RepoStagesStorage) GetStagesBySignature(ctx context.Context, projectName, signature string) ([]image.StageID, error) {
	var res []image.StageID

	if tags, err := storage.DockerRegistry.Tags(ctx, storage.RepoAddress); err != nil {
		return nil, fmt.Errorf("unable to fetch tags for repo %q: %s", storage.RepoAddress, err)
	} else {
		logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.GetRepoImagesBySignature fetched tags for %q: %#v\n", storage.RepoAddress, tags)
		for _, tag := range tags {
			if !strings.HasPrefix(tag, signature) {
				logboek.Context(ctx).Debug().LogF("Discard tag %q: should have prefix %q\n", tag, signature)
				continue
			}
			if _, uniqueID, err := getSignatureAndUniqueIDFromRepoStageImageTag(tag); err != nil {
				if isUnexpectedTagFormatError(err) {
					logboek.Context(ctx).Debug().LogLn(err.Error())
					continue
				}
				return nil, err
			} else {
				logboek.Context(ctx).Debug().LogF("Tag %q is suitable for signature %q\n", tag, signature)
				res = append(res, image.StageID{Signature: signature, UniqueID: uniqueID})
			}
		}
	}

	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.GetRepoImagesBySignature result for %q: %#v\n", storage.RepoAddress, res)

	return res, nil
}

func (storage *RepoStagesStorage) GetStageDescription(ctx context.Context, projectName, signature string, uniqueID int64) (*image.StageDescription, error) {
	stageImageName := storage.ConstructStageImageName(projectName, signature, uniqueID)

	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage GetStageDescription %s %s %d\n", projectName, signature, uniqueID)
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage stageImageName = %q\n", stageImageName)

	if imgInfo, err := storage.DockerRegistry.TryGetRepoImage(ctx, stageImageName); err != nil {
		return nil, err
	} else if imgInfo != nil {
		return &image.StageDescription{
			StageID: &image.StageID{Signature: signature, UniqueID: uniqueID},
			Info:    imgInfo,
		}, nil
	}
	return nil, nil
}

func (storage *RepoStagesStorage) AddManagedImage(ctx context.Context, projectName, imageName string) error {
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.AddManagedImage %s %s\n", projectName, imageName)

	if validateImageName(imageName) != nil {
		return nil
	}

	fullImageName := makeRepoManagedImageRecord(storage.RepoAddress, imageName)
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.AddManagedImage full image name: %s\n", fullImageName)

	if isExists, err := storage.DockerRegistry.IsRepoImageExists(ctx, fullImageName); err != nil {
		return err
	} else if isExists {
		logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.AddManagedImage record %q is exists => exiting\n", fullImageName)
		return nil
	}

	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.AddManagedImage record %q does not exist => creating record\n", fullImageName)

	if err := storage.DockerRegistry.PushImage(ctx, fullImageName, docker_registry.PushImageOptions{}); err != nil {
		return fmt.Errorf("unable to push image %s: %s", fullImageName, err)
	}

	return nil
}

func (storage *RepoStagesStorage) RmManagedImage(ctx context.Context, projectName, imageName string) error {
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.RmManagedImage %s %s\n", projectName, imageName)

	fullImageName := makeRepoManagedImageRecord(storage.RepoAddress, imageName)

	if imgInfo, err := storage.DockerRegistry.TryGetRepoImage(ctx, fullImageName); err != nil {
		return fmt.Errorf("unable to get repo image %q info: %s", fullImageName, err)
	} else if imgInfo == nil {
		logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.RmManagedImage record %q does not exist => exiting\n", fullImageName)
		return nil
	} else {
		if err := storage.DockerRegistry.DeleteRepoImage(ctx, imgInfo); err != nil {
			return fmt.Errorf("unable to delete image %q from repo: %s", fullImageName, err)
		}
	}

	return nil
}

func (storage *RepoStagesStorage) GetManagedImages(ctx context.Context, projectName string) ([]string, error) {
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.GetManagedImages %s\n", projectName)

	var res []string

	if tags, err := storage.DockerRegistry.Tags(ctx, storage.RepoAddress); err != nil {
		return nil, fmt.Errorf("unable to get repo %s tags: %s", storage.RepoAddress, err)
	} else {
		for _, tag := range tags {
			if !strings.HasPrefix(tag, RepoManagedImageRecord_ImageTagPrefix) {
				continue
			}

			managedImageName := unslugDockerImageTagAsImageName(strings.TrimPrefix(tag, RepoManagedImageRecord_ImageTagPrefix))

			if validateImageName(managedImageName) != nil {
				continue
			}

			res = append(res, managedImageName)
		}
	}

	return res, nil
}

func (storage *RepoStagesStorage) FetchImage(ctx context.Context, img container_runtime.Image) error {
	switch containerRuntime := storage.ContainerRuntime.(type) {
	case *container_runtime.LocalDockerServerRuntime:
		return containerRuntime.PullImageFromRegistry(ctx, img)
	default:
		// TODO: case *container_runtime.LocalHostRuntime:
		panic("not implemented")
	}
}

func (storage *RepoStagesStorage) StoreImage(ctx context.Context, img container_runtime.Image) error {
	switch containerRuntime := storage.ContainerRuntime.(type) {
	case *container_runtime.LocalDockerServerRuntime:
		dockerImage := img.(*container_runtime.DockerImage)

		if dockerImage.Image.GetBuiltId() != "" {
			return containerRuntime.PushBuiltImage(ctx, img)
		} else {
			return containerRuntime.PushImage(ctx, img)
		}

	default:
		// TODO: case *container_runtime.LocalHostRuntime:
		panic("not implemented")
	}
}

func (storage *RepoStagesStorage) ShouldFetchImage(_ context.Context, img container_runtime.Image) (bool, error) {
	switch storage.ContainerRuntime.(type) {
	case *container_runtime.LocalDockerServerRuntime:
		dockerImage := img.(*container_runtime.DockerImage)
		return !dockerImage.Image.IsExistsLocally(), nil
	default:
		panic("not implemented")
	}
}

func (storage *RepoStagesStorage) PutContentSignatureCommit(ctx context.Context, projectName, contentSignature, commit string) error {
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.PutContentSignatureCommit %s %s %s\n", projectName, contentSignature, commit)

	fullImageName := repoImageMetadataByCommitImageRecordName(storage.RepoAddress, contentSignature, commit)
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.PutContentSignatureCommit full image name: %s\n", fullImageName)

	if err := storage.DockerRegistry.PushImage(ctx, fullImageName, docker_registry.PushImageOptions{}); err != nil {
		return fmt.Errorf("unable to push image %s with metadata: %s", fullImageName, err)
	}

	logboek.Context(ctx).Info().LogF("Put content-signature %s commit %s\n", contentSignature, commit)

	return nil
}

func (storage *RepoStagesStorage) RmContentSignatureCommit(ctx context.Context, projectName, contentSignature, commit string) error {
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.RmContentSignatureCommit %s %s %s\n", projectName, contentSignature, commit)

	fullImageName := repoImageMetadataByCommitImageRecordName(storage.RepoAddress, contentSignature, commit)
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.RmContentSignatureCommit full image name: %s\n", fullImageName)

	if img, err := storage.DockerRegistry.TryGetRepoImage(ctx, fullImageName); err != nil {
		return fmt.Errorf("unable to get repo image %s: %s", fullImageName, err)
	} else if img != nil {
		if err := storage.DockerRegistry.DeleteRepoImage(ctx, img); err != nil {
			return fmt.Errorf("unable to remove repo image %s: %s", fullImageName, err)
		}

		logboek.Context(ctx).Info().LogF("Removed content-signature %s commit %s\n", contentSignature, commit)
	}

	return nil
}

func (storage *RepoStagesStorage) GetContentSignatureCommits(ctx context.Context, projectName, contentSignature string) ([]string, error) {
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.GetContentSignatureCommits %s %s\n", projectName, contentSignature)

	var res []string
	if err := storage.handleMetadataRecordTags(ctx, func(tag string) error {
		contentSignatureAndCommit := strings.TrimPrefix(tag, RepoImageMetadataByCommitRecord_ImageTagPrefix)
		contentSignatureAndCommitParts := strings.Split(contentSignatureAndCommit, "-")
		if len(contentSignatureAndCommitParts) < 2 {
			return nil
		}

		commit := contentSignatureAndCommitParts[len(contentSignatureAndCommitParts)-1]
		if repoMetaImageRecordTag(contentSignature, commit) == tag {
			logboek.Context(ctx).Debug().LogF("Found content-signature %s commit %s (%s:%s)\n", contentSignature, commit, storage.RepoAddress, tag)
			res = append(res, commit)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return res, nil
}

func (storage *RepoStagesStorage) GetCommitsByMetaTag(ctx context.Context, projectName string) (map[string][]string, error) {
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.GetCommitsByContentSignature %s\n", projectName)

	res := map[string][]string{}
	if err := storage.handleMetadataRecordTags(ctx, func(tag string) error {
		contentSignatureAndCommit := strings.TrimPrefix(tag, RepoImageMetadataByCommitRecord_ImageTagPrefix)
		contentSignatureAndCommitParts := strings.Split(contentSignatureAndCommit, "-")
		if len(contentSignatureAndCommitParts) < 2 {
			return nil
		}

		contentSignature := strings.Join(contentSignatureAndCommitParts[:len(contentSignatureAndCommitParts)-1], "-")
		commit := contentSignatureAndCommitParts[len(contentSignatureAndCommitParts)-1]

		commits, ok := res[contentSignature]
		if !ok {
			commits = []string{}
		}

		commits = append(commits, commit)
		res[contentSignature] = commits

		return nil
	}); err != nil {
		return nil, err
	}

	return res, nil
}

func (storage *RepoStagesStorage) handleMetadataRecordTags(ctx context.Context, handleFunc func(tag string) error) error {
	tags, err := storage.DockerRegistry.Tags(ctx, storage.RepoAddress)
	if err != nil {
		return fmt.Errorf("unable to get repo %s tags: %s", storage.RepoAddress, err)
	}

	for _, tag := range tags {
		if !strings.HasPrefix(tag, RepoImageMetadataByCommitRecord_ImageTagPrefix) {
			continue
		}

		if err := handleFunc(tag); err != nil {
			return err
		}
	}

	return nil
}

func repoImageMetadataByCommitImageRecordName(repoAddress, imageName, commit string) string {
	return strings.Join([]string{
		repoAddress,
		repoMetaImageRecordTag(imageName, commit),
	}, ":")
}

func (storage *RepoStagesStorage) String() string {
	return fmt.Sprintf("repo stages storage (%q)", storage.RepoAddress)
}

func (storage *RepoStagesStorage) Address() string {
	return storage.RepoAddress
}

func makeRepoManagedImageRecord(repoAddress, imageName string) string {
	return fmt.Sprintf(RepoManagedImageRecord_ImageNameFormat, repoAddress, slugImageNameAsDockerImageTag(imageName))
}

func repoMetaImageRecordTag(contentSignature string, commit string) string {
	return fmt.Sprintf(RepoImageMetadataByCommitRecord_TagFormat, contentSignature, commit)
}

func localMetaImageRecordTag(contentSignature string, commit string) string {
	return fmt.Sprintf(LocalMetadataRecord_TagFormat, contentSignature, commit)
}

func slugImageNameAsDockerImageTag(imageName string) string {
	res := imageName
	res = strings.ReplaceAll(res, "/", "__slash__")
	res = strings.ReplaceAll(res, "+", "__plus__")

	if imageName == "" {
		res = NamelessImageRecordTag
	}

	return res
}

func unslugDockerImageTagAsImageName(tag string) string {
	res := tag
	res = strings.ReplaceAll(res, "__slash__", "/")
	res = strings.ReplaceAll(res, "__plus__", "+")

	if res == NamelessImageRecordTag {
		res = ""
	}

	return res
}

func validateImageName(name string) error {
	if strings.ToLower(name) != name {
		return fmt.Errorf("no upcase symbols allowed")
	}
	return nil
}

func (storage *RepoStagesStorage) GetClientIDRecords(ctx context.Context, projectName string) ([]*ClientIDRecord, error) {
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.GetClientIDRecords for project %s\n", projectName)

	var res []*ClientIDRecord

	if tags, err := storage.DockerRegistry.Tags(ctx, storage.RepoAddress); err != nil {
		return nil, fmt.Errorf("unable to get repo %s tags: %s", storage.RepoAddress, err)
	} else {
		for _, tag := range tags {
			if !strings.HasPrefix(tag, RepoClientIDRecrod_ImageTagPrefix) {
				continue
			}

			tagWithoutPrefix := strings.TrimPrefix(tag, RepoClientIDRecrod_ImageTagPrefix)
			dataParts := strings.SplitN(stringutil.Reverse(tagWithoutPrefix), "-", 2)
			if len(dataParts) != 2 {
				continue
			}

			clientID, timestampMillisecStr := stringutil.Reverse(dataParts[1]), stringutil.Reverse(dataParts[0])

			timestampMillisec, err := strconv.ParseInt(timestampMillisecStr, 10, 64)
			if err != nil {
				continue
			}

			rec := &ClientIDRecord{ClientID: clientID, TimestampMillisec: timestampMillisec}
			res = append(res, rec)

			logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.GetClientIDRecords got clientID record: %s\n", rec)
		}
	}

	return res, nil
}

func (storage *RepoStagesStorage) PostClientIDRecord(ctx context.Context, projectName string, rec *ClientIDRecord) error {
	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.PostClientID %s for project %s\n", rec.ClientID, projectName)

	fullImageName := fmt.Sprintf(RepoClientIDRecrod_ImageNameFormat, storage.RepoAddress, rec.ClientID, rec.TimestampMillisec)

	logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.PostClientID full image name: %s\n", fullImageName)

	if isExists, err := storage.DockerRegistry.IsRepoImageExists(ctx, fullImageName); err != nil {
		return err
	} else if isExists {
		logboek.Context(ctx).Debug().LogF("-- RepoStagesStorage.AddManagedImage record %q is exists => exiting\n", fullImageName)
		return nil
	}

	if err := storage.DockerRegistry.PushImage(ctx, fullImageName, docker_registry.PushImageOptions{}); err != nil {
		return fmt.Errorf("unable to push image %s: %s", fullImageName, err)
	}

	logboek.Context(ctx).Info().LogF("Posted new clientID %q for project %s\n", rec.ClientID, projectName)

	return nil
}
