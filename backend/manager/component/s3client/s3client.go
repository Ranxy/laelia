// Package s3client builds and caches the S3 client used by file upload/download.
// It reads the connection details from the persisted S3 setting and rebuilds
// the client whenever the config changes (e.g. after an admin update).
package s3client

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// ErrS3NotConfigured is returned when no S3 endpoint/bucket has been set. The
// upload/download endpoints translate it into a "s3 not configured" response.
var ErrS3NotConfigured = errors.New("s3 not configured")

// Client is a lazily-built, cached S3 client keyed by the S3 config fingerprint.
type Client struct {
	store *store.Store

	mu          sync.Mutex
	client      *s3.Client
	cfg         *models.S3ConfigSetting
	fingerprint string
}

// New returns an S3 client component backed by the given store.
func New(stores *store.Store) *Client {
	return &Client{store: stores}
}

// Get returns the cached S3 client and its config, building (or rebuilding) it
// from the persisted setting when the config has changed. Returns
// ErrS3NotConfigured when endpoint and bucket are both empty.
func (c *Client) Get(ctx context.Context) (*s3.Client, *models.S3ConfigSetting, error) {
	cfg, err := c.store.GetS3ConfigSetting(ctx)
	if err != nil {
		return nil, nil, err
	}
	if cfg.Endpoint == "" && cfg.Bucket == "" {
		return nil, nil, ErrS3NotConfigured
	}

	fp := fingerprint(cfg)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil && fp == c.fingerprint {
		return c.client, c.cfg, nil
	}

	cli, err := build(cfg)
	if err != nil {
		return nil, nil, err
	}
	c.client = cli
	c.cfg = cfg
	c.fingerprint = fp
	return cli, cfg, nil
}

// Invalidate forces the next Get to rebuild the client. Called after the admin
// updates the S3 config.
func (c *Client) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client = nil
	c.cfg = nil
	c.fingerprint = ""
}

func build(cfg *models.S3ConfigSetting) (*s3.Client, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	awsCfg, err := awscfg.LoadDefaultConfig(context.Background(),
		awscfg.WithRegion(region),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load aws config")
	}

	s3Opts := []func(*s3.Options){
		func(o *s3.Options) {
			// MinIO and many S3-compatible stores require path-style addressing.
			if cfg.ForcePathStyle {
				o.UsePathStyle = true
			}
		},
	}

	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		// Allow callers to omit the scheme; use_ssl decides http vs https.
		if !strings.Contains(endpoint, "://") {
			scheme := "https"
			if !cfg.UseSsl {
				scheme = "http"
			}
			endpoint = scheme + "://" + endpoint
		}
		endpointURL, err := url.Parse(endpoint)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid s3 endpoint %q", cfg.Endpoint)
		}
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpointURL.String())
			if cfg.ForcePathStyle {
				o.UsePathStyle = true
			}
		})
	}

	// The default aws http client is fine; ensure no retries surprise callers
	// by leaving the SDK defaults.
	_ = http.DefaultClient
	return s3.NewFromConfig(awsCfg, s3Opts...), nil
}

func fingerprint(cfg *models.S3ConfigSetting) string {
	return strings.Join([]string{cfg.Endpoint, cfg.Region, cfg.Bucket, cfg.AccessKey, cfg.SecretKey, boolStr(cfg.ForcePathStyle), boolStr(cfg.UseSsl)}, "|")
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
