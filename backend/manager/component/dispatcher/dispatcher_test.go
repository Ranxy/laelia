package dispatcher

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

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
			name: "permission_requested",
			event: &v1pb.CommandEvent{
				Type: v1pb.CommandEventType_PERMISSION_REQUESTED,
				Payload: &v1pb.CommandEvent_PermissionRequested{
					PermissionRequested: &v1pb.PermissionRequestedPayload{
						ToolCallId: "tc-1", Kind: "bash", Title: "run command",
						ExpiresAt: time.Now().Unix() + 120,
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

func TestParseEnvJSON(t *testing.T) {
	result := parseEnvJSON("irrelevant")
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestFormatResultMessage(t *testing.T) {
	result := formatResultMessage(&v1pb.CommandResult{
		ErrorMessage: "something went wrong",
	})
	if result != "something went wrong" {
		t.Errorf("expected error message, got %s", result)
	}

	empty := formatResultMessage(&v1pb.CommandResult{})
	if empty != "" {
		t.Errorf("expected empty, got %s", empty)
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
