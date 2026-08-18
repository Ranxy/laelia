package v1

import (
	"testing"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestComputeConnectionState(t *testing.T) {
	tests := []struct {
		name      string
		status    storepb.AgentStatus_ConnectionState
		deleted   bool
		connected bool
		enabled   bool
		want      v1pb.AgentStatus_ConnectionState
	}{
		{name: "online", status: storepb.AgentStatus_ONLINE, connected: true, enabled: true, want: v1pb.AgentStatus_ONLINE},
		{name: "offline", status: storepb.AgentStatus_OFFLINE, enabled: true, want: v1pb.AgentStatus_OFFLINE},
		{name: "error", status: storepb.AgentStatus_ERROR, connected: true, enabled: true, want: v1pb.AgentStatus_ERROR},
		{name: "kicked", status: storepb.AgentStatus_KICKED, connected: true, enabled: true, want: v1pb.AgentStatus_KICKED},
		{name: "deleted", status: storepb.AgentStatus_ONLINE, deleted: true, connected: true, enabled: true, want: v1pb.AgentStatus_OFFLINE},
		{name: "stopped overrides online", status: storepb.AgentStatus_ONLINE, connected: true, enabled: false, want: v1pb.AgentStatus_STOPPED},
		{name: "stopped overrides offline", status: storepb.AgentStatus_OFFLINE, enabled: false, want: v1pb.AgentStatus_STOPPED},
		{name: "stopped overrides error", status: storepb.AgentStatus_ERROR, connected: true, enabled: false, want: v1pb.AgentStatus_STOPPED},
		{name: "stopped overrides kicked", status: storepb.AgentStatus_KICKED, connected: true, enabled: false, want: v1pb.AgentStatus_STOPPED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeConnectionState(&storepb.AgentStatus{State: tt.status}, tt.deleted, tt.connected, tt.enabled)
			if got != tt.want {
				t.Fatalf("computeConnectionState() = %v, want %v", got, tt.want)
			}
		})
	}
}
