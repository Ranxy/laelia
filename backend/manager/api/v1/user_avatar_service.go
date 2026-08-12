package v1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
)

// MaxAvatarBytes caps a single uploaded avatar. Clients resize before upload
// so this is a defense-in-depth ceiling, not the primary size control.
const MaxAvatarBytes = 2 * 1024 * 1024

// allowedAvatarMIME is the set of image types a user may upload as an avatar.
var allowedAvatarMIME = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/webp": "webp",
	"image/gif":  "gif",
}

// avatarS3KeyPrefix is the S3 key namespace for avatar objects. Each avatar is
// stored at avatars/{principal_id}/{content_hash}.{ext} so re-uploads with
// identical content reuse the key and updates produce a new cacheable key.
const avatarS3KeyPrefix = "avatars/"

// avatarExtensionFor returns the file extension for a sniffed/declared mime
// type, or "" if the type is not an allowed avatar image.
func avatarExtensionFor(mime string) (string, bool) {
	mime = strings.ToLower(strings.TrimSpace(mime))
	// image/jpeg is sometimes sniffed as image/jpeg; normalize synonyms.
	if mime == "image/jpg" {
		mime = "image/jpeg"
	}
	ext, ok := allowedAvatarMIME[mime]
	return ext, ok
}

// UploadAvatar replaces the current user's avatar image. Self only: the name
// in the request must match the caller. The bytes are stored in S3 and the
// user's avatar_s3_key updated; the previous object (if any) is deleted.
func (s *UserService) UploadAvatar(ctx context.Context, req *connect.Request[v1pb.UploadAvatarRequest]) (*connect.Response[v1pb.User], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	if int64(len(req.Msg.Data)) > MaxAvatarBytes {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("avatar too large: max %d bytes", MaxAvatarBytes))
	}
	if len(req.Msg.Data) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("avatar data is required"))
	}

	mime := sniffMimeType(req.Msg.MimeType, req.Msg.Data)
	ext, ok := avatarExtensionFor(mime)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported avatar image type %q (allowed: png, jpeg, webp, gif)", mime))
	}

	s3Cli, cfg, err := s.s3client.Get(ctx)
	if err != nil {
		if errors.Is(err, s3client.ErrS3NotConfigured) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("s3 not configured"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	hash := sha256.Sum256(req.Msg.Data)
	contentHash := hex.EncodeToString(hash[:])
	newKey := avatarS3KeyPrefix + common.FormatUserUID(user.ID) + "/" + contentHash + "." + ext

	if _, err := s3Cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(cfg.Bucket),
		Key:           aws.String(newKey),
		Body:          bytes.NewReader(req.Msg.Data),
		ContentType:   aws.String(mime),
		ContentLength: aws.Int64(int64(len(req.Msg.Data))),
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "s3 put failed"))
	}

	prevKey := user.AvatarS3Key
	if err := s.store.UpdateUserAvatarS3Key(ctx, user.ID, newKey); err != nil {
		// Roll back the S3 put so we don't leak an orphaned object.
		if delErr := deleteS3Object(ctx, s3Cli, cfg.Bucket, newKey); delErr != nil {
			slog.Warn("failed to clean up avatar after db update failure", log.WithError(delErr))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to persist avatar"))
	}

	// Best-effort delete of the previous avatar object now that the new key is
	// committed. A failure here only leaves an orphan, not user-visible error.
	if prevKey != "" && prevKey != newKey {
		if err := deleteS3Object(ctx, s3Cli, cfg.Bucket, prevKey); err != nil {
			slog.Warn("failed to delete previous avatar object", "key", prevKey, log.WithError(err))
		}
	}

	updated, err := s.store.GetUserByID(ctx, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(convertToUser(updated, false)), nil
}

// DownloadAvatar fetches a user's avatar image bytes. Any authenticated user
// may download any user's avatar (workspace-internal profile image).
func (s *UserService) DownloadAvatar(ctx context.Context, req *connect.Request[v1pb.DownloadAvatarRequest]) (*connect.Response[v1pb.DownloadAvatarResponse], error) {
	if _, ok := GetUserFromContext(ctx); !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	uid, err := common.ParseUserAvatarName(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid avatar name"))
	}
	user, err := s.store.GetUserByID(ctx, uid)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if user == nil || user.AvatarS3Key == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("avatar not found"))
	}

	s3Cli, cfg, err := s.s3client.Get(ctx)
	if err != nil {
		if errors.Is(err, s3client.ErrS3NotConfigured) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("s3 not configured"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out, err := s3Cli.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(cfg.Bucket),
		Key:    aws.String(user.AvatarS3Key),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "s3 get failed"))
	}
	defer out.Body.Close()

	// Cap reads at the upload ceiling so a corrupted/oversized object can't
	// exhaust memory; avatars are resized small so this is well under the cap.
	data, err := io.ReadAll(io.LimitReader(out.Body, MaxAvatarBytes+1))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read avatar body"))
	}

	hash := sha256.Sum256(data)
	return connect.NewResponse(&v1pb.DownloadAvatarResponse{
		Data:     data,
		MimeType: sniffMimeType("", data),
		Etag:     hex.EncodeToString(hash[:]),
	}), nil
}

// DeleteAvatar clears the current user's avatar, reverting to the pixel
// default. Self only: the name in the request must match the caller.
func (s *UserService) DeleteAvatar(ctx context.Context, req *connect.Request[v1pb.DeleteAvatarRequest]) (*connect.Response[v1pb.User], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	uid, err := common.ParseUserAvatarName(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid avatar name"))
	}
	if uid != user.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot delete another user's avatar"))
	}
	if user.AvatarS3Key == "" {
		// Already cleared; return the user as-is.
		return connect.NewResponse(convertToUser(user, false)), nil
	}

	s3Cli, cfg, err := s.s3client.Get(ctx)
	if err != nil {
		if errors.Is(err, s3client.ErrS3NotConfigured) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("s3 not configured"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	prevKey := user.AvatarS3Key
	if err := s.store.UpdateUserAvatarS3Key(ctx, user.ID, ""); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to clear avatar"))
	}
	if err := deleteS3Object(ctx, s3Cli, cfg.Bucket, prevKey); err != nil {
		slog.Warn("failed to delete avatar object", "key", prevKey, log.WithError(err))
	}

	updated, err := s.store.GetUserByID(ctx, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(convertToUser(updated, false)), nil
}

// deleteS3Object is a best-effort helper that swallows object-not-found errors
// (a missing object is the desired end state for a delete).
func deleteS3Object(ctx context.Context, cli *s3.Client, bucket, key string) error {
	_, err := cli.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return errors.Wrap(err, "s3 delete failed")
}
