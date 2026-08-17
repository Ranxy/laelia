package server

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/Ranxy/laelia/backend/manager/component/machinebuild"
	"github.com/Ranxy/laelia/backend/manager/store"
)

//go:embed install.sh.tmpl install.ps1.tmpl
var installTemplates embed.FS

const managerURLPlaceholder = "__LAELIA_MANAGER_URL__"

// registerMachineDownloadRoutes serves the embedded machine binaries and the
// install scripts. Downloads are intentionally unauthenticated: the installer
// runs on a fresh host before the machine has any credentials.
func registerMachineDownloadRoutes(e *echo.Echo, stores *store.Store) {
	// Publish the embedded build info (version + checksums) to the API layer
	// for the self-upgrade feature. No-op when binaries are not embedded.
	if manifest, err := machineManifest(); err == nil {
		machinebuild.SetManifest(manifest)
	}

	e.GET("/machine/install.sh", func(c *echo.Context) error {
		managerURL, err := externalManagerURL(c, stores)
		if err != nil {
			return err
		}
		script, err := installTemplates.ReadFile("install.sh.tmpl")
		if err != nil {
			return err
		}
		body := strings.ReplaceAll(string(script), managerURLPlaceholder, managerURL)
		return c.Blob(http.StatusOK, "text/x-shellscript; charset=utf-8", []byte(body))
	})

	e.GET("/machine/install.ps1", func(c *echo.Context) error {
		managerURL, err := externalManagerURL(c, stores)
		if err != nil {
			return err
		}
		script, err := installTemplates.ReadFile("install.ps1.tmpl")
		if err != nil {
			return err
		}
		body := strings.ReplaceAll(string(script), managerURLPlaceholder, managerURL)
		return c.Blob(http.StatusOK, "text/powershell; charset=utf-8", []byte(body))
	})

	e.GET("/machine/manifest.json", func(c *echo.Context) error {
		manifest, err := machineManifest()
		if err != nil {
			return c.NoContent(http.StatusNotFound)
		}
		return c.Blob(http.StatusOK, "application/json", manifest)
	})

	e.GET("/machine/bin/:target", func(c *echo.Context) error {
		target := c.Param("target")
		f, err := openMachineGz(target)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return c.NoContent(http.StatusNotFound)
			}
			return err
		}
		defer f.Close()
		return c.Stream(http.StatusOK, "application/gzip", f)
	})
}

// externalManagerURL resolves the manager's public URL from the workspace
// profile setting, falling back to the current request's scheme+host. The
// X-Forwarded-Proto header is honored so installs behind a TLS-terminating
// reverse proxy still receive an https manager URL.
func externalManagerURL(c *echo.Context, stores *store.Store) (string, error) {
	setting, err := stores.GetWorkspaceGeneralSetting(c.Request().Context())
	if err != nil {
		return "", err
	}
	if url := strings.TrimRight(setting.GetExternalUrl(), "/"); url != "" {
		return url, nil
	}
	scheme := "http"
	if proto := c.Request().Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if c.Request().TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request().Host, nil
}
