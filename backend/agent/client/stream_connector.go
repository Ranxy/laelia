package client

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const cmdPingInterval = 15 * time.Second

// Start runs one AgentChannel connection lifecycle (mainLoop) and returns its
// terminal error. It deliberately does NOT retry internally: the runner's
// connectLoop owns reconnection (phase 2 — a dead stream must not tear down
// the runner or its in-flight turn), so a single lifecycle stays a simple,
// testable unit.
func (c *commandStream) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	return c.mainLoop(ctx)
}

// connectLoop owns the AgentChannel across streams: it opens one connection
// lifecycle at a time and reconnects with backoff when the stream dies. The
// drain loop and the uploader live OUTSIDE this loop (runner lifetime), so a
// dead stream can never interrupt a running turn — a proxy that bounds request
// read time costs the agent a reconnect, nothing more (phase 2).
func (c *commandStream) connectLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.mainLoop(ctx); err != nil {
			slog.Warn("agent channel died; reconnecting", "agent", c.agentID, "error", err)
		}
		if c.backoff.Wait(ctx) != nil {
			return
		}
	}
}

// mainLoop owns one AgentChannel connection: it opens the stream, sends
// AgentReady, installs the connection for the drain loop, starts the receive
// pump (messageRouter), and keeps the link alive with pings until the stream
// dies or the context is cancelled. The drain loop is NOT owned here: it
// outlives connections.
func (c *commandStream) mainLoop(ctx context.Context) error {
	token := c.getToken()
	if token == "" {
		_ = c.backoff.Wait(ctx)
		return nil
	}

	stream := c.client.AgentChannel(ctx)
	stream.RequestHeader().Set("Authorization", "Bearer "+token)

	ready := &v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_AgentReady{
			AgentReady: &v1pb.AgentReady{
				AgentName: c.agentName,
				SessionId: c.getSessID(),
			},
		},
	}
	if state, err := executor.LoadLocalState(c.machineID, c.agentID); err != nil {
		slog.Warn("failed to load local command state", "error", err)
	} else if state != nil {
		ready.GetAgentReady().LastCommandId = state.CommandID
		ready.GetAgentReady().LastAckSeq = state.LastSeqSent
		ready.GetAgentReady().LastEventSeq = state.LastEventSeqSent
	}
	if err := stream.Send(ready); err != nil {
		return err
	}

	// serializedSender guards Send: connect-go's Send is not safe to call
	// concurrently, and the workspace reply goroutines send alongside the ping
	// ticker and the drain loop.
	conn := &agentConn{
		sender:     &serializedSender{stream: stream},
		done:       make(chan struct{}),
		beginResps: make(chan *v1pb.BeginSessionResponse, 1),
	}
	c.setConn(conn)
	defer c.clearConn(conn)
	defer close(conn.done)

	router := newMessageRouter(c)

	pingTicker := time.NewTicker(cmdPingInterval)
	defer pingTicker.Stop()

	var pingSeq int64

	errCh := make(chan error, 1)

	// Receive pump: dispatches manager messages on this connection.
	go func() {
		for {
			msg, err := stream.Receive()
			if err != nil {
				if err != io.EOF {
					select {
					case errCh <- err:
					case <-conn.done:
					}
				}
				return
			}
			router.route(ctx, conn, msg)
		}
	}()

	// Kick the drain loop once on connect so missed-offline messages are
	// discovered immediately (AgentReady already told the manager we're back).
	c.wake()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-conn.done:
			return nil
		case err := <-errCh:
			return err
		case <-pingTicker.C:
			pingSeq++
			ping := &v1pb.AgentStreamMessage{
				Message: &v1pb.AgentStreamMessage_Ping{
					Ping: &v1pb.Ping{
						Seq:    pingSeq,
						SentAt: time.Now().UnixMilli(),
					},
				},
			}
			if err := conn.sender.Send(ping); err != nil {
				return err
			}
		}
	}
}
