package v1

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/machinebuild"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func (s *MachineService) convertToMachine(ctx context.Context, m *store.MachineMessage) *v1pb.Machine {
	state := v1pb.State_ACTIVE
	if m.Deleted {
		state = v1pb.State_DELETED
	}
	out := &v1pb.Machine{
		Name:      common.FormatMachineUID(m.ResourceID),
		State:     state,
		Title:     m.Name,
		Info:      convertToV1MachineInfo(m.Info),
		Status:    convertToV1MachineStatus(m.Status, m.Deleted),
		CreatedAt: timestamppb.New(m.CreatedAt),
	}
	if m.CreatedBy != 0 {
		out.CreatedBy = resolveUserResource(ctx, s.store, m.CreatedBy)
	}
	latest := machinebuild.LatestVersion()
	out.LatestVersion = latest
	out.UpgradeAvailable = machinebuild.UpgradeAvailable(m.Info.GetVersion(), latest)
	if s.dispatcher != nil {
		if st := s.dispatcher.MachineUpgradeStatus(m.ID); st != nil {
			out.UpgradeStatus = st
		}
	}
	return out
}

func (s *MachineService) convertToMachineSummary(ctx context.Context, m *store.MachineMessage, agentCount int) *v1pb.MachineSummary {
	state := v1pb.State_ACTIVE
	if m.Deleted {
		state = v1pb.State_DELETED
	}
	out := &v1pb.MachineSummary{
		Name:       common.FormatMachineUID(m.ResourceID),
		State:      state,
		Title:      m.Name,
		Status:     convertToV1MachineStatus(m.Status, m.Deleted),
		AgentCount: int32(agentCount),
		CreatedAt:  timestamppb.New(m.CreatedAt),
	}
	if m.CreatedBy != 0 {
		out.CreatedBy = resolveUserResource(ctx, s.store, m.CreatedBy)
	}
	latest := machinebuild.LatestVersion()
	out.LatestVersion = latest
	out.UpgradeAvailable = machinebuild.UpgradeAvailable(m.Info.GetVersion(), latest)
	return out
}

func convertToV1MachineInfo(info *storepb.MachineInfo) *v1pb.MachineInfo {
	if info == nil {
		return nil
	}
	return &v1pb.MachineInfo{
		Hostname:            info.Hostname,
		Os:                  info.Os,
		Arch:                info.Arch,
		Ip:                  info.Ip,
		Version:             info.Version,
		PromptBundleVersion: info.PromptBundleVersion,
		Labels:              info.Labels,
		Capability:          convertToV1AgentCapability(info.Capability),
		AvailableProviders:  convertToV1Providers(info.AvailableProviders),
	}
}

func convertToStoreMachineInfo(info *v1pb.MachineInfo) *storepb.MachineInfo {
	if info == nil {
		return nil
	}
	return &storepb.MachineInfo{
		Hostname:            info.Hostname,
		Os:                  info.Os,
		Arch:                info.Arch,
		Ip:                  info.Ip,
		Version:             info.Version,
		PromptBundleVersion: info.PromptBundleVersion,
		Labels:              info.Labels,
		Capability:          convertToStoreAgentCapability(info.Capability),
		AvailableProviders:  convertToStoreProviders(info.AvailableProviders),
	}
}

func convertToV1MachineStatus(status *storepb.MachineStatus, deleted bool) *v1pb.MachineStatus {
	if status == nil {
		return nil
	}
	var lastHeartbeatTime *timestamppb.Timestamp
	if status.LastHeartbeatAt > 0 {
		lastHeartbeatTime = timestamppb.New(time.Unix(status.LastHeartbeatAt, 0))
	}
	var connectedTime *timestamppb.Timestamp
	if status.ConnectedAt > 0 {
		connectedTime = timestamppb.New(time.Unix(status.ConnectedAt, 0))
	}
	return &v1pb.MachineStatus{
		State:             computeMachineConnectionState(status, deleted),
		LastHeartbeatTime: lastHeartbeatTime,
		ConnectedTime:     connectedTime,
		ErrorMessage:      status.ErrorMessage,
		ActiveSessionId:   status.ActiveSessionId,
	}
}

func computeMachineConnectionState(status *storepb.MachineStatus, deleted bool) v1pb.MachineStatus_ConnectionState {
	if status.State == storepb.MachineStatus_ERROR {
		return v1pb.MachineStatus_ERROR
	}
	if status.State == storepb.MachineStatus_KICKED {
		return v1pb.MachineStatus_KICKED
	}
	if deleted {
		return v1pb.MachineStatus_OFFLINE
	}
	threshold := time.Now().Unix() - agentOfflineThresholdSeconds
	if status.LastHeartbeatAt >= threshold {
		return v1pb.MachineStatus_ONLINE
	}
	return v1pb.MachineStatus_OFFLINE
}

// cloneStoreMachineInfo returns a deep copy of info safe to mutate before a
// partial UpdateMachine, or a fresh empty MachineInfo when info is nil.
func cloneStoreMachineInfo(info *storepb.MachineInfo) *storepb.MachineInfo {
	if info == nil {
		return &storepb.MachineInfo{}
	}
	cloned := proto.Clone(info)
	patchInfo, ok := cloned.(*storepb.MachineInfo)
	if !ok || patchInfo == nil {
		return &storepb.MachineInfo{}
	}
	return patchInfo
}
