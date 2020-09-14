package parallel

import (
	"bytes"
	"context"
	"os"

	"github.com/werf/logboek"
	"github.com/werf/logboek/pkg/style"
	"github.com/werf/logboek/pkg/types"

	"github.com/werf/werf/pkg/docker"
)

type DoTasksOptions struct {
	InitDockerCLIForEachWorker bool
	MaxNumberOfWorkers         int
	IsLiveOutputOn             bool
}

type bufWorkerResult struct {
	buf *bytes.Buffer
	baseWorkerResult
}

type liveWorkerResult struct {
	baseWorkerResult
}

func (res *liveWorkerResult) IsLiveWorker() bool {
	return true
}

type baseWorkerResult struct {
	err error
}

func (res *baseWorkerResult) Error() error {
	return res.err
}

func (res *baseWorkerResult) SetError(err error) {
	res.err = err
}

func (res *baseWorkerResult) IsLiveWorker() bool {
	return false
}

type workerResult interface {
	IsLiveWorker() bool
	SetError(err error)
	Error() error
}

func DoTasks(ctx context.Context, numberOfTasks int, options DoTasksOptions, taskFunc func(ctx context.Context, taskId int) error) error {
	numberOfWorkers := options.MaxNumberOfWorkers
	if numberOfWorkers <= 0 || numberOfWorkers > numberOfTasks {
		numberOfWorkers = numberOfTasks
	}

	var numberOfTasksPerWorker []int
	for i := 0; i < numberOfWorkers; i++ {
		workerNumberOfTasks := numberOfTasks / numberOfWorkers
		rest := numberOfTasks % numberOfWorkers
		if rest > i {
			workerNumberOfTasks += 1
		}

		numberOfTasksPerWorker = append(numberOfTasksPerWorker, workerNumberOfTasks)
	}

	errCh := make(chan workerResult)
	doneTaskCh := make(chan workerResult)
	doneWorkerCh := make(chan workerResult)
	quitCh := make(chan bool)
	doneWorkersCounter := numberOfWorkers
	isLiveOutputOnFlag := options.IsLiveOutputOn

	var liveLogger types.LoggerInterface
	var liveContext context.Context
	if options.IsLiveOutputOn {
		liveLogger = logboek.NewLogger(os.Stdout, os.Stderr)
		liveLogger.GetStreamsSettingsFrom(logboek.Context(ctx))
		liveLogger.SetAcceptedLevel(logboek.Context(ctx).AcceptedLevel())

		liveContext = logboek.NewContext(ctx, liveLogger)

		if err := docker.SyncContextCliWithLogger(liveContext); err != nil {
			return err
		}
		defer docker.SyncContextCliWithLogger(ctx)
	}

	var workersBuffs, workersDoneBuffs []*bytes.Buffer
	for i := 0; i < numberOfWorkers; i++ {
		var workerContext context.Context
		var workerResult workerResult

		workerId := i

		if i == 0 && options.IsLiveOutputOn {
			workerContext = liveContext
			workerResult = &liveWorkerResult{}
		} else {
			workerBuf := bytes.NewBuffer([]byte{})
			workersBuffs = append(workersBuffs, workerBuf)
			workerResult = &bufWorkerResult{buf: workerBuf}

			workerContext = logboek.NewContext(ctx, logboek.Context(ctx).NewSubLogger(workerBuf, workerBuf))
			logboek.Context(workerContext).Streams().SetPrefixStyle(style.Highlight())

			if options.InitDockerCLIForEachWorker {
				workerContextWithDockerCli, err := docker.NewContext(workerContext)
				if err != nil {
					return err
				}

				workerContext = workerContextWithDockerCli
			}
		}

		go func() {
			workerNumberOfTasks := numberOfTasksPerWorker[workerId]

			for workerTaskId := 0; workerTaskId < workerNumberOfTasks; workerTaskId++ {
				taskId := calculateTaskId(numberOfTasks, numberOfWorkers, workerId, workerTaskId)
				err := taskFunc(workerContext, taskId)

				ch := doneTaskCh
				if err != nil {
					workerResult.SetError(err)
					ch = errCh
				}

				select {
				case ch <- workerResult:
				case <-quitCh:
					return
				}
			}

			doneWorkerCh <- workerResult
		}()
	}

	for {
		select {
		case res := <-doneTaskCh:
			switch workerResult := res.(type) {
			case *bufWorkerResult:
				if isLiveOutputOnFlag {
					workersDoneBuffs = append(workersDoneBuffs, workerResult.buf)
				} else {
					processBuf(ctx, workerResult.buf)
				}
			case *liveWorkerResult:
				for _, buf := range workersDoneBuffs {
					processBuf(ctx, buf)
				}
			}
		case res := <-errCh:
			close(quitCh)

			switch workerResult := res.(type) {
			case *bufWorkerResult:
				if isLiveOutputOnFlag {
					liveLogger.Streams().Mute()
				}

				for _, buf := range workersBuffs {
					if buf != workerResult.buf {
						processBuf(ctx, buf)
					}
				}

				processBuf(ctx, workerResult.buf)
			case *liveWorkerResult:
				if logboek.Context(ctx).Info().IsAccepted() {
					for _, buf := range append(
						workersDoneBuffs,
						workersBuffs...
					) {
						processBuf(ctx, buf)
					}
				}
			}

			return res.Error()
		case res := <-doneWorkerCh:
			if res.IsLiveWorker() {
				isLiveOutputOnFlag = false
			}

			doneWorkersCounter--
			if doneWorkersCounter == 0 {
				return nil
			}
		}
	}
}

func calculateTaskId(tasksNumber, workersNumber, workerInd, workerTaskId int) int {
	taskId := workerInd*(tasksNumber/workersNumber) + workerTaskId

	rest := tasksNumber % workersNumber
	if rest != 0 {
		if rest > workerInd {
			taskId += workerInd
		} else {
			taskId += rest
		}
	}

	return taskId
}

func processBuf(ctx context.Context, buf *bytes.Buffer) {
	logboek.Streams().DoWithoutIndent(func() {
		if logboek.Context(ctx).Streams().IsPrefixWithTimeEnabled() {
			logboek.Context(ctx).Streams().DisablePrefixWithTime()
			defer logboek.Context(ctx).Streams().EnablePrefixWithTime()
		}

		_, _ = logboek.Context(ctx).ProxyOutStream().Write(buf.Bytes())
		logboek.Context(ctx).LogOptionalLn()

		buf.Reset()
	})
}
