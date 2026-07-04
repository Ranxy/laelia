package v1

import (
	"context"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/store"
)

type AgentStreamService struct {
	v1connect.UnimplementedAgentStreamServiceHandler
	store      *store.Store
	dispatcher *dispatcher.Dispatcher
}

func NewAgentCommandService(s *store.Store, d *dispatcher.Dispatcher) *AgentStreamService {
	return &AgentStreamService{store: s, dispatcher: d}
}

func (s *AgentStreamService) AgentChannel(
	ctx context.Context,
	stream *connect.BidiStream[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage],
) error {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}

	sendFunc := func(msg *v1pb.ManagerStreamMessage) error {
		return stream.Send(msg)
	}

	sess := s.dispatcher.RegisterAgent(ctx, agent.ID, agent.ResourceID, sendFunc)
	defer s.dispatcher.UnregisterAgent(agent.ID)

	for {
		msg, err := stream.Receive()
		if err != nil {
			if err == io.EOF {
				slog.Info("agent command stream closed", "agentID", agent.ID)
				return nil
			}
			return err
		}

		switch m := msg.Message.(type) {
		case *v1pb.AgentStreamMessage_AgentReady:
			s.handleAgentReady(ctx, agent, sess, m.AgentReady)

		case *v1pb.AgentStreamMessage_BeginSession:
			resp, beginErr := s.dispatcher.HandleBeginSession(ctx, agent.ID)
			if beginErr != nil {
				slog.Error("failed to handle begin session", "error", beginErr)
				continue
			}
			if sendErr := stream.Send(&v1pb.ManagerStreamMessage{
				Message: &v1pb.ManagerStreamMessage_BeginSessionResponse{
					BeginSessionResponse: resp,
				},
			}); sendErr != nil {
				slog.Error("failed to send begin session response", "error", sendErr)
			}

		case *v1pb.AgentStreamMessage_Progress:
			if err := s.dispatcher.HandleProgress(ctx, agent.ID, m.Progress); err != nil {
				slog.Error("failed to handle progress", "error", err)
			}

		case *v1pb.AgentStreamMessage_Result:
			if err := s.dispatcher.HandleResult(ctx, agent.ID, m.Result); err != nil {
				slog.Error("failed to handle result", "error", err)
			}

		case *v1pb.AgentStreamMessage_Event:
			if err := s.dispatcher.HandleEvent(ctx, m.Event); err != nil {
				slog.Error("failed to handle event", "error", err)
			}

		case *v1pb.AgentStreamMessage_Ping:
			s.dispatcher.HandlePing(agent.ID, m.Ping)
			pong := &v1pb.ManagerStreamMessage{
				Message: &v1pb.ManagerStreamMessage_Pong{
					Pong: &v1pb.Pong{
						Seq:        m.Ping.Seq,
						ServerTime: 0,
					},
				},
			}
			if err := stream.Send(pong); err != nil {
				slog.Error("failed to send pong", "error", err)
			}

		case *v1pb.AgentStreamMessage_ProvidersDiscovered:
			s.dispatcher.CompletePendingDiscover(m.ProvidersDiscovered)

		default:
			slog.Warn("unknown agent stream message type")
		}
	}
}

func (s *AgentStreamService) handleAgentReady(
	ctx context.Context,
	agent *store.AgentMessage,
	sess *dispatcher.AgentSession,
	ready *v1pb.AgentReady,
) {
	if ready.LastCommandId != "" {
		cmd, err := s.store.GetCommandByName(ctx, formatCommandName(agent.ResourceID, uuid.MustParse(ready.LastCommandId)))
		if err == nil && cmd != nil {
			// An in-flight (RUNNING) command from before the disconnect is not
			// resumed — the agent's drain loop starts a fresh session — so mark
			// it FAILED here rather than leaving it stale.
			if cmd.Status == int32(v1pb.CommandStatus_RUNNING) {
				now := time.Now()
				if err := s.store.UpdateCommandStatus(ctx, cmd.ID, int32(v1pb.CommandStatus_FAILED), nil, &now, nil, nil, "agent disconnected during execution"); err != nil {
					slog.Error("failed to mark in-flight command failed on reconnect", "commandID", ready.LastCommandId, "error", err)
				}
				sess.ClearCurrentCommand(ready.LastCommandId)
			}
		}
	}

	// Kick the agent's drain loop so it discovers any messages missed while
	// offline. The wake is best-effort: the durable per-channel cursor is the
	// source of truth, so a missed wake just means the loop is idle until the
	// next BeginSession. The agent client also self-kicks after AgentReady.
	s.dispatcher.NotifyWake(ctx, agent.ID)
}
