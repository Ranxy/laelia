package v1

import (
	"context"
	"io"
	"log/slog"

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

		case *v1pb.AgentStreamMessage_PullMessages:
			snapshot, pullErr := s.dispatcher.HandlePullMessages(ctx, agent.ID, m.PullMessages.ConversationId, m.PullMessages.AfterVersion)
			if pullErr != nil {
				slog.Error("failed to handle pull messages", "error", pullErr)
				continue
			}
			if sendErr := stream.Send(&v1pb.ManagerStreamMessage{
				Message: &v1pb.ManagerStreamMessage_MessageSnapshot{
					MessageSnapshot: snapshot,
				},
			}); sendErr != nil {
				slog.Error("failed to send message snapshot", "error", sendErr)
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

		default:
			slog.Warn("unknown agent stream message type")
		}
	}
}

func (s *AgentStreamService) handleAgentReady(
	ctx context.Context,
	agent *store.AgentMessage,
	_ *dispatcher.AgentSession,
	ready *v1pb.AgentReady,
) {
	if ready.LastCommandId != "" {
		cmd, err := s.store.GetCommandByName(ctx, formatCommandName(agent.ResourceID, uuid.MustParse(ready.LastCommandId)))
		if err != nil || cmd == nil {
			return
		}
		// An in-flight (RUNNING) command on reconnect hands it to the grace
		// period cleanup path. PENDING commands are handled by the dispatcher's
		// pending drain, so we only unregister when something is actively
		// running.
		if cmd.Status == 2 {
			slog.Info("agent reconnected with in-flight command", "commandID", ready.LastCommandId)
			s.dispatcher.UnregisterAgent(agent.ID)
			return
		}
	}
	// No in-flight command: trigger a drain of any PENDING commands for this
	// agent. (Idempotent — dispatchNextPending skips when the agent is busy.)
	s.dispatcher.DispatchPending(ctx, agent.ID)
}
