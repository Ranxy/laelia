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

type AgentCommandService struct {
	v1connect.UnimplementedAgentCommandServiceHandler
	store      *store.Store
	dispatcher *dispatcher.Dispatcher
}

func NewAgentCommandService(s *store.Store, d *dispatcher.Dispatcher) *AgentCommandService {
	return &AgentCommandService{store: s, dispatcher: d}
}

func (s *AgentCommandService) CommandChannel(
	ctx context.Context,
	stream *connect.BidiStream[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage],
) error {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}

	sendFunc := func(msg *v1pb.ManagerCommandMessage) error {
		return stream.Send(msg)
	}

	sess := s.dispatcher.RegisterAgent(ctx, agent.ID, agent.ResourceID, sendFunc)
	defer s.dispatcher.UnregisterAgent(agent.ID)

	s.dispatcher.TryDispatchNext(ctx, agent.ID)

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
		case *v1pb.AgentCommandMessage_AgentReady:
			s.handleAgentReady(ctx, agent, sess, m.AgentReady)

		case *v1pb.AgentCommandMessage_Progress:
			if err := s.dispatcher.HandleProgress(ctx, agent.ID, m.Progress); err != nil {
				slog.Error("failed to handle progress", "error", err)
			}

		case *v1pb.AgentCommandMessage_Result:
			if err := s.dispatcher.HandleResult(ctx, agent.ID, m.Result); err != nil {
				slog.Error("failed to handle result", "error", err)
			}

		case *v1pb.AgentCommandMessage_Ping:
			s.dispatcher.HandlePing(agent.ID, m.Ping)
			pong := &v1pb.ManagerCommandMessage{
				Message: &v1pb.ManagerCommandMessage_Pong{
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
			slog.Warn("unknown agent command message type")
		}
	}
}

func (s *AgentCommandService) handleAgentReady(
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
		if cmd.Status == 1 || cmd.Status == 2 {
			slog.Info("agent reconnected with in-flight command", "commandID", ready.LastCommandId)
			s.dispatcher.UnregisterAgent(agent.ID)
			return
		}
	}
	s.dispatcher.TryDispatchNext(ctx, agent.ID)
}
