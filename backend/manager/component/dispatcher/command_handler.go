package dispatcher

import (
	"strconv"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func ConvertChatMessageToV1(m *store.ChatMessage) *v1pb.ChatMessage {
	cm := &v1pb.ChatMessage{
		Name:          m.ID.String(),
		Conversation:  m.ConversationID.String(),
		PrincipalName: m.PrincipalName,
		Role:          m.Role,
		Content:       m.Content,
		CreatedAt:     timestamppb.New(m.CreatedAt),
		SenderName:    m.AgentName,
		SenderType:    v1pb.SenderType(m.SenderType),
		PrincipalId:   strconv.Itoa(m.PrincipalID),
		RoomVersion:   m.RoomVersion,
		Mentions:      m.Mentions,
	}
	if m.CommandID.Valid {
		cm.CommandId = m.CommandID.UUID.String()
	}
	if m.SenderType != store.SenderTypeAgent {
		cm.SenderName = m.PrincipalName
	}
	return cm
}

// marshalEventPayload renders one command event's typed payload as the JSONB
// payload column. The SYSTEM kind (and any future payload-less kind) returns
// nil, which callers store as "{}".
func marshalEventPayload(event *v1pb.CommandEvent) ([]byte, error) {
	switch event.Type {
	case v1pb.CommandEventType_LIFECYCLE:
		return protojson.Marshal(event.GetLifecycle())
	case v1pb.CommandEventType_TEXT_DELTA:
		return protojson.Marshal(event.GetTextDelta())
	case v1pb.CommandEventType_TOOL_CALL_STARTED:
		return protojson.Marshal(event.GetToolCallStarted())
	case v1pb.CommandEventType_TOOL_CALL_FINISHED:
		return protojson.Marshal(event.GetToolCallFinished())
	case v1pb.CommandEventType_DIFF_EMITTED:
		return protojson.Marshal(event.GetDiffEmitted())
	case v1pb.CommandEventType_WARNING:
		return protojson.Marshal(event.GetWarning())
	case v1pb.CommandEventType_RAW_ACP:
		return protojson.Marshal(event.GetRawAcp())
	case v1pb.CommandEventType_FINAL_SUMMARY:
		return protojson.Marshal(event.GetFinalSummary())
	case v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED, v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED:
		return protojson.Marshal(event.GetContextCompaction())
	case v1pb.CommandEventType_CONTEXT_USAGE_UPDATE:
		return protojson.Marshal(event.GetContextUsage())
	case v1pb.CommandEventType_TOKEN_USAGE:
		return protojson.Marshal(event.GetTokenUsage())
	default:
		return nil, nil
	}
}
