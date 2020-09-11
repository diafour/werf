package cleaning

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/werf/logboek"

	"github.com/werf/werf/pkg/config"
)

func scanReferencesHistory(ctx context.Context, gitRepository *git.Repository, refs []*referenceToScan, expectedTagCommitHashes map[string][]plumbing.Hash) ([]string, map[string][]plumbing.Hash, error) {
	var reachedTagList []string
	var stopCommitHashes []plumbing.Hash

	tagHitCommitHashes := map[string][]plumbing.Hash{}

	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]

		var refReachedTagList []string
		var refStopCommitHashes []plumbing.Hash
		var refTagHitCommitHashes map[string]plumbing.Hash
		var err error

		var logProcessMessage string
		if ref.Reference.Name().IsTag() {
			logProcessMessage = "Tag " + ref.String()
		} else {
			logProcessMessage = "Reference " + ref.String()
		}

		if err := logboek.Context(ctx).Info().LogProcess(logProcessMessage).DoError(func() error {
			refReachedTagList, refStopCommitHashes, refTagHitCommitHashes, err = scanReferenceHistory(ctx, gitRepository, ref, expectedTagCommitHashes, stopCommitHashes)
			if err != nil {
				return fmt.Errorf("scan reference history failed: %s", err)
			}

			stopCommitHashes = append(stopCommitHashes, refStopCommitHashes...)

		reachedTagListLoop:
			for _, c1 := range refReachedTagList {
				for _, c2 := range reachedTagList {
					if c1 == c2 {
						continue reachedTagListLoop
					}
				}

				reachedTagList = append(reachedTagList, c1)
			}

		refHitCommitHashesLoop:
			for tag, refHitCommitHash := range refTagHitCommitHashes {
				hitCommitHashes, ok := tagHitCommitHashes[tag]
				if !ok {
					hitCommitHashes = []plumbing.Hash{}
				}

				for _, hitCommitHash := range hitCommitHashes {
					if refHitCommitHash == hitCommitHash {
						continue refHitCommitHashesLoop
					}
				}

				tagHitCommitHashes[tag] = append(hitCommitHashes, refHitCommitHash)
			}

			return nil
		}); err != nil {
			return nil, nil, err
		}
	}

	return reachedTagList, tagHitCommitHashes, nil
}

func applyImagesCleanupInPolicy(gitRepository *git.Repository, tagCommitHashes map[string][]plumbing.Hash, in *time.Duration) map[string][]plumbing.Hash {
	if in == nil {
		return tagCommitHashes
	}

	policyTagCommitHashes := map[string][]plumbing.Hash{}
	for tag, commitHashes := range tagCommitHashes {
		var resultCommitHashList []plumbing.Hash
		for _, commitHash := range commitHashes {
			commit, err := gitRepository.CommitObject(commitHash)
			if err != nil {
				panic("unexpected condition")
			}

			if commit.Committer.When.After(time.Now().Add(-*in)) {
				resultCommitHashList = append(resultCommitHashList, commitHash)
			}
		}

		if len(resultCommitHashList) != 0 {
			policyTagCommitHashes[tag] = resultCommitHashList
		}
	}

	return policyTagCommitHashes
}

type commitHistoryScanner struct {
	gitRepository                        *git.Repository
	expectedTagCommitHashes map[string][]plumbing.Hash
	reachedTagCommitHashes  map[string][]plumbing.Hash
	reachedCommitHashes                  []plumbing.Hash
	stopCommitHashes                     []plumbing.Hash
	isAlreadyScannedCommit               map[plumbing.Hash]bool
	scanDepth                            int

	referenceScanOptions
}

func (s *commitHistoryScanner) reachedTagList() []string {
	var reachedTagList []string
	for tag, commitHashes := range s.reachedTagCommitHashes {
		if len(commitHashes) != 0 {
			reachedTagList = append(reachedTagList, tag)
		}
	}

	return reachedTagList
}

func scanReferenceHistory(ctx context.Context, gitRepository *git.Repository, ref *referenceToScan, expectedTagCommitHashes map[string][]plumbing.Hash, stopCommitHashes []plumbing.Hash) ([]string, []plumbing.Hash, map[string]plumbing.Hash, error) {
	filteredExpectedTagCommitHashes := applyImagesCleanupInPolicy(gitRepository, expectedTagCommitHashes, ref.imagesCleanupKeepPolicy.In)

	var refExpectedTagCommitHashes map[string][]plumbing.Hash
	isImagesCleanupKeepPolicyOnlyInOrAndBoth := ref.imagesCleanupKeepPolicy.Last == nil || (ref.imagesCleanupKeepPolicy.Operator != nil && *ref.imagesCleanupKeepPolicy.Operator == config.AndOperator)
	if isImagesCleanupKeepPolicyOnlyInOrAndBoth {
		refExpectedTagCommitHashes = filteredExpectedTagCommitHashes
	} else {
		refExpectedTagCommitHashes = expectedTagCommitHashes
	}

	if len(refExpectedTagCommitHashes) == 0 {
		logboek.Context(ctx).Info().LogLn("Skip reference due to nothing to seek")
		return []string{}, stopCommitHashes, map[string]plumbing.Hash{}, nil
	}

	s := &commitHistoryScanner{
		gitRepository:                        gitRepository,
		expectedTagCommitHashes: refExpectedTagCommitHashes,
		reachedTagCommitHashes:  map[string][]plumbing.Hash{},
		stopCommitHashes:                     stopCommitHashes,

		referenceScanOptions:   ref.referenceScanOptions,
		isAlreadyScannedCommit: map[plumbing.Hash]bool{},
	}

	if err := s.scanCommitHistory(ctx, ref.HeadCommit.Hash); err != nil {
		return nil, nil, nil, fmt.Errorf("scan commit %s history failed: %s", ref.HeadCommit.Hash.String(), err)
	}

	isImagesCleanupKeepPolicyLastWithoutLimit := s.referenceScanOptions.imagesCleanupKeepPolicy.Last != nil && *s.referenceScanOptions.imagesCleanupKeepPolicy.Last != -1
	if isImagesCleanupKeepPolicyLastWithoutLimit {
		if len(s.reachedTagList()) == *s.referenceScanOptions.imagesCleanupKeepPolicy.Last {
			return s.reachedTagList(), s.stopCommitHashes, s.tagHitCommitHash(), nil
		} else if len(s.reachedTagList()) > *s.referenceScanOptions.imagesCleanupKeepPolicy.Last {
			logboek.Context(ctx).Info().LogF("Reached more content signatures than expected by last (%d/%d)\n", len(s.reachedTagList()), *s.referenceScanOptions.imagesCleanupKeepPolicy.Last)

			latestCommitTag := s.tagByLatestCommit()
			var latestCommitList []*object.Commit
			for latestCommit := range latestCommitTag {
				latestCommitList = append(latestCommitList, latestCommit)
			}

			sort.Slice(latestCommitList, func(i, j int) bool {
				return latestCommitList[i].Committer.When.After(latestCommitList[j].Committer.When)
			})

			if s.referenceScanOptions.imagesCleanupKeepPolicy.In == nil {
				return s.handleExtraTagsByLast(ctx, latestCommitTag, latestCommitList)
			} else {
				return s.handleExtraTagsByLastWithIn(ctx, latestCommitTag, latestCommitList)
			}
		}
	}

	if !reflect.DeepEqual(expectedTagCommitHashes, refExpectedTagCommitHashes) {
		return s.reachedTagList(), s.stopCommitHashes, s.tagHitCommitHash(), nil
	}

	return s.handleStopCommitHashes(ctx, ref)
}

func (s *commitHistoryScanner) handleStopCommitHashes(ctx context.Context, ref *referenceToScan) ([]string, []plumbing.Hash, map[string]plumbing.Hash, error) {
	if s.referenceScanOptions.scanDepthLimit != 0 {
		if len(s.reachedTagList()) == len(s.expectedTagCommitHashes) {
			s.stopCommitHashes = append(s.stopCommitHashes, s.reachedCommitHashes[len(s.reachedCommitHashes)-1])
		} else {
			return s.reachedTagList(), s.stopCommitHashes, s.tagHitCommitHash(), nil
		}
	} else if len(s.reachedTagList()) != 0 {
		s.stopCommitHashes = append(s.stopCommitHashes, s.reachedCommitHashes[len(s.reachedCommitHashes)-1])
	} else {
		s.stopCommitHashes = append(s.stopCommitHashes, ref.HeadCommit.Hash)
	}
	logboek.Context(ctx).Debug().LogF("Stop commit %s added\n", s.stopCommitHashes[len(s.stopCommitHashes)-1].String())

	return s.reachedTagList(), s.stopCommitHashes, s.tagHitCommitHash(), nil
}

func (s *commitHistoryScanner) handleExtraTagsByLastWithIn(ctx context.Context, latestCommitTag map[*object.Commit]string, latestCommitList []*object.Commit) ([]string, []plumbing.Hash, map[string]plumbing.Hash, error) {
	var latestCommitListByLast []*object.Commit
	var latestCommitListByIn []*object.Commit

	tagHitCommitHashes := map[string]plumbing.Hash{}

	for ind, latestCommit := range latestCommitList {
		if ind < *s.referenceScanOptions.imagesCleanupKeepPolicy.Last {
			latestCommitListByLast = append(latestCommitListByLast, latestCommit)
		}

		if latestCommit.Committer.When.After(time.Now().Add(-*s.referenceScanOptions.imagesCleanupKeepPolicy.In)) {
			latestCommitListByIn = append(latestCommitListByIn, latestCommit)
		}
	}

	var resultLatestCommitList []*object.Commit
	isImagesCleanupKeepPolicyOperatorAnd := s.referenceScanOptions.imagesCleanupKeepPolicy.Operator == nil || *s.referenceScanOptions.imagesCleanupKeepPolicy.Operator == config.AndOperator
	if isImagesCleanupKeepPolicyOperatorAnd {
		for _, commitByLast := range latestCommitListByLast {
			for _, commitByIn := range latestCommitListByIn {
				if commitByLast == commitByIn {
					resultLatestCommitList = append(resultLatestCommitList, commitByLast)
				}
			}
		}
	} else {
		resultLatestCommitList = latestCommitListByIn[:]
	latestCommitListByLastLoop:
		for _, commitByLast := range latestCommitListByLast {
			for _, commitByIn := range latestCommitListByIn {
				if commitByLast == commitByIn {
					continue latestCommitListByLastLoop
				}
			}

			resultLatestCommitList = append(resultLatestCommitList, commitByLast)
		}
	}

	var reachedTagList []string
	for _, latestCommit := range resultLatestCommitList {
		tag := latestCommitTag[latestCommit]
		reachedTagList = append(reachedTagList, tag)
		tagHitCommitHashes[tag] = latestCommit.Hash
	}

	var skippedTagList []string
latestCommitTagLoop:
	for _, tag := range latestCommitTag {
		for _, reachedTag := range reachedTagList {
			if tag == reachedTag {
				continue latestCommitTagLoop
			}
		}

		skippedTagList = append(skippedTagList, tag)
	}

	if len(skippedTagList) != 0 {
		logboek.Context(ctx).Info().LogBlock(fmt.Sprintf("Skipped content signatures by keep policy (%s)", s.imagesCleanupKeepPolicy.String())).Do(func() {
			for _, tag := range skippedTagList {
				logboek.Context(ctx).Info().LogLn(tag)
			}
		})
	}

	return reachedTagList, s.stopCommitHashes, tagHitCommitHashes, nil
}

func (s *commitHistoryScanner) handleExtraTagsByLast(ctx context.Context, latestCommitTag map[*object.Commit]string, latestCommitList []*object.Commit) ([]string, []plumbing.Hash, map[string]plumbing.Hash, error) {
	var reachedTagList []string
	var skippedTagList []string
	tagHitCommitHashes := map[string]plumbing.Hash{}

	for ind, latestCommit := range latestCommitList {
		tag := latestCommitTag[latestCommit]
		if ind < *s.referenceScanOptions.imagesCleanupKeepPolicy.Last {
			reachedTagList = append(reachedTagList, tag)
			tagHitCommitHashes[tag] = latestCommit.Hash
		} else {
			skippedTagList = append(skippedTagList, tag)
		}
	}

	logboek.Context(ctx).Info().LogBlock(fmt.Sprintf("Skipped content signatures by keep policy (%s)", s.imagesCleanupKeepPolicy.String())).Do(func() {
		for _, tag := range skippedTagList {
			logboek.Context(ctx).Info().LogLn(tag)
		}
	})

	return reachedTagList, s.stopCommitHashes, tagHitCommitHashes, nil
}

func (s *commitHistoryScanner) scanCommitHistory(ctx context.Context, commitHash plumbing.Hash) error {
	var currentIteration, nextIteration []plumbing.Hash

	currentIteration = append(currentIteration, commitHash)
	for {
		s.scanDepth++

		for _, commitHash := range currentIteration {
			if s.isAlreadyScannedCommit[commitHash] {
				continue
			}

			if s.isStopCommitHash(commitHash) {
				logboek.Context(ctx).Debug().LogF("Stop scanning commit history %s due to stop commit reached\n", commitHash.String())
				continue
			}

			commitParents, err := s.handleCommitHash(ctx, commitHash)
			if err != nil {
				return err
			}

			if s.scanDepth == s.referenceScanOptions.scanDepthLimit {
				logboek.Context(ctx).Debug().LogF("Stop scanning commit history %s due to scanDepthLimit (%d)\n", commitHash.String(), s.referenceScanOptions.scanDepthLimit)
				continue
			}

			if len(s.expectedTagCommitHashes) == len(s.reachedTagCommitHashes) {
				logboek.Context(ctx).Debug().LogLn("Stop scanning due to all expected content signatures reached")
				break
			}

			s.isAlreadyScannedCommit[commitHash] = true
			nextIteration = append(nextIteration, commitParents...)
		}

		if len(nextIteration) == 0 {
			break
		}

		currentIteration = nextIteration
		nextIteration = []plumbing.Hash{}
	}

	return nil
}

func (s *commitHistoryScanner) handleCommitHash(ctx context.Context, commitHash plumbing.Hash) ([]plumbing.Hash, error) {
outerLoop:
	for tag, commitHashes := range s.expectedTagCommitHashes {
		for _, c := range commitHashes {
			if c == commitHash {
				for _, reachedC := range s.reachedCommitHashes {
					if reachedC == c {
						break outerLoop
					}
				}

				if s.imagesCleanupKeepPolicy.In != nil {
					commit, err := s.gitRepository.CommitObject(commitHash)
					if err != nil {
						panic("unexpected condition")
					}

					isImagesCleanupKeepPolicyOnlyInOrAndBoth := s.imagesCleanupKeepPolicy.Last == nil || s.imagesCleanupKeepPolicy.Operator == nil || *s.imagesCleanupKeepPolicy.Operator == config.AndOperator
					if isImagesCleanupKeepPolicyOnlyInOrAndBoth {
						if commit.Committer.When.Before(time.Now().Add(-*s.imagesCleanupKeepPolicy.In)) {
							break outerLoop
						}
					}
				}

				s.reachedCommitHashes = append(s.reachedCommitHashes, c)

				reachedCommitHashes, ok := s.reachedTagCommitHashes[tag]
				if !ok {
					reachedCommitHashes = []plumbing.Hash{}
				}
				reachedCommitHashes = append(reachedCommitHashes, c)
				s.reachedTagCommitHashes[tag] = reachedCommitHashes

				if !ok {
					logboek.Context(ctx).Info().LogF(
						"Expected content signature %s was reached on commit %s\n",
						tag,
						commitHash.String(),
					)
				} else {
					logboek.Context(ctx).Info().LogF(
						"Expected content signature %s was reached again on another commit %s\n",
						tag,
						commitHash.String(),
					)
				}

				break outerLoop
			}
		}
	}

	co, err := s.gitRepository.CommitObject(commitHash)
	if err != nil {
		return nil, fmt.Errorf("commit hash %s resolve failed: %s", commitHash.String(), err)
	}

	return co.ParentHashes, nil
}

func (s *commitHistoryScanner) isStopCommitHash(commitHash plumbing.Hash) bool {
	for _, c := range s.stopCommitHashes {
		if commitHash == c {
			return true
		}
	}

	return false
}

func (s *commitHistoryScanner) tagHitCommitHash() map[string]plumbing.Hash {
	result := map[string]plumbing.Hash{}
	for commit, tag := range s.tagByLatestCommit() {
		result[tag] = commit.Hash
	}

	return result
}

func (s *commitHistoryScanner) tagByLatestCommit() map[*object.Commit]string {
	tagLatestCommit := map[*object.Commit]string{}
	for tag, commitHashes := range s.reachedTagCommitHashes {
		var latestCommit *object.Commit
		for _, commitHash := range commitHashes {
			commit, err := s.gitRepository.CommitObject(commitHash)
			if err != nil {
				panic("unexpected condition")
			}

			if latestCommit == nil || commit.Committer.When.After(latestCommit.Committer.When) {
				latestCommit = commit
			}
		}

		if latestCommit != nil {
			tagLatestCommit[latestCommit] = tag
		}
	}

	return tagLatestCommit
}
