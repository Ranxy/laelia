package dispatcher

import (
	"context"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// activityAggregator computes per-agent conversation activity from the store
// and the dispatcher's machine connectivity. It is extracted from Dispatcher so
// this aggregation can be tested and evolved independently.
type activityAggregator struct {
	store      *store.Store
	dispatcher *Dispatcher
}

func (a *activityAggregator) FetchConversationActivity(ctx context.Context, conversationID string) ([]*v1pb.AgentActivity, error) {
	convUUID, err := uuid.Parse(conversationID)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid conversation id")
	}

	members, err := a.store.ListConversationMembers(ctx, convUUID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list conversation members")
	}

	// Collect agent members: member_id is the agent resource ID.
	type agentEntry struct {
		resourceID string
		name       string
		id         int
		machineID  int
	}
	var agents []agentEntry
	var agentIDs []int
	for _, m := range members {
		if m.MemberType != store.MemberTypeAgent {
			continue
		}
		ag, agErr := a.store.GetAgentByResourceID(ctx, m.MemberID)
		if agErr != nil || ag == nil {
			continue
		}
		agents = append(agents, agentEntry{resourceID: ag.ResourceID, name: ag.Name, id: ag.ID, machineID: ag.MachineID})
		agentIDs = append(agentIDs, ag.ID)
	}

	// Batch-query running commands for these agents in this conversation.
	running, runErr := a.store.GetRunningCommandsForConversation(ctx, agentIDs, convUUID)
	if runErr != nil {
		return nil, errors.Wrapf(runErr, "failed to get running commands")
	}
	runningByAgent := make(map[int]*store.RunningCommandInfo, len(running))
	for _, r := range running {
		runningByAgent[r.AgentID] = r
	}

	// Build activity entries. An agent is online exactly when its machine's
	// control stream is live (the per-agent stream is retired).
	activities := make([]*v1pb.AgentActivity, 0, len(agents))
	for _, ag := range agents {
		act := &v1pb.AgentActivity{
			AgentId:     ag.resourceID,
			DisplayName: ag.name,
			Status:      "idle",
		}

		if a.dispatcher == nil || !a.dispatcher.IsMachineConnected(ag.machineID) {
			act.Status = "offline"
			activities = append(activities, act)
			continue
		}

		rci, hasRunning := runningByAgent[ag.id]
		if !hasRunning {
			activities = append(activities, act) // stays "idle"
			continue
		}

		// Derive status from the latest command event.
		switch rci.EventType {
		case 0:
			act.Status = "starting"
		case int32(v1pb.CommandEventType_LIFECYCLE):
			act.Status = "starting"
		case int32(v1pb.CommandEventType_TEXT_DELTA):
			act.Status = "output"
		case int32(v1pb.CommandEventType_TOOL_CALL_STARTED):
			if rci.Summary.Valid {
				act.Status = rci.Summary.String
				act.ToolName = rci.Summary.String
			} else {
				act.Status = "tool"
			}
		case int32(v1pb.CommandEventType_TOOL_CALL_FINISHED):
			act.Status = "thinking"
		case int32(v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED):
			act.Status = "compacting"
		case int32(v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED), int32(v1pb.CommandEventType_CONTEXT_USAGE_UPDATE):
			act.Status = "thinking"
		default:
			act.Status = "starting"
		}

		// Suppress idle-looking rows for agents whose drain session has moved
		// on (the tracker is the surviving session state).
		if a.dispatcher.CurrentCommandID(ag.id) == "" {
			act.Status = "idle"
		}

		activities = append(activities, act)
	}

	return activities, nil
}
