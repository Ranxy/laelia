package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// This file implements the agent-side address resolver: it turns the
// name-oriented conversation/message addresses the LLM is prompted to write
// ("#<title>", "dm:@<peer>") into the canonical "conversations/<id>" /
// "conversations/<id>/messages/<m>" resource names the manager expects,
// creating DMs if absent.
//
// The grammar is name-based, with one id escape hatch: "conversations/<id>" is
// accepted for conversations the agent can already read (surfaced by `channel
// list`; the manager re-validates read permission), while bare ids and
// "conversations/<c>/messages/<m>" paths are rejected as input. Files (bare
// file id), reminders ("reminders/<id>"), and thread roots (bare message id)
// stay id-based by design — they have no human name.
//
// The resolver is the input-side counterpart of conversationAddress (also in
// this file): output emits "<address>", input accepts "<address>".

// resolveConversationAddress turns a conversation address into the canonical
// "conversations/<id>" resource name. "#<title>" resolves (never creates) a
// channel; "dm:@<peer>" opens or reuses a DM with the named agent or user;
// "conversations/<id>" is passed through for conversations the agent can
// already read (its memberships or owner-follow — the manager re-validates read
// permission on every use), which is how `channel list` surfaces owner-visible
// DMs that have no name-form address. Anything else is an
// INVALID_ARGUMENT_FAILED — only the name grammar is accepted. An empty
// address resolves to "" with no error so optional callers (search, upload) can
// pass through unchanged.
func resolveConversationAddress(ctx context.Context, d Deps, addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", nil
	}
	switch {
	case strings.HasPrefix(addr, "#"):
		return resolveChannelTitle(ctx, d, addr[1:])
	case strings.HasPrefix(addr, "dm:"):
		peer := strings.TrimSpace(strings.TrimPrefix(addr[3:], "@"))
		if peer == "" {
			return "", localError("INVALID_ARGUMENT_FAILED", "dm: requires a peer name (dm:@<agent-or-user>)", "Use dm:@<peer>.")
		}
		return resolveDMAddress(ctx, d, peer)
	case strings.HasPrefix(addr, "conversations/"):
		// The id must be a well-formed UUID; anything else is rejected locally so
		// a malformed name never reaches the manager (the manager re-validates
		// read permission on the well-formed ones).
		if _, err := uuid.Parse(strings.TrimPrefix(addr, "conversations/")); err != nil {
			return "", localError("INVALID_ARGUMENT_FAILED", fmt.Sprintf("invalid conversation resource name %q", addr), "Use the name printed by `channel list` (conversations/<uuid>).")
		}
		return addr, nil
	default:
		return "", localError("INVALID_ARGUMENT_FAILED", fmt.Sprintf("unknown conversation address %q; use #<title>, dm:@<peer>, or a conversations/<id> name", addr), "Run `channel list` to see what you can read, or `message check` for your channels.")
	}
}

// resolveChannelTitle looks up the unique channel with the given title. It never
// creates one: an absent channel is a NOT_FOUND_FAILED, matching the design's
// "channels are user-created; agents only address existing ones" rule.
func resolveChannelTitle(ctx context.Context, d Deps, title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "# requires a channel title (e.g. #general)", "Use #<title>.")
	}
	resp, err := d.Client.ResolveChannelByTitle(ctx, connect.NewRequest(&v1pb.ResolveChannelByTitleRequest{Title: title}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return conversationName(resp.Msg, fmt.Sprintf("channel %q", title))
}

// resolveDMAddress opens or reuses a DM with the named peer. A peer given as
// "agents/<id>" addresses an agent directly; otherwise the peer is a display
// name resolved to an agent first and, if no agent matches, to a user. Agent-
// first means a shared name addresses the agent. When an agent DOES match, its
// DM is opened and the result returned verbatim — a NotFound from that create
// (e.g. the peer was deleted between the list and the create) must NOT fall
// through to the user path, or a same-named user would silently receive agent
// DMs. Only "no agent matched the name" falls through to the user path.
func resolveDMAddress(ctx context.Context, d Deps, peer string) (string, error) {
	if strings.HasPrefix(peer, "agents/") {
		return createAgentDM(ctx, d, peer)
	}
	match, err := findPeerAgentByName(ctx, d, peer)
	if err != nil {
		return "", err
	}
	if match != nil {
		return createAgentDM(ctx, d, match.GetName())
	}
	return createUserDM(ctx, d, peer)
}

// findPeerAgentByName returns the single peer agent whose display name matches
// peer, or nil when no agent matches so the caller falls through to the user
// path. More than one match is an ambiguous-name error; a ListPeerAgents
// failure is propagated. Splitting the lookup from the DM create lets the caller
// own the fall-through, so a create-time NotFound after a match is not mistaken
// for "no agent matched".
func findPeerAgentByName(ctx context.Context, d Deps, peer string) (*v1pb.PeerAgent, error) {
	resp, err := d.Client.ListPeerAgents(ctx, connect.NewRequest(&v1pb.ListPeerAgentsRequest{}))
	if err != nil {
		return nil, wrapManagerError(err)
	}
	var match *v1pb.PeerAgent
	for _, a := range resp.Msg.GetAgents() {
		if a.GetDisplayName() == peer {
			if match != nil {
				return nil, localError("AMBIGUOUS_PEER", fmt.Sprintf("multiple agents named %q; address one as dm:@agents/<resource-id>", peer), "Run `agent list` and use dm:@agents/<resource-id>.")
			}
			match = a
		}
	}
	return match, nil
}

// findUserByName returns the single user whose display name (title) matches
// name, or nil when no user matches so the caller can report a not-found. More
// than one match is an ambiguous-name error; a ListUsers failure is propagated.
// The UserServiceClient is optional (Deps.UserClient may be nil for callers
// that never resolve users); a nil client is a PERMISSION_FAILED, not a panic.
func findUserByName(ctx context.Context, d Deps, name string) (*v1pb.User, error) {
	if d.UserClient == nil {
		return nil, localError("PERMISSION_FAILED", "user lookup is unavailable in this context", "Use users/<id> to add a user by id.")
	}
	var match *v1pb.User
	pageToken := ""
	for {
		resp, err := d.UserClient.ListUsers(ctx, connect.NewRequest(&v1pb.ListUsersRequest{PageToken: pageToken, PageSize: 100}))
		if err != nil {
			return nil, wrapManagerError(err)
		}
		for _, u := range resp.Msg.GetUsers() {
			if u.GetTitle() == name {
				if match != nil {
					return nil, localError("AMBIGUOUS_USER", fmt.Sprintf("multiple users named %q; address one as users/<id>", name), "Run `members <address>` or use users/<id> to disambiguate.")
				}
				match = u
			}
		}
		next := resp.Msg.GetNextPageToken()
		if next == "" || len(resp.Msg.GetUsers()) == 0 {
			break
		}
		pageToken = next
	}
	return match, nil
}

// createAgentDM opens or reuses the type-3 agent DM with the peer given as the
// "agents/<id>" resource name. The manager rejects self-address and unknown
// agents; those reach the caller as wrapped manager errors.
func createAgentDM(ctx context.Context, d Deps, peerAgent string) (string, error) {
	resp, err := d.Client.GetOrCreateAgentDM(ctx, connect.NewRequest(&v1pb.GetOrCreateAgentDMRequest{PeerAgent: peerAgent}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return conversationName(resp.Msg, fmt.Sprintf("agent DM with %s", peerAgent))
}

// createUserDM opens or reuses the type-1 DM with the named end user. The
// manager resolves the name (NOT_FOUND for an unknown user, FailedPrecondition
// for an ambiguous one) and creates the DM if the user exists.
func createUserDM(ctx context.Context, d Deps, peer string) (string, error) {
	resp, err := d.Client.GetOrCreateUserDM(ctx, connect.NewRequest(&v1pb.GetOrCreateUserDMRequest{PeerUserName: peer}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return conversationName(resp.Msg, fmt.Sprintf("user DM with %q", peer))
}

// conversationName extracts the canonical "conversations/<id>" name from a
// resolution response, returning NOT_FOUND_FAILED when the manager returned a
// response carrying no resource name. The guard is shared by the channel/agent-
// DM/user-DM resolvers so an empty name can never flow silently into a later
// manager RPC that requires one.
func conversationName[T interface{ GetConversation() *v1pb.Conversation }](resp T, what string) (string, error) {
	name := strings.TrimSpace(resp.GetConversation().GetName())
	if name == "" {
		return "", localError("NOT_FOUND_FAILED", fmt.Sprintf("%s resolved without a resource name", what), "")
	}
	return name, nil
}

// splitMessageAddress splits a message address into its conversation address
// and bare message id. It accepts two forms:
//   - address form "<addr>:<message-uuid>" → ("<addr>", "<uuid>"), where <addr>
//     is "#title" or "dm:@peer"; ':' inside a title is tolerated because only a
//     UUID suffix is split off
//   - a bare token with no message id → (token, "")
//
// The message id is the suffix after the last ':' whose remainder parses as a
// UUID, so a title containing ':' (e.g. "#plan:b") is not mis-split. A legacy
// "conversations/<c>/messages/<m>" token has no ':' and no UUID suffix, so it
// returns as a bare conversation token and is rejected downstream by
// resolveConversationAddress.
func splitMessageAddress(addr string) (convAddr, msgID string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", ""
	}
	// Address form "<addr>:<message-uuid>": split off the message id only when
	// the suffix after the last ':' parses as a UUID. A UUID contains no ':',
	// so if the last ':' does not yield one, no earlier ':' can either — its
	// suffix would include the later ':' and trailing text. A single check
	// therefore suffices and tolerates ':' inside a channel title (e.g.
	// "#plan:b").
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		if _, err := uuid.Parse(addr[i+1:]); err == nil {
			return strings.TrimSpace(addr[:i]), addr[i+1:]
		}
	}
	return addr, ""
}

// resolveThreadRoot resolves a thread operation's "--conversation" and
// "--root" into the canonical conversation name and bare root message id the
// manager expects. The conversation is taken from --conversation when present,
// otherwise derived from a "<addr>:<uuid>" --root; a bare root id with no
// conversation is rejected (threads have no name, so a root is a bare message
// id or a "<addr>:<uuid>" handle — never a legacy full name).
func resolveThreadRoot(ctx context.Context, d Deps, conv, root string) (convName, rootID string, err error) {
	rootAddr, msgID := splitMessageAddress(root)
	if msgID != "" {
		rootID = msgID
	} else {
		rootID = strings.TrimSpace(rootAddr) // bare root id (e.g. from `thread check`)
	}

	switch {
	case strings.TrimSpace(conv) != "":
		convName, err = resolveConversationAddress(ctx, d, conv)
	case msgID != "":
		convName, err = resolveConversationAddress(ctx, d, rootAddr)
	default:
		return "", "", localError("MISSING_CONVERSATION", "conversation is required (pass --conversation or a --root message address)", "Pass --conversation #<channel> or dm:@<peer>.")
	}
	return convName, rootID, err
}

// resolveMessageName turns a message address into the full
// "conversations/<c>/messages/<m>" resource name the manager's task/reminder
// RPCs expect. A bare token with no message id is rejected: those RPCs need a
// real message name, never a bare conversation id.
func resolveMessageName(ctx context.Context, d Deps, addr string) (string, error) {
	convAddr, msgID := splitMessageAddress(addr)
	if msgID == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "message is required as an address (<address>:<message-id>)", "Pass the message handle from `task list` / `message read`.")
	}
	convName, err := resolveConversationAddress(ctx, d, convAddr)
	if err != nil {
		return "", err
	}
	return convName + "/messages/" + msgID, nil
}

// bareRootID extracts the bare thread-root message id from a root reference.
// It accepts the same forms as splitMessageAddress: an address "<addr>:<uuid>",
// or a bare id (as printed by `thread check`). A root without a message suffix
// returns the trimmed token unchanged so a bare id round-trips.
func bareRootID(root string) string {
	convAddr, msgID := splitMessageAddress(root)
	if msgID != "" {
		return msgID
	}
	return strings.TrimSpace(convAddr)
}

// conversationAddress renders the canonical display address ("#<title>",
// "dm:@<peer>") for a conversation given its "conversations/<id>" resource name,
// by looking it up via GetChannel and reading the manager-populated Address
// field (the single source of truth for the grammar — it already encodes the
// type-3 agent-DM peer). It is the emit-side counterpart of
// resolveConversationAddress. A lookup failure or an empty address falls back to
// the resource name so a label-only emit site never breaks the agent's display.
// This fallback is a display label, not a copyable message handle: callers that
// build agent-usable handles (messageHandle) pass a name-form address obtained
// from the agent's input, never this fallback, so no rejected id form ever
// reaches the resolver as input.
func conversationAddress(ctx context.Context, d Deps, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	resp, err := d.Client.GetChannel(ctx, connect.NewRequest(&v1pb.GetChannelRequest{Name: name}))
	if err != nil {
		return name
	}
	if addr := strings.TrimSpace(resp.Msg.GetAddress()); addr != "" {
		return addr
	}
	return name
}

// quoteAddress wraps a conversation address (or message handle) in single
// quotes when it begins with '#'. A bare unquoted "#general" is silently
// treated as the start of a shell comment and dropped before the CLI sees it,
// so every channel address an agent copies from output is shown quoted: the
// agent pastes "'#general'" verbatim and the shell strips the quotes, leaving
// the literal "#general" for the resolver. dm: addresses contain no '#' and
// are returned unchanged.
func quoteAddress(addr string) string {
	if strings.HasPrefix(addr, "#") {
		return "'" + addr + "'"
	}
	return addr
}

// messageHandle renders the message handle an agent copies from output into
// task/reminder/thread commands: "<address>:<message-id>". The address must be a
// real name form (callers obtain it from the agent's input, which is already
// validated as a name); an unresolved "conversations/<id>" address yields "" so
// no rejected id form is ever emitted as a copyable handle. A channel handle
// (address begins with '#') is wrapped in single quotes via quoteAddress so the
// agent can copy it into a shell command without the '#' being parsed as a
// comment. Returns "" when either part is empty.
func messageHandle(addr, messageID string) string {
	addr = strings.TrimSpace(addr)
	messageID = strings.TrimSpace(messageID)
	if addr == "" || messageID == "" {
		return ""
	}
	if strings.HasPrefix(addr, "conversations/") {
		return ""
	}
	return quoteAddress(addr + ":" + messageID)
}
