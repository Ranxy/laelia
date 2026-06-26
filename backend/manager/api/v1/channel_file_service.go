package v1

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// MaxUploadBytes caps a single uploaded/downloaded file. It bounds the in-memory
// buffering the bytes-based file RPCs do and matches the connect.WithReadMaxBytes
// limit applied to the handler.
const MaxUploadBytes = 100 * 1024 * 1024

// resolveFileCaller returns the user or agent making the call. The auth
// interceptor injects exactly one of them (user token vs agent token); both nil
// means unauthenticated.
func resolveFileCaller(ctx context.Context) (*store.UserMessage, *store.AgentMessage, error) {
	user, _ := GetUserFromContext(ctx)
	agent, _ := GetAgentFromContext(ctx)
	if user == nil && agent == nil {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	return user, agent, nil
}

// requireFileMember parses the conversation id and checks the caller is a
// member, using the right MemberType for a user vs agent caller.
func requireFileMember(
	ctx context.Context,
	stores *store.Store,
	conversation string,
	user *store.UserMessage,
	agent *store.AgentMessage,
) (uuid.UUID, error) {
	convID, err := parseConversationID(conversation)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation id"))
	}
	var (
		memberID   string
		memberType int32
	)
	switch {
	case user != nil:
		memberType = store.MemberTypeUser
		memberID = fmt.Sprintf("%d", user.ID)
	case agent != nil:
		memberType = store.MemberTypeAgent
		memberID = agent.ResourceID
	default:
		return uuid.Nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	ok, err := stores.IsConversationMember(ctx, convID, memberType, memberID)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check conversation membership"))
	}
	if !ok {
		return uuid.Nil, connect.NewError(connect.CodePermissionDenied, errors.New("not a conversation member"))
	}
	return convID, nil
}

// sniffMimeType returns the request's declared mime type, or — when empty —
// the type detected from the first 512 bytes of the file content.
func sniffMimeType(declared string, data []byte) string {
	if declared != "" {
		return declared
	}
	n := len(data)
	if n > 512 {
		n = 512
	}
	if n == 0 {
		return ""
	}
	return http.DetectContentType(data[:n])
}

// UploadFile stores a blob in S3 and persists a file row. Both browser users
// and agents call this; the caller must be a member of the conversation when
// one is supplied.
func (s *CommandService) UploadFile(ctx context.Context, req *connect.Request[v1pb.UploadFileRequest]) (*connect.Response[v1pb.File], error) {
	user, agent, err := resolveFileCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.OriginalName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("original_name is required"))
	}
	if int64(len(req.Msg.Data)) > MaxUploadBytes {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("file too large"))
	}

	s3Cli, cfg, err := s.s3clientManager.Get(ctx)
	if err != nil {
		if errors.Is(err, s3client.ErrS3NotConfigured) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("s3 not configured"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Agents don't map to a principal row; use the system principal (id 1).
	// Agent uploads are always tied to a conversation, so access is governed by
	// membership, not this field.
	uploaderPrincipalID := 1
	if user != nil {
		uploaderPrincipalID = user.ID
	}
	mimeType := sniffMimeType(req.Msg.MimeType, req.Msg.Data)

	fileRow := &store.File{
		UploaderPrincipalID: uploaderPrincipalID,
		OriginalName:        req.Msg.OriginalName,
		MimeType:            mimeType,
		SizeBytes:           int64(len(req.Msg.Data)),
	}
	if req.Msg.Conversation != "" {
		convID, err := requireFileMember(ctx, s.store, req.Msg.Conversation, user, agent)
		if err != nil {
			return nil, err
		}
		fileRow.ConversationID = toNullUUID(convID)
	}

	tx, err := s.store.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to begin pending scheduler transaction"))
	}

	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Error("Failed to rollback pending upload file to s3", slog.String("err", rollbackErr.Error()))
		}
	}()

	fileRow, err = s.store.CreateFile(ctx, tx, fileRow)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if _, err := s3Cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(cfg.Bucket),
		Key:           aws.String(fileRow.S3Key),
		Body:          bytes.NewReader(req.Msg.Data),
		ContentType:   aws.String(fileRow.MimeType),
		ContentLength: aws.Int64(fileRow.SizeBytes),
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "s3 put failed"))
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to commit pending scheduler transaction"))
	}

	return connect.NewResponse(fileToV1(fileRow)), nil
}

// DownloadFile fetches a file's bytes from S3. The caller must be a member of
// the file's conversation; untied files are uploader-only (agents are denied,
// since they don't own untied user files).
func (s *CommandService) DownloadFile(ctx context.Context, req *connect.Request[v1pb.DownloadFileRequest]) (*connect.Response[v1pb.DownloadFileResponse], error) {
	user, agent, err := resolveFileCaller(ctx)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid file id"))
	}

	fileRow, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if fileRow == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("file not found"))
	}

	if fileRow.ConversationID.Valid {
		if _, err := requireFileMember(ctx, s.store, "conversations/"+fileRow.ConversationID.UUID.String(), user, agent); err != nil {
			return nil, err
		}
	} else if user == nil || fileRow.UploaderPrincipalID != user.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("not the file owner"))
	}

	s3Cli, cfg, err := s.s3clientManager.Get(ctx)
	if err != nil {
		if errors.Is(err, s3client.ErrS3NotConfigured) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("s3 not configured"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out, err := s3Cli.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(cfg.Bucket),
		Key:    aws.String(fileRow.S3Key),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "s3 get failed"))
	}
	defer out.Body.Close()

	data, err := readAll(out.Body)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read file body"))
	}

	return connect.NewResponse(&v1pb.DownloadFileResponse{
		File: fileToV1(fileRow),
		Data: data,
	}), nil
}

// ListFiles returns the files attached to a conversation. The caller must be a
// member.
func (s *CommandService) ListFiles(ctx context.Context, req *connect.Request[v1pb.ListFilesRequest]) (*connect.Response[v1pb.ListFilesResponse], error) {
	user, agent, err := resolveFileCaller(ctx)
	if err != nil {
		return nil, err
	}
	convID, err := requireFileMember(ctx, s.store, req.Msg.Conversation, user, agent)
	if err != nil {
		return nil, err
	}

	files, err := s.store.ListFilesByConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	v1Files := make([]*v1pb.File, 0, len(files))
	for _, f := range files {
		v1Files = append(v1Files, fileToV1(f))
	}
	return connect.NewResponse(&v1pb.ListFilesResponse{Files: v1Files}), nil
}

// readAll reads the object body, capping at maxUploadBytes+1 so a corrupted/
// oversized object can't exhaust memory.
func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, MaxUploadBytes+1))
}

// fileToV1 converts a store file row to the proto File message.
func fileToV1(f *store.File) *v1pb.File {
	if f == nil {
		return nil
	}
	conv := ""
	if f.ConversationID.Valid {
		conv = "conversations/" + f.ConversationID.UUID.String()
	}
	return &v1pb.File{
		Id:                  f.ID.String(),
		Conversation:        conv,
		UploaderPrincipalId: fmt.Sprintf("%d", f.UploaderPrincipalID),
		OriginalName:        f.OriginalName,
		MimeType:            f.MimeType,
		SizeBytes:           f.SizeBytes,
		S3Key:               f.S3Key,
		CreatedAt:           timestamppb.New(f.CreatedAt),
	}
}

func toNullUUID(id uuid.UUID) uuid.NullUUID {
	if id == uuid.Nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: id, Valid: true}
}
