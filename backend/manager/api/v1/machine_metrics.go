package v1

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// machineMetricsRoundTripTimeout bounds the GetMachineMetrics scrape over the
// machine control stream. The machine renders its local registry into text;
// a healthy machine answers in milliseconds.
const machineMetricsRoundTripTimeout = 15 * time.Second

// GetMachineMetrics renders an online machine's local metrics (outbox lag and
// bytes, upload batching, barrier waits; design §8.2) in the Prometheus text
// exposition format. The scrape travels over the machine control stream, so
// machine-local observability works even though machines make outbound-only
// connections. Requires the machine creator or a workspace admin
// (isMachineAdmin, matching Machine.can_manage).
func (s *MachineService) GetMachineMetrics(ctx context.Context, req *connect.Request[v1pb.GetMachineMetricsRequest]) (*connect.Response[v1pb.GetMachineMetricsResponse], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	user, _ := GetUserFromContext(ctx)
	if !isMachineAdmin(ctx, s.iam, user, machine) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("metrics access requires machine creator or admin permission"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsMachineConnected(machine.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("machine is not connected; cannot scrape metrics"))
	}

	requestID := uuid.NewString()
	replyCh := s.dispatcher.RegisterPendingMetrics(requestID)
	defer s.dispatcher.CancelPendingMetrics(requestID)

	if err := s.dispatcher.SendMachineMetricsRequest(machine.ID, requestID); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to request machine metrics"))
	}

	select {
	case msg := <-replyCh:
		if msg == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("machine metrics returned no result"))
		}
		if msg.Error != "" {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("machine failed to render its metrics: %s", msg.Error))
		}
		return connect.NewResponse(&v1pb.GetMachineMetricsResponse{Payload: msg.Payload}), nil
	case <-time.After(machineMetricsRoundTripTimeout):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for machine metrics"))
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
	}
}
