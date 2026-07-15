package v1

import (
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/component/iam"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// resolveResource extracts the target resource of an IAM-gated RPC from its
// request message, so the IAM interceptor can consult that resource's IAM
// policy in addition to the workspace policy. It scans the request's string
// fields (top-level, then one level of nested message — e.g.
// UpdateChannelRequest.conversation.name) for a value carrying a laelia
// resource prefix.
//
// It returns nil for list/create RPCs and unannotated RPCs (no recognizable
// resource in the request), and never panics: any reflection miss yields nil,
// so a resolver gap denies rather than crashes. Conversation-scoped requests
// resolve to a CONVERSATION ref; agent-scoped requests to an AGENT ref.
func resolveResource(req connect.AnyRequest) *iam.ResourceRef {
	if req == nil {
		return nil
	}
	msg, ok := req.Any().(proto.Message)
	if !ok || msg == nil {
		return nil
	}
	return resolveProtoMessage(msg.ProtoReflect())
}

func resolveProtoMessage(m protoreflect.Message) *iam.ResourceRef {
	if ref := scanResourceFields(m); ref != nil {
		return ref
	}

	// One level of nesting (e.g. UpdateChannelRequest.conversation.name).
	var nested *iam.ResourceRef
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() == protoreflect.MessageKind && fd.Cardinality() != protoreflect.Repeated {
			if ref := scanResourceFields(v.Message()); ref != nil {
				nested = ref
				return false
			}
		}
		return true
	})
	return nested
}

// scanResourceFields scans the direct string fields of m for the first value
// carrying a recognized resource prefix, in ascending field-number order.
// Conversations are preferred to agents when both prefixes appear so a
// conversation-scoped RPC is not misclassified as agent-scoped.
func scanResourceFields(m protoreflect.Message) *iam.ResourceRef {
	var agentName string
	var ref *iam.ResourceRef
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.StringKind || fd.Cardinality() == protoreflect.Repeated {
			return true
		}
		s := v.String()
		switch {
		case strings.HasPrefix(s, common.ConversationNamePrefix):
			ref = &iam.ResourceRef{ResourceType: models.Policy_CONVERSATION, Name: parentResourceName(s, common.ConversationNamePrefix)}
			return false
		case strings.HasPrefix(s, common.AgentNamePrefix) && agentName == "":
			agentName = parentResourceName(s, common.AgentNamePrefix)
		default:
			// Not a resource name; keep scanning.
		}
		return true
	})
	if ref != nil {
		return ref
	}
	if agentName != "" {
		return &iam.ResourceRef{ResourceType: models.Policy_AGENT, Name: agentName}
	}
	return nil
}

// parentResourceName strips any child-resource path so the name matches the
// resource the IAM policy is stored under. "conversations/abc/messages/42" ->
// "conversations/abc"; "agents/x/commands/c" -> "agents/x". A bare
// "conversations/abc" is unchanged.
func parentResourceName(name, prefix string) string {
	rest := strings.TrimPrefix(name, prefix)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return prefix + rest
}
