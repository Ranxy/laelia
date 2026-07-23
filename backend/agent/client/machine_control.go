package client

import (
	"context"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// runControlStream opens the machine-level MachineChannel bidi stream and pumps
// it for the lifetime of one connection. It sends MachineReady on open, pings
// on a ticker, and replies to DiscoverProviders with a fresh host probe. On
// the receive side it drives the agent roster: AgentAssignment / ReloadAgentAssignment
// spawn or re-config a runner, RemoveAgent tears one down, AgentConfigUpdate
// hot-reloads a runner's ACP config. It returns when the stream ends or ctx is
// cancelled; the caller (Run) treats a non-nil return as a death signal that
// tears down the whole connection.
func (c *MachineClient) runControlStream(ctx context.Context, _ *daemonsrv.Server) error {
	c.mu.RLock()
	token := c.accessToken
	sessionID := c.sessionID
	c.mu.RUnlock()

	streamClient := v1connect.NewMachineStreamServiceClient(c.streamClient, c.managerURL)
	stream := streamClient.MachineChannel(ctx)
	stream.RequestHeader().Set("Authorization", "Bearer "+token)

	if err := stream.Send(&v1pb.MachineStreamMessage{
		Message: &v1pb.MachineStreamMessage_MachineReady{
			MachineReady: &v1pb.MachineReady{SessionId: sessionID},
		},
	}); err != nil {
		return err
	}

	pingTicker := time.NewTicker(machinePingInterval)
	defer pingTicker.Stop()

	var pingSeq int64
	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	defer close(doneCh)

	// Receive pump: drive the agent roster from manager pushes.
	go func() {
		for {
			msg, err := stream.Receive()
			if err != nil {
				if err != io.EOF {
					select {
					case errCh <- err:
					case <-doneCh:
					}
				}
				return
			}

			switch m := msg.Message.(type) {
			case *v1pb.ManagerMachineStreamMessage_AgentAssignment:
				c.spawnOrUpdate(ctx, m.AgentAssignment)

			case *v1pb.ManagerMachineStreamMessage_ReloadAgentAssignment:
				// Full re-sync of one agent: drop any existing runner and spawn
				// fresh with the new assignment.
				if m.ReloadAgentAssignment != nil {
					c.stopRunner(m.ReloadAgentAssignment.GetAgentName())
					c.spawnOrUpdate(ctx, m.ReloadAgentAssignment.GetAssignment())
				}

			case *v1pb.ManagerMachineStreamMessage_RemoveAgent:
				c.stopRunner(m.RemoveAgent.GetAgentName())

			case *v1pb.ManagerMachineStreamMessage_AgentConfigUpdate:
				c.hotReloadAgentConfig(m.AgentConfigUpdate)

			case *v1pb.ManagerMachineStreamMessage_DiscoverProviders:
				c.handleDiscoverProviders(ctx, stream, m.DiscoverProviders.GetRequestId())

			case *v1pb.ManagerMachineStreamMessage_Pong:
				// pong received, link acknowledged

			default:
				slog.Warn("unknown message type from manager on machine control stream")
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// Announce a graceful disconnect before tearing the stream down.
			_ = stream.Send(&v1pb.MachineStreamMessage{
				Message: &v1pb.MachineStreamMessage_DisconnectNotice{
					DisconnectNotice: &v1pb.MachineDisconnectNotice{Reason: "shutdown"},
				},
			})
			return nil
		case <-doneCh:
			return nil
		case err := <-errCh:
			return err
		case <-pingTicker.C:
			pingSeq++
			if err := stream.Send(&v1pb.MachineStreamMessage{
				Message: &v1pb.MachineStreamMessage_Ping{
					Ping: &v1pb.Ping{
						Seq:    pingSeq,
						SentAt: time.Now().UnixMilli(),
					},
				},
			}); err != nil {
				return err
			}
		}
	}
}

// hotReloadAgentConfig updates one agent runner's ACP config in place; the next
// BeginSession picks it up. The runner is left running.
func (c *MachineClient) hotReloadAgentConfig(update *v1pb.AgentConfigUpdate) {
	if update == nil || update.GetAgentName() == "" {
		return
	}
	agentID := bareAgentID(update.GetAgentName())
	c.runnersMu.Lock()
	r, ok := c.runners[agentID]
	c.runnersMu.Unlock()
	if !ok {
		// The manager pushed a config update for an agent we are not currently
		// hosting (e.g. it was removed, or we have not yet received its
		// assignment). Drop it — the next assigned_agents resync or
		// AgentAssignment will carry the current config.
		slog.Warn("config update for unknown agent runner; ignoring", "agent", update.GetAgentName())
		return
	}
	r.setConfig(r.buildAcpConfig(&v1pb.AgentAssignment{
		AgentName:        update.GetAgentName(),
		AgentDisplayName: r.displayName,
		AcpConfig:        update.GetAcpConfig(),
	}))
	slog.Info("hot-reloaded agent ACP config", "agent", update.GetAgentName())
}

// handleDiscoverProviders re-probes the host and replies with the fresh
// provider list, correlated by the manager's request_id.
func (c *MachineClient) handleDiscoverProviders(ctx context.Context, stream *connect.BidiStreamForClient[v1pb.MachineStreamMessage, v1pb.ManagerMachineStreamMessage], requestID string) {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	discovered := c.refreshProviders(probeCtx)
	cancel()
	if err := stream.Send(&v1pb.MachineStreamMessage{
		Message: &v1pb.MachineStreamMessage_ProvidersDiscovered{
			ProvidersDiscovered: &v1pb.ProvidersDiscovered{
				RequestId: requestID,
				Providers: discoveredToProto(discovered, time.Now()),
			},
		},
	}); err != nil {
		slog.Error("failed to send providers_discovered", "requestID", requestID, "error", err)
	}
}
