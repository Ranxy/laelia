package v1

import (
	"context"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
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

// AgentChannel is the per-agent data plane. The machine app opens one
// AgentChannel per hosted agent, authenticating the stream with the machine's
// access token (the auth interceptor resolves it into MachineContextKey). The
// first inbound message must be AgentReady carrying agent_name (agents/{id});
// the handler resolves that agent, verifies the machine owns it
// (agent.machine_id == machine.id), and registers the agent-keyed session. The
// rest of the stream — BeginSession / Progress / Result / Event / Ping /
// ProvidersDiscovered — is unchanged and keyed by agent id.
func (s *AgentStreamService) AgentChannel(
	ctx context.Context,
	stream *connect.BidiStream[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage],
) error {
	machine, ok := GetMachineFromContext(ctx)
	if !ok || machine == nil {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	// Reject agent data streams for machines that are not ONLINE. Mirrors the
	// MachineChannel gate: a force-disconnected machine must re-ConnectMachine
	// (flipping state to ONLINE) before it can re-establish agent streams,
	// preventing a non-cooperative machine from resuming agents with a
	// still-valid access token after a force-disconnect.
	if machine.Status == nil || machine.Status.GetState() != storepb.MachineStatus_ONLINE {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("machine %s is not online", machine.ResourceID))
	}

	sendFunc := func(msg *v1pb.ManagerStreamMessage) error {
		return stream.Send(msg)
	}

	// The agent is declared in-stream, not by a header: wait for the first
	// AgentReady before registering. A non-AgentReady first message is a
	// protocol violation.
	first, err := stream.Receive()
	if err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	ready, ok := first.Message.(*v1pb.AgentStreamMessage_AgentReady)
	if !ok || ready.AgentReady == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("first agent stream message must be AgentReady"))
	}
	agent, err := s.resolveAgentForMachine(ctx, machine, ready.AgentReady.GetAgentName())
	if err != nil {
		return err
	}

	sess := s.dispatcher.RegisterAgent(ctx, agent.ID, machine.ID, agent.ResourceID, sendFunc)
	// Identity-aware teardown: if a reconnect replaced this session before the
	// old stream ends, do not destroy the new (live) session.
	defer s.dispatcher.UnregisterAgentIf(agent.ID, sess)

	s.handleAgentReady(ctx, agent, sess, ready.AgentReady)

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
			// A reconnecting runner re-announces; re-run the ready bookkeeping.
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

// resolveAgentForMachine looks up the agent declared by an AgentReady and
// verifies the authenticated machine owns it. The agent_name is the agents/{id}
// resource name carried in-stream.
func (s *AgentStreamService) resolveAgentForMachine(ctx context.Context, machine *store.MachineMessage, agentName string) (*store.AgentMessage, error) {
	if agentName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("AgentReady.agent_name must be set"))
	}
	resourceID, err := common.GetAgentResourceID(agentName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}
	if agent.MachineID != machine.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("agent %s is not bound to machine %s", resourceID, machine.ResourceID))
	}
	return agent, nil
}

func (s *AgentStreamService) handleAgentReady(
	ctx context.Context,
	agent *store.AgentMessage,
	sess *dispatcher.AgentSession,
	ready *v1pb.AgentReady,
) {
	if ready.LastCommandId != "" {
		cmdID, parseErr := uuid.Parse(ready.LastCommandId)
		if parseErr != nil {
			// A malformed last_command_id (corrupted/tampered on-disk state on
			// the machine) must not crash the handler — it just means there is
			// no in-flight command to reap.
			slog.Warn("ignoring malformed last_command_id from agent", "last_command_id", ready.LastCommandId, "error", parseErr)
		} else {
			cmd, err := s.store.GetCommandByName(ctx, formatCommandName(agent.ResourceID, cmdID))
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
	}

	// Kick the agent's drain loop so it discovers any messages missed while
	// offline. The wake is best-effort: the durable per-channel cursor is the
	// source of truth, so a missed wake just means the loop is idle until the
	// next BeginSession. The agent client also self-kicks after AgentReady.
	s.dispatcher.NotifyWake(ctx, agent.ID)
}
