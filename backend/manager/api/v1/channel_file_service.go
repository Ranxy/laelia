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

// UploadFileStreamInput carries the metadata + streaming body for a file
// upload. The browser multipart route uses this so large files stream straight
// to S3 instead of being buffered in memory.
type UploadFileStreamInput struct {
	User         *store.UserMessage
	Agent        *store.AgentMessage
	Conversation string
	OriginalName string
	MimeType     string
	SizeBytes    int64
	Body         io.Reader
}

// UploadFileStream stores a blob in S3 and persists a file row, streaming the
// body to S3 instead of buffering it in memory. It performs the same
// auth/membership checks as UploadFile.
func (s *CommandService) UploadFileStream(ctx context.Context, in *UploadFileStreamInput) (*v1pb.File, error) {
	if in.OriginalName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("original_name is required"))
	}
	if in.SizeBytes > MaxUploadBytes {
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
	if in.User != nil {
		uploaderPrincipalID = in.User.ID
	}

	fileRow := &store.File{
		UploaderPrincipalID: uploaderPrincipalID,
		OriginalName:        in.OriginalName,
		MimeType:            in.MimeType,
		SizeBytes:           in.SizeBytes,
	}
	if in.Conversation != "" {
		convID, err := parseConversationID(in.Conversation)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation id"))
		}
		// Agent-DM conversations (type 3) are agent-only. Agents exchange
		// files in their own DMs; users — including workspace admins, who can
		// view via the admin bypass — must not upload into one.
		if in.User != nil {
			conv, convErr := s.store.GetConversation(ctx, convID)
			if convErr != nil {
				return nil, connect.NewError(connect.CodeNotFound, convErr)
			}
			if conv.Type == store.ConversationTypeAgentDM {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("agent-DM conversations are agent-only; users can view but cannot upload"))
			}
		}
		// Conversation-tied uploads require membership, mirroring the download
		// side (files.download resolves the file's conversation and denies
		// non-members). files.upload is workspace-baseline so the agent file
		// tool can upload conversation-less blobs, so without this gate any
		// authenticated user could spray blobs into arbitrary conversation
		// UUIDs. Untied uploads stay workspace-baseline (downloads remain
		// uploader-only).
		memberType, memberID, ok := callerMemberInfo(in.User, in.Agent)
		if !ok {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
		}
		isMember, memErr := s.store.IsConversationMember(ctx, convID, memberType, memberID)
		if memErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(memErr, "failed to check conversation membership"))
		}
		if !isMember {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only conversation members can upload files"))
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
		Body:          in.Body,
		ContentType:   aws.String(fileRow.MimeType),
		ContentLength: aws.Int64(fileRow.SizeBytes),
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "s3 put failed"))
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to commit pending scheduler transaction"))
	}

	return fileToV1(fileRow), nil
}

// UploadFile stores a blob in S3 and persists a file row. Both browser users
// and agents call this; the caller must be a member of the conversation when
// one is supplied.
func (s *CommandService) UploadFile(ctx context.Context, req *connect.Request[v1pb.UploadFileRequest]) (*connect.Response[v1pb.File], error) {
	user, agent, err := resolveFileCaller(ctx)
	if err != nil {
		return nil, err
	}
	mimeType := sniffMimeType(req.Msg.MimeType, req.Msg.Data)
	file, err := s.UploadFileStream(ctx, &UploadFileStreamInput{
		User:         user,
		Agent:        agent,
		Conversation: req.Msg.Conversation,
		OriginalName: req.Msg.OriginalName,
		MimeType:     mimeType,
		SizeBytes:    int64(len(req.Msg.Data)),
		Body:         bytes.NewReader(req.Msg.Data),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(file), nil
}

// DownloadFile fetches a file's bytes from S3. The caller must be a member of
// the file's conversation; untied files are uploader-only (agents are denied,
// since they don't own untied user files).
func (s *CommandService) DownloadFile(ctx context.Context, req *connect.Request[v1pb.DownloadFileRequest]) (*connect.Response[v1pb.DownloadFileResponse], error) {
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
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation id"))
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

// resolveAttachments validates the attachment ids on a message being posted to
// convID and returns full Attachment metadata (name/mime/size) read from the
// file rows. Callers (the agent CLI) only carry the file id from `file upload`
// output; the file row is the source of truth for the rest. Each attachment must
// reference a file tied to convID — this is what binds an uploaded file to the
// message that carries it and prevents referencing files from another
// conversation. An empty/invalid id, a missing file, or a file tied elsewhere
// is rejected.
//
// The anchor fields (section_anchor/section_id/quoted_text) are caller-supplied
// semantics, not file-row metadata, so they are preserved verbatim while the
// file metadata (name/mime/size) is normalized from the file row. This is what
// carries a "comment on a section of a file" as a thread reply.
func (s *CommandService) resolveAttachments(ctx context.Context, convID uuid.UUID, attachments []*v1pb.Attachment) ([]*v1pb.Attachment, error) {
	if len(attachments) == 0 {
		return attachments, nil
	}
	resolved := make([]*v1pb.Attachment, 0, len(attachments))
	for _, a := range attachments {
		if a == nil || a.Id == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("attachment id is required"))
		}
		fid, err := uuid.Parse(a.Id)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid attachment id %q", a.Id))
		}
		f, err := s.store.GetFile(ctx, fid)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if f == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("attachment file %s not found", a.Id))
		}
		if !f.ConversationID.Valid || f.ConversationID.UUID != convID {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("file %s is not attached to this conversation; upload it with --conversation first", a.Id))
		}
		resolved = append(resolved, &v1pb.Attachment{
			Id:            f.ID.String(),
			Name:          f.OriginalName,
			MimeType:      f.MimeType,
			SizeBytes:     f.SizeBytes,
			SectionAnchor: a.SectionAnchor,
			SectionId:     a.SectionId,
			QuotedText:    a.QuotedText,
		})
	}
	return resolved, nil
}
