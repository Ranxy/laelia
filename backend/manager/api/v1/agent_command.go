package v1

import (
	"context"
	"io"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/Ranxy/laelia/backend/common"
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

		case *v1pb.AgentStreamMessage_SubmitAction:
			resp, submitErr := s.dispatcher.HandleSubmitAction(ctx, agent.ID, m.SubmitAction)
			if submitErr != nil {
				slog.Error("failed to handle submit action", "error", submitErr)
				continue
			}
			if sendErr := stream.Send(&v1pb.ManagerStreamMessage{
				Message: &v1pb.ManagerStreamMessage_ActionResponse{
					ActionResponse: resp,
				},
			}); sendErr != nil {
				slog.Error("failed to send action response", "error", sendErr)
			}

		case *v1pb.AgentStreamMessage_ResolveHeldAction:
			followUp, resolveErr := s.dispatcher.HandleResolveHeldAction(ctx, agent.ID, m.ResolveHeldAction)
			if resolveErr != nil {
				slog.Error("failed to handle resolve held action", "error", resolveErr)
				continue
			}
			if followUp != nil {
				if sendErr := stream.Send(followUp); sendErr != nil {
					slog.Error("failed to send command request after held action resolution", "error", sendErr)
				}
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
	sess *dispatcher.AgentSession,
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

	// Phase 2: check for held actions that the agent needs to resolve.
	heldActions, haErr := s.dispatcher.GetHeldActionsForAgent(ctx, agent.ID)
	if haErr != nil {
		slog.Error("failed to check held actions on reconnect", "agentID", agent.ID, "error", haErr)
	}
	for _, ha := range heldActions {
		var submitReq v1pb.SubmitAction
		if unmarshalErr := common.ProtojsonUnmarshaler.Unmarshal([]byte(ha.ActionJSON), &submitReq); unmarshalErr != nil {
			slog.Error("failed to unmarshal held action on reconnect", "actionID", ha.ID, "error", unmarshalErr)
			continue
		}
		newMsgs, msgErr := s.store.GetMessagesAfterVersion(ctx, ha.ConversationID, ha.BaseVersion)
		if msgErr != nil {
			slog.Error("failed to get new messages for held action on reconnect", "error", msgErr)
			continue
		}
		actionResp := &v1pb.ActionResponse{
			ActionId:       ha.ID.String(),
			Committed:      false,
			CurrentVersion: ha.CurrentVersion,
		}
		for _, m := range newMsgs {
			actionResp.NewMessages = append(actionResp.NewMessages, dispatcher.ConvertChatMessageToV1(m))
		}
		msg := &v1pb.ManagerStreamMessage{
			Message: &v1pb.ManagerStreamMessage_ActionResponse{
				ActionResponse: actionResp,
			},
		}
		if sendErr := sess.Send(msg); sendErr != nil {
			slog.Warn("failed to re-prompt held action on reconnect", "agentID", agent.ID, "actionID", ha.ID, "error", sendErr)
		}
		slog.Info("re-prompted agent with held action on reconnect", "agentID", agent.ID, "actionID", ha.ID)
	}

	if len(heldActions) == 0 {
		// No held actions: trigger a drain of PENDING commands, and notify
		// the agent of any new messages it may have missed while disconnected.
		s.dispatcher.DispatchPending(ctx, agent.ID)
	}
}
