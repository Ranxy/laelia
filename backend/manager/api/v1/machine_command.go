package v1

import (
	"context"
	"io"
	"log/slog"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// MachineStreamService implements MachineStreamService.MachineChannel: the
// machine-level control plane. A machine authenticates once (machine access
// token, resolved by the auth interceptor into MachineContextKey) and holds
// this single bidi stream for the lifetime of its connection. Over it the
// manager pushes agent-roster changes (AgentAssignment / RemoveAgent /
// AgentConfigUpdate / ReloadAgentAssignment) and provider-discovery requests;
// the machine reports readiness, pings, discovery results, and graceful
// disconnect. The per-agent data plane runs on each agent's own AgentChannel.
type MachineStreamService struct {
	v1connect.UnimplementedMachineStreamServiceHandler
	store      *store.Store
	dispatcher *dispatcher.Dispatcher
}

func NewMachineStreamService(s *store.Store, d *dispatcher.Dispatcher) *MachineStreamService {
	return &MachineStreamService{store: s, dispatcher: d}
}

func (s *MachineStreamService) MachineChannel(
	ctx context.Context,
	stream *connect.BidiStream[v1pb.MachineStreamMessage, v1pb.ManagerMachineStreamMessage],
) error {
	machine, ok := GetMachineFromContext(ctx)
	if !ok || machine == nil {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}

	sendFunc := func(msg *v1pb.ManagerMachineStreamMessage) error {
		return stream.Send(msg)
	}

	s.dispatcher.RegisterMachine(machine.ID, machine.ResourceID, sendFunc)
	defer s.dispatcher.UnregisterMachine(machine.ID)

	slog.Info("machine control stream connected", "machineID", machine.ID, "resourceID", machine.ResourceID)

	for {
		msg, err := stream.Receive()
		if err != nil {
			if err == io.EOF {
				slog.Info("machine control stream closed", "machineID", machine.ID)
				return nil
			}
			return err
		}

		switch m := msg.Message.(type) {
		case *v1pb.MachineStreamMessage_MachineReady:
			// The machine echoes the session id ConnectMachine minted; nothing
			// to persist (the session row is already ACTIVE). Acknowledged for
			// log correlation only.
			slog.Info("machine ready", "machineID", machine.ID, "sessionID", m.MachineReady.GetSessionId())

		case *v1pb.MachineStreamMessage_Ping:
			s.dispatcher.HandleMachinePing(machine.ID, m.Ping)
			if err := s.dispatcher.SendPongToMachine(machine.ID); err != nil {
				slog.Error("failed to send pong to machine", "machineID", machine.ID, "error", err)
			}

		case *v1pb.MachineStreamMessage_ProvidersDiscovered:
			// Completes a pending RefreshMachineProviders round-trip; the
			// DiscoverProviders request is correlated by request_id.
			s.dispatcher.CompletePendingDiscover(m.ProvidersDiscovered)

		case *v1pb.MachineStreamMessage_DisconnectNotice:
			slog.Info("machine announced graceful disconnect", "machineID", machine.ID, "reason", m.DisconnectNotice.GetReason())
			return nil

		default:
			slog.Warn("unknown machine stream message type", "machineID", machine.ID)
		}
	}
}
