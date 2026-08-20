package server

import (
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/labstack/echo/v5"

	"github.com/Ranxy/laelia/backend/manager/api/auth"
	apiv1 "github.com/Ranxy/laelia/backend/manager/api/v1"
)

// registerFileUploadRoute exposes a browser-friendly multipart upload endpoint.
// Unlike the Connect UploadFile RPC (which buffers the whole request in
// memory), this route streams the file directly to S3 and lets the frontend
// report real upload progress via XHR.
func registerFileUploadRoute(
	e *echo.Echo,
	authInterceptor *auth.APIAuthInterceptor,
	commandService *apiv1.CommandService,
) {
	e.POST("/v1/files/upload", func(c *echo.Context) error {
		// Cap the multipart body so a malicious client cannot stream an
		// unbounded request; the per-file size is validated again below.
		c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, apiv1.MaxStreamUploadBytes+1024*1024)

		ctx, err := authInterceptor.AuthenticateHTTP(c.Request().Context(), c.Request().Header, c.Request().RemoteAddr)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
		}

		fileHeader, err := c.FormFile("file")
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
			}
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing file"})
		}
		if fileHeader.Size > apiv1.MaxStreamUploadBytes {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "file too large"})
		}
		file, err := fileHeader.Open()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to open file"})
		}
		defer file.Close()

		user, _ := apiv1.GetUserFromContext(ctx)
		agent, _ := apiv1.GetAgentFromContext(ctx)

		f, err := commandService.UploadFileStream(ctx, &apiv1.UploadFileStreamInput{
			User:         user,
			Agent:        agent,
			Conversation: c.FormValue("conversation"),
			OriginalName: c.FormValue("originalName"),
			MimeType:     c.FormValue("mimeType"),
			SizeBytes:    fileHeader.Size,
			Body:         file,
		})
		if err != nil {
			code := connect.CodeOf(err)
			return c.JSON(httpStatusForCode(code), map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, f)
	})
}

func httpStatusForCode(code connect.Code) int {
	switch code {
	case connect.CodeInvalidArgument, connect.CodeFailedPrecondition:
		return http.StatusBadRequest
	case connect.CodeUnauthenticated:
		return http.StatusUnauthorized
	case connect.CodePermissionDenied:
		return http.StatusForbidden
	case connect.CodeNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
