package v1

import (
	"strings"

	annotationsproto "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/component/iam"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// resolveResources extracts the target resources of an IAM-gated RPC from its
// request message using google.api.resource_reference annotations (Bytebase's
// getResourceFromSingleRequest pattern), replacing the former string-field
// scan. It returns every annotated resource the engine can authorize:
// conversations, agents, commands, reminders, and files. Repeated fields are
// expanded so batch methods authorize each element. It never panics: a
// reflection miss yields no ref, which denies rather than crashes for
// resource-scoped permissions.
func resolveResources(msg any) []*iam.ResourceRef {
	if msg == nil {
		return nil
	}
	pm, ok := msg.(proto.Message)
	if !ok {
		return nil
	}
	var refs []*iam.ResourceRef
	collectResourceRefs(pm.ProtoReflect(), &refs)
	return dedupeRefs(refs)
}

// collectResourceRefs walks the message's annotated string fields and
// one-level nested/repeated message fields, appending recognized resources in
// field-number order.
func collectResourceRefs(m protoreflect.Message, refs *[]*iam.ResourceRef) {
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		refType := resourceReferenceType(fd)
		if refType == "" {
			return true
		}
		switch {
		case fd.Kind() == protoreflect.StringKind && !fd.IsList():
			var ref *iam.ResourceRef
			if refType == "laelia/File" {
				ref = classifyFileID(v.String())
			} else {
				ref = classifyResource(v.String())
			}
			if ref != nil {
				*refs = append(*refs, ref)
			}
		case fd.Kind() == protoreflect.MessageKind && !fd.IsList():
			if ref := refFromMessageName(v.Message()); ref != nil {
				*refs = append(*refs, ref)
			}
		case fd.Kind() == protoreflect.MessageKind && fd.IsList():
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				if ref := refFromMessageName(list.Get(i).Message()); ref != nil {
					*refs = append(*refs, ref)
				}
			}
		default:
			// Other field kinds cannot carry a resource name.
		}
		return true
	})
}

// refFromMessageName reads the "name" field of an annotated inline resource
// message (e.g. UpdateChannelRequest.conversation.name) and classifies it.
func refFromMessageName(m protoreflect.Message) *iam.ResourceRef {
	fd := m.Descriptor().Fields().ByName("name")
	if fd == nil || fd.Kind() != protoreflect.StringKind {
		return nil
	}
	return classifyResource(m.Get(fd).String())
}

// resourceReferenceType returns the google.api.resource_reference type of the
// field (e.g. "laelia/Conversation"), or "" when the field is not annotated.
func resourceReferenceType(fd protoreflect.FieldDescriptor) string {
	if !proto.HasExtension(fd.Options(), annotationsproto.E_ResourceReference) {
		return ""
	}
	ref, ok := proto.GetExtension(fd.Options(), annotationsproto.E_ResourceReference).(*annotationsproto.ResourceReference)
	if !ok || ref == nil {
		return ""
	}
	return ref.GetType()
}

// classifyResource maps a resource name to the engine resource kind it
// authorizes. Child paths are normalized to their parent resource where the
// engine keys on the parent (conversations/{id}/messages/{mid} ->
// conversations/{id}); commands, reminders, and files keep their full name
// because the engine looks the object up by name.
func classifyResource(name string) *iam.ResourceRef {
	switch {
	case strings.HasPrefix(name, common.ConversationNamePrefix):
		return &iam.ResourceRef{
			ResourceType: models.Policy_CONVERSATION,
			Name:         parentResourceName(name, common.ConversationNamePrefix),
		}
	case strings.HasPrefix(name, common.AgentNamePrefix):
		if strings.Contains(name, "/commands/") {
			return &iam.ResourceRef{ResourceType: models.Policy_COMMAND, Name: name}
		}
		return &iam.ResourceRef{
			ResourceType: models.Policy_AGENT,
			Name:         parentResourceName(name, common.AgentNamePrefix),
		}
	case strings.HasPrefix(name, "reminders/"):
		return &iam.ResourceRef{ResourceType: models.Policy_REMINDER, Name: name}
	case strings.HasPrefix(name, "files/"):
		return &iam.ResourceRef{ResourceType: models.Policy_FILE, Name: name}
	}
	return nil
}

// classifyFileID classifies a bare file UUID (DownloadFileRequest.id) as a
// FILE resource. Kept separate because the value is not a prefixed name.
func classifyFileID(id string) *iam.ResourceRef {
	if id == "" {
		return nil
	}
	return &iam.ResourceRef{ResourceType: models.Policy_FILE, Name: "files/" + id}
}

// parentResourceName strips any child-resource path so the name matches the
// resource the IAM policy is stored under. "conversations/abc/messages/42" ->
// "conversations/abc"; "agents/x/commands/c" is returned unchanged by callers
// that keep object names. A bare "conversations/abc" is unchanged.
func parentResourceName(name, prefix string) string {
	rest := strings.TrimPrefix(name, prefix)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return prefix + rest
}

// dedupeRefs removes duplicate (type, name) pairs in first-seen order.
func dedupeRefs(refs []*iam.ResourceRef) []*iam.ResourceRef {
	seen := make(map[[2]string]bool, len(refs))
	out := refs[:0]
	for _, r := range refs {
		key := [2]string{r.ResourceType.String(), r.Name}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}
