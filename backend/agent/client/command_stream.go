package client

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

const (
	cmdPingInterval = 15 * time.Second
	cmdPingTimeout  = 5 * time.Second
)

type commandStream struct {
	client     v1connect.AgentCommandServiceClient
	managerURL string
	backoff    *ExponentialBackoff
	getToken   func() string
	getSessID  func() string
}

func newCommandStream(httpClient *http.Client, managerURL string) *commandStream {
	return &commandStream{
		client:     v1connect.NewAgentCommandServiceClient(httpClient, managerURL),
		managerURL: managerURL,
		backoff:    NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait),
	}
}

func (c *commandStream) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err := c.mainLoop(ctx); err != nil {
			slog.Error("command stream error, reconnecting", "error", err)
			if err := c.backoff.Wait(ctx); err != nil {
				return err
			}
			continue
		}

		c.backoff.Reset()
	}
}

func (c *commandStream) mainLoop(ctx context.Context) error {
	token := c.getToken()
	if token == "" {
		_ = c.backoff.Wait(ctx)
		return nil
	}

	stream := c.client.CommandChannel(ctx)
	stream.RequestHeader().Set("Authorization", "Bearer "+token)

	ready := &v1pb.AgentCommandMessage{
		Message: &v1pb.AgentCommandMessage_AgentReady{
			AgentReady: &v1pb.AgentReady{
				SessionId: c.getSessID(),
			},
		},
	}
	if err := stream.Send(ready); err != nil {
		return err
	}

	pingTicker := time.NewTicker(cmdPingInterval)
	defer pingTicker.Stop()

	var pingSeq int64
	var currentExecutor *executor.BashExecutor

	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	defer close(doneCh)

	go func() {
		for {
			msg, err := stream.Receive()
			if err != nil {
				if err != io.EOF {
					errCh <- err
				}
				return
			}

			switch m := msg.Message.(type) {
			case *v1pb.ManagerCommandMessage_CommandRequest:
				req := m.CommandRequest
				slog.Info("received command", "commandID", req.CommandId, "command", req.Command)

				currentExecutor = executor.New(req.Command, req.Env, req.WorkingDir, req.TimeoutSeconds)
				go c.runCommand(ctx, currentExecutor, stream, req.CommandId)

			case *v1pb.ManagerCommandMessage_Cancel:
				if currentExecutor != nil {
					slog.Info("cancelling command", "commandID", m.Cancel.CommandId)
					currentExecutor.Cancel()
				}

			case *v1pb.ManagerCommandMessage_Pong:
				// pong received, link acknowledged

			default:
				slog.Warn("unknown message type from manager")
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-doneCh:
			return nil
		case err := <-errCh:
			return err
		case <-pingTicker.C:
			pingSeq++
			ping := &v1pb.AgentCommandMessage{
				Message: &v1pb.AgentCommandMessage_Ping{
					Ping: &v1pb.Ping{
						Seq:    pingSeq,
						SentAt: time.Now().UnixMilli(),
					},
				},
			}
			if err := stream.Send(ping); err != nil {
				return err
			}
		}
	}
}

func (*commandStream) runCommand(
	ctx context.Context,
	executor *executor.BashExecutor,
	stream *connect.BidiStreamForClient[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage],
	commandID string,
) {
	executor.Start()

	for {
		select {
		case <-ctx.Done():
			executor.Cancel()
			return
		case <-executor.Done():
			result := <-executor.ResultChannel()
			msg := &v1pb.AgentCommandMessage{
				Message: &v1pb.AgentCommandMessage_Result{
					Result: &v1pb.CommandResult{
						CommandId:    commandID,
						ExitCode:     result.ExitCode,
						DurationMs:   result.DurationMs,
						ErrorMessage: result.ErrorMessage,
						LastSeqNo:    result.LastSeqNo,
					},
				},
			}
			if err := stream.Send(msg); err != nil {
				slog.Error("failed to send command result", "commandID", commandID, "error", err)
			}
			slog.Info("command result sent", "commandID", commandID, "exitCode", result.ExitCode)
			return
		case chunk, ok := <-executor.OutputChannel():
			if !ok {
				continue
			}
			msg := &v1pb.AgentCommandMessage{
				Message: &v1pb.AgentCommandMessage_Progress{
					Progress: &v1pb.CommandProgress{
						CommandId: commandID,
						Type:      chunk.StreamType,
						Content:   chunk.Content,
						SeqNo:     chunk.SeqNo,
					},
				},
			}
			if err := stream.Send(msg); err != nil {
				slog.Error("failed to send command progress", "commandID", commandID, "error", err)
				executor.Cancel()
				return
			}
		}
	}
}
