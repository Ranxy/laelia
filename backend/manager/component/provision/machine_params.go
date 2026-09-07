// The machine-parameter catalog: the manager-owned universe of
// user-customizable machine parameters (design
// docs/plan/provisioner-machine-params-design.md). Each provisioner reports
// which catalog keys it accepts at connect time (ProvisionerReady), each with
// per-instance defaults and bounds; ValidateMachineSchema filters that report
// against this catalog and ValidateMachineParams checks user-provided values
// against the persisted schema. Only catalog keys ever reach a workload spec,
// and every value is tightly shaped — the fail-fast validation layer in front
// of the backend's own authoritative validation.
package provision

import (
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common/machineparam"
	"github.com/Ranxy/laelia/backend/common/quantity"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

// maxParamValueLength bounds one parameter value (quantities and DNS labels
// are both far shorter).
const maxParamValueLength = 64

// dnsLabelPattern is the k8s object-name shape (RFC 1123 label): lowercase
// alphanumerics with inner dashes.
var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// catalog maps every manager-known parameter key to its value type. Keys
// absent from this map are rejected wherever they appear: a provisioner
// reporting an unknown key has its entry dropped (forward compatibility: a
// newer provisioner paired with an older manager degrades, never breaks).
var catalog = map[string]storepb.MachineParamType{
	machineparam.CPU:          storepb.MachineParamType_MACHINE_PARAM_TYPE_QUANTITY,
	machineparam.Memory:       storepb.MachineParamType_MACHINE_PARAM_TYPE_QUANTITY,
	machineparam.Disk:         storepb.MachineParamType_MACHINE_PARAM_TYPE_QUANTITY,
	machineparam.StorageClass: storepb.MachineParamType_MACHINE_PARAM_TYPE_STRING,
}

// CatalogType reports the value type of one catalog key.
func CatalogType(key string) (storepb.MachineParamType, bool) {
	typ, ok := catalog[key]
	return typ, ok
}

// ValidateMachineSchema filters a provisioner-reported parameter schema
// against the catalog: unknown keys are dropped (logged), duplicate keys
// collapse (first wins), and entries whose default or bounds fail value
// validation are dropped too, so a malformed report can never poison the
// persisted schema. The filtered schema keeps the report's order.
func ValidateMachineSchema(specs []*storepb.MachineParamSpec) []*storepb.MachineParamSpec {
	var out []*storepb.MachineParamSpec
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		key := strings.TrimSpace(spec.GetKey())
		typ, ok := catalog[key]
		if !ok {
			slog.Warn("provisioner reported an unknown machine parameter; dropping it", "key", key)
			continue
		}
		if _, dup := seen[key]; dup {
			slog.Warn("provisioner reported a duplicate machine parameter; keeping the first", "key", key)
			continue
		}
		if err := validateSchemaEntry(typ, spec); err != nil {
			slog.Warn("provisioner reported an invalid machine parameter; dropping it", "key", key, "error", err)
			continue
		}
		seen[key] = struct{}{}
		out = append(out, spec)
	}
	return out
}

// validateSchemaEntry checks one reported entry: the default must pass value
// validation (the form placeholder must be usable), QUANTITY bounds must
// parse and order correctly, and bounds on non-quantity params are ignored
// (cleared) rather than trusted.
func validateSchemaEntry(typ storepb.MachineParamType, spec *storepb.MachineParamSpec) error {
	if spec.GetRequired() && spec.GetDefaultValue() == "" {
		return errors.New("required parameter has no default")
	}
	if def := strings.TrimSpace(spec.GetDefaultValue()); def != "" {
		if err := validateParamValue(typ, def, nil); err != nil {
			return errors.Wrap(err, "invalid default")
		}
	}
	if typ != storepb.MachineParamType_MACHINE_PARAM_TYPE_QUANTITY {
		spec.MinValue = ""
		spec.MaxValue = ""
		return nil
	}
	lower, upper, err := parseBounds(spec.GetMinValue(), spec.GetMaxValue())
	if err != nil {
		return err
	}
	if lower != nil && upper != nil && *lower > *upper {
		return errors.Errorf("min %q exceeds max %q", spec.GetMinValue(), spec.GetMaxValue())
	}
	// The default is the form placeholder: it must be usable as-is, so it is
	// bounds-checked too — a default outside its own bounds is a config error.
	if def := strings.TrimSpace(spec.GetDefaultValue()); def != "" {
		parsed, err := quantity.Parse(def)
		if err != nil {
			return err // unreachable: the shape check above parsed it
		}
		if lower != nil && parsed < *lower {
			return errors.Errorf("default %q is below the minimum %q", def, spec.GetMinValue())
		}
		if upper != nil && parsed > *upper {
			return errors.Errorf("default %q is above the maximum %q", def, spec.GetMaxValue())
		}
	}
	return nil
}

// ValidateMachineParams validates user-provided parameter values against the
// catalog and the target provisioner's persisted schema, returning the values
// to persist. Empty values are treated as absent (the provisioner default
// applies). Every failure is InvalidArgument at the API boundary: the error
// names the offending key.
func ValidateMachineParams(schema []*storepb.MachineParamSpec, values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	declared := make(map[string]*storepb.MachineParamSpec, len(schema))
	for _, spec := range schema {
		declared[spec.GetKey()] = spec
	}

	out := make(map[string]string, len(values))
	for _, key := range slices.Sorted(maps.Keys(values)) {
		value := strings.TrimSpace(values[key])
		if value == "" {
			continue // absent: the provisioner default applies
		}
		typ, ok := catalog[key]
		if !ok {
			return nil, errors.Errorf("unknown machine parameter %q", key)
		}
		spec, ok := declared[key]
		if !ok {
			return nil, errors.Errorf("machine parameter %q is not accepted by this provisioner", key)
		}
		if err := validateParamValue(typ, value, spec.GetOptions()); err != nil {
			return nil, errors.Wrapf(err, "invalid machine parameter %q", key)
		}
		if typ == storepb.MachineParamType_MACHINE_PARAM_TYPE_QUANTITY {
			if err := validateBounds(spec, key, value); err != nil {
				return nil, err
			}
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// validateParamValue checks one value against its catalog type and, when the
// schema constrains it, its declared options.
func validateParamValue(typ storepb.MachineParamType, value string, options []string) error {
	if len(value) > maxParamValueLength {
		return errors.Errorf("value exceeds %d characters", maxParamValueLength)
	}
	switch typ {
	case storepb.MachineParamType_MACHINE_PARAM_TYPE_QUANTITY:
		if _, err := quantity.Parse(value); err != nil {
			return errors.Wrap(err, "not a valid quantity")
		}
	case storepb.MachineParamType_MACHINE_PARAM_TYPE_STRING:
		if !dnsLabelPattern.MatchString(value) {
			return errors.Errorf("%q is not a valid name (lowercase alphanumerics and dashes)", value)
		}
	default:
		return errors.Errorf("unsupported parameter type %s", typ)
	}
	if len(options) > 0 && !slices.Contains(options, value) {
		return errors.Errorf("%q is not one of the allowed values (%s)", value, strings.Join(options, ", "))
	}
	return nil
}

// parseBounds parses one or both QUANTITY bounds; nil means unbounded.
func parseBounds(minValue, maxValue string) (*quantity.Milli, *quantity.Milli, error) {
	var lower, upper *quantity.Milli
	if minValue != "" {
		parsed, err := quantity.Parse(minValue)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "invalid minimum %q", minValue)
		}
		lower = &parsed
	}
	if maxValue != "" {
		parsed, err := quantity.Parse(maxValue)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "invalid maximum %q", maxValue)
		}
		upper = &parsed
	}
	return lower, upper, nil
}

// validateBounds enforces the schema's inclusive min/max on one value.
func validateBounds(spec *storepb.MachineParamSpec, key, value string) error {
	lower, upper, err := parseBounds(spec.GetMinValue(), spec.GetMaxValue())
	if err != nil {
		// The schema was validated at report time; a failure here means the
		// persisted entry is corrupt — treat it as unbounded rather than
		// failing the provision.
		slog.Warn("provisioner parameter schema has invalid bounds; ignoring them", "key", key, "error", err)
		return nil
	}
	parsed, err := quantity.Parse(value)
	if err != nil {
		return err // unreachable: validateParamValue already parsed it
	}
	if lower != nil && parsed < *lower {
		return errors.Errorf("machine parameter %q value %q is below the minimum %q", key, value, spec.GetMinValue())
	}
	if upper != nil && parsed > *upper {
		return errors.Errorf("machine parameter %q value %q is above the maximum %q", key, value, spec.GetMaxValue())
	}
	return nil
}
