package v1

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	defaultAuditPageSize = 100
	defaultExportLimit   = 10000
	maxExportLimit       = 100000
)

// AuditLogService implements the audit log search/export service.
type AuditLogService struct {
	v1connect.UnimplementedAuditLogServiceHandler
	store *store.Store
}

// NewAuditLogService returns a new AuditLogService.
func NewAuditLogService(s *store.Store) *AuditLogService {
	return &AuditLogService{store: s}
}

// SearchAuditLogs searches audit logs.
func (s *AuditLogService) SearchAuditLogs(ctx context.Context, req *connect.Request[v1pb.SearchAuditLogsRequest]) (*connect.Response[v1pb.SearchAuditLogsResponse], error) {
	page := int(req.Msg.GetPageSize())
	if page == 0 {
		page = defaultAuditPageSize
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   page,
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	find, err := auditLogFindFromRequest(req.Msg.GetFilter(), req.Msg.GetOrderBy(), offset.limit+1, offset.offset)
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListAuditLogs(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to search audit logs"))
	}

	nextPageToken := ""
	if len(records) == offset.limit+1 {
		records = records[:offset.limit]
		if nextPageToken, err = offset.getNextPageToken(); err != nil {
			return nil, err
		}
	}

	response := &v1pb.SearchAuditLogsResponse{NextPageToken: nextPageToken}
	for _, r := range records {
		response.AuditLogs = append(response.AuditLogs, convertToAuditLog(r))
	}
	return connect.NewResponse(response), nil
}

// ExportAuditLogs exports audit logs as CSV.
func (s *AuditLogService) ExportAuditLogs(ctx context.Context, req *connect.Request[v1pb.ExportAuditLogsRequest]) (*connect.Response[v1pb.ExportAuditLogsResponse], error) {
	limit := int(req.Msg.GetLimit())
	if limit == 0 {
		limit = defaultExportLimit
	}
	if limit > maxExportLimit {
		limit = maxExportLimit
	}
	find, err := auditLogFindFromRequest(req.Msg.GetFilter(), req.Msg.GetOrderBy(), limit, 0)
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListAuditLogs(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to export audit logs"))
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"name", "method", "actor_type", "actor_id", "source_ip", "status", "error", "resource", "payload", "create_time"}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	for _, r := range records {
		row := []string{
			fmt.Sprintf("auditLogs/%d", r.ID),
			r.Method,
			r.ActorType,
			r.ActorID,
			r.SourceIP,
			r.Status,
			r.Error,
			r.Resource,
			r.Payload,
			r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if err := w.Write(row); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1pb.ExportAuditLogsResponse{Content: buf.String()}), nil
}

// auditLogFindFromRequest parses the filter/order_by into the store find
// message. Supported filter fields (equality): method, actor, resource,
// status. Order: "" / "create_time desc" / "create_time asc".
func auditLogFindFromRequest(filter, orderBy string, limit, offset int) (*store.FindAuditLogMessage, error) {
	find := &store.FindAuditLogMessage{
		Limit:  &limit,
		Offset: &offset,
	}
	switch strings.TrimSpace(orderBy) {
	case "", "create_time desc":
	case "create_time asc":
		find.OrderAsc = true
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported order_by %q (want create_time desc or create_time asc)", orderBy))
	}
	if filter == "" {
		return find, nil
	}
	expressions, err := ParseFilter(filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	for _, e := range expressions {
		if e.Operator != ComparatorTypeEqual {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported operator %q for audit log filter", e.Operator))
		}
		switch e.Key {
		case "method":
			find.Method = &e.Value
		case "actor":
			find.ActorID = &e.Value
		case "resource":
			find.Resource = &e.Value
		case "status":
			find.Status = &e.Value
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported filter field %q", e.Key))
		}
	}
	return find, nil
}

// convertToAuditLog maps a store record to the v1 API shape.
func convertToAuditLog(r *store.AuditLogRecord) *v1pb.AuditLog {
	return &v1pb.AuditLog{
		Name:       fmt.Sprintf("auditLogs/%d", r.ID),
		Method:     r.Method,
		ActorType:  r.ActorType,
		ActorId:    r.ActorID,
		SourceIp:   r.SourceIP,
		Status:     r.Status,
		Error:      r.Error,
		Resource:   r.Resource,
		Payload:    r.Payload,
		CreateTime: timestamppb.New(r.CreatedAt),
	}
}
