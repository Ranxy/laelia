package executor

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const outputBufferSize = 1024

type OutputChunk struct {
	StreamType v1pb.CommandOutput_StreamType
	Content    string
	SeqNo      int32
}

type Result struct {
	ExitCode     int32
	DurationMs   int64
	ErrorMessage string
	LastSeqNo    int32
	FinalSummary string
	Result       *structpb.Struct
}

type BashExecutor struct {
	ctx       context.Context
	cancel    context.CancelFunc
	cmd       *exec.Cmd
	outputCh  chan OutputChunk
	eventCh   chan Event
	resultCh  chan Result
	seqNo     atomic.Int32
	startedAt time.Time
	done      chan struct{}
}

func New(cmdStr string, env map[string]string, workDir string, timeoutSeconds int32) *BashExecutor {
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeoutSeconds > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if workDir != "" {
		cmd.Dir = workDir
	}

	if len(env) > 0 {
		cmdEnv := cmd.Environ()
		for k, v := range env {
			cmdEnv = append(cmdEnv, k+"="+v)
		}
		cmd.Env = cmdEnv
	}

	return &BashExecutor{
		ctx:      ctx,
		cancel:   cancel,
		cmd:      cmd,
		outputCh: make(chan OutputChunk, outputBufferSize),
		eventCh:  make(chan Event, outputBufferSize),
		resultCh: make(chan Result, 1),
		done:     make(chan struct{}),
	}
}

func (e *BashExecutor) Start() {
	e.startedAt = time.Now()

	stdout, err := e.cmd.StdoutPipe()
	if err != nil {
		e.sendResult(Result{ExitCode: -1, ErrorMessage: err.Error()})
		return
	}
	stderr, err := e.cmd.StderrPipe()
	if err != nil {
		e.sendResult(Result{ExitCode: -1, ErrorMessage: err.Error()})
		return
	}

	if err := e.cmd.Start(); err != nil {
		e.sendResult(Result{ExitCode: -1, ErrorMessage: err.Error()})
		return
	}

	go e.readStream(stdout, v1pb.CommandOutput_STDOUT)
	go e.readStream(stderr, v1pb.CommandOutput_STDERR)

	go func() {
		err := e.cmd.Wait()
		duration := time.Since(e.startedAt).Milliseconds()

		result := Result{DurationMs: duration, LastSeqNo: e.seqNo.Load() - 1}

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = int32(exitErr.ExitCode())
			} else if e.ctx.Err() != nil {
				if e.ctx.Err() == context.DeadlineExceeded {
					result.ExitCode = -1
					result.ErrorMessage = "command timed out"
				} else {
					result.ExitCode = -1
					result.ErrorMessage = "command cancelled"
				}
			} else {
				result.ExitCode = -1
				result.ErrorMessage = err.Error()
			}
		}

		e.sendResult(result)
	}()
}

func (e *BashExecutor) readStream(r io.Reader, streamType v1pb.CommandOutput_StreamType) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		seq := e.seqNo.Add(1) - 1
		select {
		case e.outputCh <- OutputChunk{
			StreamType: streamType,
			Content:    scanner.Text(),
			SeqNo:      seq,
		}:
		case <-e.ctx.Done():
			return
		}
	}
}

func (e *BashExecutor) Cancel() {
	e.cancel()
	_ = syscall.Kill(-e.cmd.Process.Pid, syscall.SIGKILL)
}

func (e *BashExecutor) OutputChannel() <-chan OutputChunk {
	return e.outputCh
}

func (e *BashExecutor) ResultChannel() <-chan Result {
	return e.resultCh
}

func (e *BashExecutor) EventChannel() <-chan Event {
	return e.eventCh
}

func (e *BashExecutor) Done() <-chan struct{} {
	return e.done
}

func (e *BashExecutor) Run() Result {
	e.Start()
	return <-e.resultCh
}

func (e *BashExecutor) sendResult(r Result) {
	e.resultCh <- r
	close(e.resultCh)
	close(e.done)
	close(e.eventCh)
	close(e.outputCh)
}
