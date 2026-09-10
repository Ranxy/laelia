package dispatcher

import (
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func TestConvertChatMessageToV1(t *testing.T) {
	msgID := uuid.New()
	convID := uuid.New()
	cmdID := uuid.New()
	now := time.Now()

	msg := &store.ChatMessage{
		ID:             msgID,
		ConversationID: convID,
		PrincipalName:  "alice",
		AgentName:      "my-agent",
		Role:           1,
		Content:        "hello world",
		CommandID:      uuid.NullUUID{UUID: cmdID, Valid: true},
		CreatedAt:      now,
		RoomVersion:    42,
		SenderType:     store.SenderTypeUser,
	}

	result := ConvertChatMessageToV1(msg)

	if result.Name != msgID.String() {
		t.Errorf("expected name %s, got %s", msgID.String(), result.Name)
	}
	if result.Conversation != convID.String() {
		t.Errorf("expected conversation %s, got %s", convID.String(), result.Conversation)
	}
	if result.PrincipalName != "alice" {
		t.Errorf("expected principalName 'alice', got %s", result.PrincipalName)
	}
	if result.Role != 1 {
		t.Errorf("expected role 1, got %d", result.Role)
	}
	if result.Content != "hello world" {
		t.Errorf("expected content 'hello world', got %s", result.Content)
	}
	if result.CommandId != cmdID.String() {
		t.Errorf("expected commandId %s, got %s", cmdID.String(), result.CommandId)
	}
	if result.RoomVersion != 42 {
		t.Errorf("expected roomVersion 42, got %d", result.RoomVersion)
	}
	if result.SenderType != v1pb.SenderType(store.SenderTypeUser) {
		t.Errorf("expected senderType SENDER_TYPE_USER, got %v", result.SenderType)
	}
	if result.SenderName != "alice" {
		t.Errorf("expected senderName 'alice' for user, got %s", result.SenderName)
	}
}

func TestConvertChatMessageToV1_AgentSender(t *testing.T) {
	msg := &store.ChatMessage{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		PrincipalName:  "alice",
		AgentName:      "agent-007",
		Role:           2,
		Content:        "response",
		CreatedAt:      time.Now(),
		RoomVersion:    3,
		SenderType:     store.SenderTypeAgent,
	}

	result := ConvertChatMessageToV1(msg)

	if result.SenderName != "agent-007" {
		t.Errorf("expected senderName 'agent-007' for agent, got %s", result.SenderName)
	}
	if result.SenderType != v1pb.SenderType(store.SenderTypeAgent) {
		t.Errorf("expected senderType SENDER_TYPE_AGENT, got %v", result.SenderType)
	}
}

func TestConvertChatMessageToV1_NoCommand(t *testing.T) {
	msg := &store.ChatMessage{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		PrincipalName:  "bob",
		Role:           1,
		Content:        "no command linked",
		CreatedAt:      time.Now(),
		RoomVersion:    1,
		SenderType:     store.SenderTypeUser,
	}

	result := ConvertChatMessageToV1(msg)

	if result.CommandId != "" {
		t.Errorf("expected empty commandId, got %s", result.CommandId)
	}
}

func TestConvertChatMessageToV1_SystemSender(t *testing.T) {
	msg := &store.ChatMessage{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		PrincipalName:  "system",
		Role:           1,
		Content:        "ci trigger",
		CreatedAt:      time.Now(),
		RoomVersion:    5,
		SenderType:     store.SenderTypeSystem,
	}

	result := ConvertChatMessageToV1(msg)

	if result.SenderType != v1pb.SenderType(store.SenderTypeSystem) {
		t.Errorf("expected senderType SENDER_TYPE_SYSTEM, got %v", result.SenderType)
	}
	// System messages: SenderType != SenderTypeAgent, so senderName falls back to PrincipalName
	if result.SenderName != "system" {
		t.Errorf("expected senderName 'system', got %s", result.SenderName)
	}
}

func TestMarshalEventPayload(t *testing.T) {
	tests := []struct {
		name  string
		event *v1pb.CommandEvent
	}{
		{
			name: "lifecycle",
			event: &v1pb.CommandEvent{
				Type: v1pb.CommandEventType_LIFECYCLE,
				Payload: &v1pb.CommandEvent_Lifecycle{
					Lifecycle: &v1pb.LifecyclePayload{ExecutorKind: "ACP", Profile: "default"},
				},
			},
		},
		{
			name: "text_delta",
			event: &v1pb.CommandEvent{
				Type: v1pb.CommandEventType_TEXT_DELTA,
				Payload: &v1pb.CommandEvent_TextDelta{
					TextDelta: &v1pb.TextDeltaPayload{StreamType: "STDOUT", Content: "hello"},
				},
			},
		},
		{
			name: "tool_call_started",
			event: &v1pb.CommandEvent{
				Type: v1pb.CommandEventType_TOOL_CALL_STARTED,
				Payload: &v1pb.CommandEvent_ToolCallStarted{
					ToolCallStarted: &v1pb.ToolCallStartedPayload{Title: "read_file", RawInput: &structpb.Struct{}},
				},
			},
		},
		{
			name: "final_summary",
			event: &v1pb.CommandEvent{
				Type: v1pb.CommandEventType_FINAL_SUMMARY,
				Payload: &v1pb.CommandEvent_FinalSummary{
					FinalSummary: &v1pb.FinalSummaryPayload{StopReason: "end_turn", SessionId: "sess-1"},
				},
			},
		},
		{
			name: "context_compaction_finished",
			event: &v1pb.CommandEvent{
				Type: v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED,
				Payload: &v1pb.CommandEvent_ContextCompaction{
					ContextCompaction: &v1pb.ContextCompactionPayload{Reason: "window full", Inferred: true},
				},
			},
		},
		{
			name: "context_usage_update",
			event: &v1pb.CommandEvent{
				Type: v1pb.CommandEventType_CONTEXT_USAGE_UPDATE,
				Payload: &v1pb.CommandEvent_ContextUsage{
					ContextUsage: &v1pb.ContextUsagePayload{Size: 200000, Used: 180000, UsageRatio: 0.9},
				},
			},
		},
		{
			name: "token_usage",
			event: &v1pb.CommandEvent{
				Type: v1pb.CommandEventType_TOKEN_USAGE,
				Payload: &v1pb.CommandEvent_TokenUsage{
					TokenUsage: &v1pb.TokenUsagePayload{
						InputTokens: 100, OutputTokens: 50,
						CacheReadTokens: 20, CacheWriteTokens: 10, TotalTokens: 150,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := marshalEventPayload(tt.event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(data) == 0 {
				t.Error("expected non-empty payload")
			}
		})
	}
}

func TestMarshalEventPayload_NilForUnknown(t *testing.T) {
	event := &v1pb.CommandEvent{
		Type: v1pb.CommandEventType_COMMAND_EVENT_TYPE_UNSPECIFIED,
	}
	data, err := marshalEventPayload(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Error("expected nil data for unspecified event type")
	}
}

// Ensure store.ChatMessage SenderType constants match the proto enum values.
func TestSenderTypeConstants(t *testing.T) {
	if store.SenderTypeUser != 1 {
		t.Error("SenderTypeUser should be 1")
	}
	if store.SenderTypeAgent != 2 {
		t.Error("SenderTypeAgent should be 2")
	}
	if store.SenderTypeSystem != 3 {
		t.Error("SenderTypeSystem should be 3")
	}
}

// Ensure store.MemberType constants match what the migration/design expects.
func TestMemberTypeConstants(t *testing.T) {
	if store.MemberTypeUser != 1 {
		t.Error("MemberTypeUser should be 1")
	}
	if store.MemberTypeAgent != 2 {
		t.Error("MemberTypeAgent should be 2")
	}
}

// Ensure proto timestamps round-trip through our conversion.
func TestConvertChatMessageToV1_Timestamp(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	msg := &store.ChatMessage{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		PrincipalName:  "test",
		Role:           1,
		Content:        "ts",
		CreatedAt:      now,
		RoomVersion:    1,
		SenderType:     store.SenderTypeUser,
	}
	result := ConvertChatMessageToV1(msg)
	ts := result.CreatedAt.AsTime()
	if !ts.Equal(now) {
		t.Errorf("expected timestamp %v, got %v", now, ts)
	}
}

// Verify that ChatMessage fields used in the message-driven flow are wired.
func TestConvertChatMessageToV1_RoomVersionZero(t *testing.T) {
	// RoomVersion=0 is valid for legacy messages created before the migration.
	msg := &store.ChatMessage{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		PrincipalName:  "legacy",
		Role:           1,
		Content:        "old message",
		CreatedAt:      time.Now(),
		RoomVersion:    0,
		SenderType:     store.SenderTypeUser,
	}
	result := ConvertChatMessageToV1(msg)
	if result.RoomVersion != 0 {
		t.Errorf("expected roomVersion 0 for legacy message, got %d", result.RoomVersion)
	}
}

// Test that uuid.NullUUID with Valid=false results in empty command_id.
func TestConvertChatMessageToV1_NullCommand(t *testing.T) {
	msg := &store.ChatMessage{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		PrincipalName:  "test",
		Role:           1,
		Content:        "no cmd",
		CommandID:      uuid.NullUUID{Valid: false},
		CreatedAt:      time.Now(),
		RoomVersion:    1,
		SenderType:     store.SenderTypeUser,
	}
	result := ConvertChatMessageToV1(msg)
	if result.CommandId != "" {
		t.Errorf("expected empty commandId for NullUUID(Valid=false), got %s", result.CommandId)
	}
}

// sql.NullInt32 propagation for sender_agent_id.
func TestConvertChatMessageToV1_AgentSenderWithNullAgentName(t *testing.T) {
	msg := &store.ChatMessage{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		PrincipalName:  "alice",
		AgentName:      "",
		Role:           2,
		Content:        "response",
		CreatedAt:      time.Now(),
		RoomVersion:    1,
		SenderType:     store.SenderTypeAgent,
		SenderAgentID:  sql.NullInt32{Int32: 101, Valid: true},
	}
	result := ConvertChatMessageToV1(msg)
	// SenderType = Agent -> uses AgentName; if AgentName is empty, senderName is empty.
	if result.SenderName != "" {
		t.Errorf("expected empty senderName when AgentName is empty, got %s", result.SenderName)
	}
}

// Verify correct timestamp wrapping.
func TestConvertChatMessageToV1_TimestampProto(t *testing.T) {
	ts := time.Date(2025, 6, 23, 12, 0, 0, 0, time.UTC)
	msg := &store.ChatMessage{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		PrincipalName:  "test",
		Role:           1,
		Content:        "ts test",
		CreatedAt:      ts,
		RoomVersion:    1,
		SenderType:     store.SenderTypeUser,
	}
	result := ConvertChatMessageToV1(msg)
	expected := timestamppb.New(ts)
	if !result.CreatedAt.AsTime().Equal(expected.AsTime()) {
		t.Errorf("expected created_at %v, got %v", expected.AsTime(), result.CreatedAt.AsTime())
	}
}

// TestCurrentCommandID locks the getter used to link a session's running command
// to the conversation the agent is working on (so the channel status bar shows
// live activity). It returns the tracker's current command id, or "" when the
// agent has no in-flight command.
func TestCurrentCommandID(t *testing.T) {
	d := &Dispatcher{}

	// No entry at all.
	if got := d.CurrentCommandID(7); got != "" {
		t.Errorf("expected empty for unknown agent, got %q", got)
	}

	// Set, then read back.
	cmd := uuid.New().String()
	d.tracker.set(7, cmd)
	if got := d.CurrentCommandID(7); got != cmd {
		t.Errorf("expected %q, got %q", cmd, got)
	}

	// clear only drops the exact command that finished: a mint racing a late
	// terminal is not wiped.
	d.tracker.clear(7, uuid.New().String())
	if got := d.CurrentCommandID(7); got != cmd {
		t.Errorf("expected %q to survive a mismatched clear, got %q", cmd, got)
	}
	d.tracker.clear(7, cmd)
	if got := d.CurrentCommandID(7); got != "" {
		t.Errorf("expected empty after clear, got %q", got)
	}
}

// ---- concurrency, lifecycle ----

func noopMachineSend(_ *v1pb.ManagerMachineStreamMessage) error { return nil }

// TestDispatcher_Send_NoDataRace hammers concurrent RegisterMachine/
// UnregisterMachine/Send on a shared dispatcher. Run with -race: the send
// function lives in an atomic.Pointer, and every outbound message routes
// through the single deliver path, so writers (register/unregister/replace)
// and readers (deliver) never race on the field.
func TestDispatcher_Send_NoDataRace(_ *testing.T) {
	d := New(nil)
	defer d.Stop()

	const machines = 8
	const iters = 100
	var wg sync.WaitGroup
	for i := 0; i < machines; i++ {
		machineID := i + 1
		wg.Go(func() {
			for j := 0; j < iters; j++ {
				sess := d.RegisterMachine(machineID, "machines/m", noopMachineSend)
				var swg sync.WaitGroup
				for k := 0; k < 4; k++ {
					swg.Go(func() {
						_ = sess.Send(&v1pb.ManagerMachineStreamMessage{})
						d.tracker.set(machineID, uuid.NewString())
						d.CurrentCommandID(machineID)
					})
				}
				swg.Wait()
				d.UnregisterMachine(machineID)
			}
		})
	}
	wg.Wait()
}

// TestDispatcher_ShutdownJoinsGoroutines starts the ping monitor, then asserts
// Stop returns within a timeout — i.e. the lifecycle context cancels the ping
// ticker and the WaitGroup joins it. Previously the ping goroutine had no
// context/join.
func TestDispatcher_ShutdownJoinsGoroutines(t *testing.T) {
	d := New(nil)
	d.StartPingMonitor()

	for i := 0; i < 4; i++ {
		d.RegisterMachine(i+1, "machines/m", noopMachineSend)
	}

	done := make(chan struct{})
	go func() {
		d.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not join the ping monitor within 3s")
	}
}

func TestBuildPromptVersion(t *testing.T) {
	agent := &store.AgentMessage{Info: &models.AgentInfo{
		AcpConfig: &models.AgentACPConfig{PersonaPrompt: "be concise"},
	}}

	// Same persona/team/owner must produce the same dynamic hash; changing any
	// of them changes the dynamic part.
	v1 := buildPromptVersion("Alice Owner", &v1pb.TeamContext{TeamPrompt: "team A"}, agent)
	v2 := buildPromptVersion("Alice Owner", &v1pb.TeamContext{TeamPrompt: "team A"}, agent)
	if v1 != v2 {
		t.Fatalf("identical inputs produced different versions: %q vs %q", v1, v2)
	}
	v3 := buildPromptVersion("Alice Owner", &v1pb.TeamContext{TeamPrompt: "team B"}, agent)
	if v1 == v3 {
		t.Fatalf("team change did not change the prompt version: %q", v1)
	}
	v4 := buildPromptVersion("Bob Owner", &v1pb.TeamContext{TeamPrompt: "team A"}, agent)
	if v1 == v4 {
		t.Fatalf("owner change did not change the prompt version: %q", v1)
	}

	// The composite is "<static>.<dynamic>": static part is the manager's
	// expected machine prompt bundle version, dynamic part is 16 hex chars.
	parts := strings.Split(v1, ".")
	if len(parts) != 2 {
		t.Fatalf("expected composite \"<static>.<dynamic>\", got %q", v1)
	}
	if len(parts[1]) != 16 {
		t.Fatalf("expected 16-char dynamic hash, got %q (len=%d)", parts[1], len(parts[1]))
	}
}
