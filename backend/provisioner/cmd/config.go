package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/pkg/errors"
	goyaml "go.yaml.in/yaml/v3"

	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

// Config is the provisioner's configuration: yaml file (--config) overlaid by
// the --manager/--token/--backend flags.
type Config struct {
	// ManagerURL is the manager the provisioner connects to.
	ManagerURL string `yaml:"manager_url"`
	// Token is the one-time provisioner token minted at registration.
	Token string `yaml:"token"`
	// Backend names the workload backend to drive (e.g. "kubernetes").
	Backend string `yaml:"backend"`
	// Namespace is the backend-scoped namespace workloads land in.
	Namespace string `yaml:"namespace"`
	// ManagerURLOverride replaces job manager_url so pods reach the manager
	// through an in-cluster service URL instead of the public one.
	ManagerURLOverride string `yaml:"manager_url_override"`
	// RetainData preserves machine data volumes when the backend supports
	// retention (default: delete them with the workload).
	RetainData bool `yaml:"retain_data"`
	// AutoUpgrade lets the manager auto-upgrade this provisioner's machines
	// when a new binary version is available (default off).
	AutoUpgrade bool `yaml:"auto_upgrade"`
	// Resources sizes machine workloads (backend passthrough).
	Resources backend.Resources `yaml:"resources"`
	// Storage sizes machine data volumes (backend passthrough; the kubernetes
	// backend uses it for the data PVC). Empty size = backend default.
	Storage backend.Storage `yaml:"storage"`
	// ParamBounds limits the machine parameters users may set at
	// ProvisionMachine time (catalog keys → inclusive min/max, k8s
	// quantities; an omitted side is unbounded). Reported with the parameter
	// schema and enforced by the manager.
	ParamBounds map[string]paramBounds `yaml:"param_bounds"`
	// ExtraEnv passes additional environment entries into machine workloads
	// (e.g. LAELIA_INSECURE for a self-signed manager certificate).
	ExtraEnv map[string]string `yaml:"extra_env"`
}

// paramBounds are the yaml shape of one parameter's inclusive bounds.
type paramBounds struct {
	Min string `yaml:"min"`
	Max string `yaml:"max"`
}

// loadConfig reads the yaml config (when --config is set) and applies the
// flag overrides; manager_url, token, and backend are required.
func loadConfig() (*Config, error) {
	cfg := &Config{}
	if flags.config != "" {
		data, err := os.ReadFile(flags.config)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read provisioner config")
		}
		if err := goyaml.Unmarshal(data, cfg); err != nil {
			return nil, errors.Wrap(err, "failed to parse provisioner config")
		}
	}
	if flags.manager != "" {
		cfg.ManagerURL = flags.manager
	}
	if flags.token != "" {
		cfg.Token = flags.token
	}
	// In-cluster deployments inject the token as an environment variable from
	// a Secret (deploy/deployment.yaml); it applies last so file/flag values
	// always win.
	if cfg.Token == "" {
		if envToken := os.Getenv("LAELIA_PROVISIONER_TOKEN"); envToken != "" {
			cfg.Token = envToken
		}
	}
	if flags.backend != "" {
		cfg.Backend = flags.backend
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate checks the required configuration surface.
func (c *Config) validate() error {
	if strings.TrimSpace(c.ManagerURL) == "" {
		return errors.New("manager_url is required (config file or --manager)")
	}
	if c.Token == "" {
		return errors.New("token is required (config file or --token)")
	}
	if strings.TrimSpace(c.Backend) == "" {
		return errors.New("backend is required (config file or --backend)")
	}
	return nil
}

// backendConfig is the backend-neutral slice handed to the backend factory.
func (c *Config) backendConfig() backend.Config {
	bounds := make(map[string]backend.ParamBounds, len(c.ParamBounds))
	for key, b := range c.ParamBounds {
		bounds[key] = backend.ParamBounds{Min: b.Min, Max: b.Max}
	}
	return backend.Config{
		Namespace:   c.Namespace,
		RetainData:  c.RetainData,
		Resources:   c.Resources,
		Storage:     c.Storage,
		ExtraEnv:    c.ExtraEnv,
		ParamBounds: bounds,
	}
}

// Digest is the short hash of the backend-affecting configuration, reported
// in ProvisionerReady for drift visibility. Secrets (token) and endpoint
// plumbing (manager_url, insecure flags) are deliberately excluded.
func (c *Config) Digest() string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "backend=%s\n", c.Backend)
	_, _ = fmt.Fprintf(h, "namespace=%s\n", c.Namespace)
	_, _ = fmt.Fprintf(h, "manager_url_override=%s\n", c.ManagerURLOverride)
	_, _ = fmt.Fprintf(h, "retain_data=%t\n", c.RetainData)
	_, _ = fmt.Fprintf(h, "storage_size=%s\n", c.Storage.Size)
	_, _ = fmt.Fprintf(h, "storage_class=%s\n", c.Storage.StorageClassName)
	writeSortedEntries := func(label string, entries map[string]string) {
		keys := make([]string, 0, len(entries))
		for k := range entries {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		_, _ = fmt.Fprintf(h, "%s.count=%d\n", label, len(keys))
		for _, k := range keys {
			_, _ = fmt.Fprintf(h, "%s.%s=%s\n", label, k, entries[k])
		}
	}
	writeSortedEntries("resource_requests", c.Resources.Requests)
	writeSortedEntries("resource_limits", c.Resources.Limits)
	writeSortedEntries("extra_env", c.ExtraEnv)
	writeSortedBounds(h, "param_bounds", c.ParamBounds)
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// writeSortedBounds hashes the parameter-bounds map in stable key order.
func writeSortedBounds(h io.Writer, label string, entries map[string]paramBounds) {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	_, _ = fmt.Fprintf(h, "%s.count=%d\n", label, len(keys))
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%s.%s={min:%s,max:%s}\n", label, k, entries[k].Min, entries[k].Max)
	}
}
