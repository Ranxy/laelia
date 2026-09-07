package provision

import (
	"testing"

	"github.com/Ranxy/laelia/backend/common/machineparam"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

func spec(key, def, lower, upper string) *storepb.MachineParamSpec {
	return &storepb.MachineParamSpec{
		Key:          key,
		DefaultValue: def,
		MinValue:     lower,
		MaxValue:     upper,
	}
}

func TestValidateMachineSchema(t *testing.T) {
	tests := []struct {
		name string
		in   []*storepb.MachineParamSpec
		want []string // surviving keys, in order
	}{
		{
			name: "valid schema survives",
			in: []*storepb.MachineParamSpec{
				spec(machineparam.CPU, "1", "250m", "8"),
				spec(machineparam.Memory, "2Gi", "512Mi", "32Gi"),
				spec(machineparam.Disk, "10Gi", "1Gi", ""),
			},
			want: []string{machineparam.CPU, machineparam.Memory, machineparam.Disk},
		},
		{
			name: "unknown key dropped",
			in:   []*storepb.MachineParamSpec{spec("gpu", "", "", "")},
			want: nil,
		},
		{
			name: "duplicate key keeps first",
			in: []*storepb.MachineParamSpec{
				spec(machineparam.CPU, "1", "", ""),
				spec(machineparam.CPU, "2", "", ""),
			},
			want: []string{machineparam.CPU},
		},
		{
			name: "bad default dropped",
			in:   []*storepb.MachineParamSpec{spec(machineparam.CPU, "abc", "", "")},
			want: nil,
		},
		{
			name: "default outside bounds dropped",
			in:   []*storepb.MachineParamSpec{spec(machineparam.CPU, "16", "", "8")},
			want: nil,
		},
		{
			name: "unparseable min dropped",
			in:   []*storepb.MachineParamSpec{spec(machineparam.Disk, "10Gi", "abc", "")},
			want: nil,
		},
		{
			name: "min above max dropped",
			in:   []*storepb.MachineParamSpec{spec(machineparam.Memory, "2Gi", "32Gi", "512Mi")},
			want: nil,
		},
		{
			name: "string bounds cleared",
			in: []*storepb.MachineParamSpec{{
				Key: machineparam.StorageClass, MinValue: "10Gi", MaxValue: "1Gi",
			}},
			want: []string{machineparam.StorageClass},
		},
	}
	for _, tt := range tests {
		got := ValidateMachineSchema(tt.in)
		var keys []string
		for _, s := range got {
			keys = append(keys, s.GetKey())
		}
		if len(keys) != len(tt.want) {
			t.Errorf("%s: keys = %v, want %v", tt.name, keys, tt.want)
			continue
		}
		for i := range keys {
			if keys[i] != tt.want[i] {
				t.Errorf("%s: keys[%d] = %q, want %q", tt.name, i, keys[i], tt.want[i])
			}
		}
	}
}

func TestValidateMachineSchemaClearsStringBounds(t *testing.T) {
	in := []*storepb.MachineParamSpec{{
		Key: machineparam.StorageClass, MinValue: "10Gi", MaxValue: "1Gi",
	}}
	out := ValidateMachineSchema(in)
	if len(out) != 1 || out[0].GetMinValue() != "" || out[0].GetMaxValue() != "" {
		t.Errorf("string bounds not cleared: %+v", out)
	}
}

func TestValidateMachineParams(t *testing.T) {
	schema := []*storepb.MachineParamSpec{
		spec(machineparam.CPU, "1", "250m", "8"),
		spec(machineparam.Memory, "2Gi", "512Mi", "32Gi"),
		spec(machineparam.Disk, "10Gi", "1Gi", "500Gi"),
		spec(machineparam.StorageClass, "", "", ""),
	}

	ok := []struct {
		name   string
		values map[string]string
		want   map[string]string
	}{
		{"nil values", nil, nil},
		{"empty values", map[string]string{}, nil},
		{"cpu override", map[string]string{"cpu": "  2  "}, map[string]string{"cpu": "2"}},
		{"all set", map[string]string{
			"cpu": "4", "memory": "8Gi", "disk": "20Gi", "storage_class": "fast-ssd",
		}, map[string]string{
			"cpu": "4", "memory": "8Gi", "disk": "20Gi", "storage_class": "fast-ssd",
		}},
		{"empty values skipped", map[string]string{"cpu": "", "memory": "  "}, nil},
		{"bounds edges accepted", map[string]string{"cpu": "250m", "memory": "512Mi", "disk": "500Gi"},
			map[string]string{"cpu": "250m", "memory": "512Mi", "disk": "500Gi"}},
	}
	for _, tt := range ok {
		got, err := ValidateMachineParams(schema, tt.values)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tt.name, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
			continue
		}
		for k, v := range tt.want {
			if got[k] != v {
				t.Errorf("%s: %q = %q, want %q", tt.name, k, got[k], v)
			}
		}
	}

	for _, tt := range []struct {
		name   string
		values map[string]string
	}{
		{"unknown key", map[string]string{"gpu": "1"}},
		{"undeclared key", map[string]string{"disk": "20Gi"}}, // schema without disk
		{"not a quantity", map[string]string{"cpu": "abc"}},
		{"not a name", map[string]string{"storage_class": "Fast SSD!"}},
		{"below minimum", map[string]string{"cpu": "100m"}},
		{"above maximum", map[string]string{"cpu": "16"}},
		{"above max memory", map[string]string{"memory": "64Gi"}},
	} {
		schema := schema
		if tt.name == "undeclared key" {
			// A schema that declares no disk: the key must be rejected even
			// though the catalog knows it.
			schema = []*storepb.MachineParamSpec{schema[0], schema[1], schema[3]}
		}
		if _, err := ValidateMachineParams(schema, tt.values); err == nil {
			t.Errorf("%s: expected error, got nil", tt.name)
		}
	}
}

func TestValidateMachineParamsEmptyMapNil(t *testing.T) {
	got, err := ValidateMachineParams([]*storepb.MachineParamSpec{}, map[string]string{})
	if got != nil || err != nil {
		t.Errorf("ValidateMachineParams(empty) = %v, %v; want nil, nil", got, err)
	}
}

func TestCatalogType(t *testing.T) {
	if typ, ok := CatalogType(machineparam.CPU); !ok ||
		typ != storepb.MachineParamType_MACHINE_PARAM_TYPE_QUANTITY {
		t.Errorf("CatalogType(cpu) = %v, %v; want QUANTITY, true", typ, ok)
	}
	if _, ok := CatalogType("gpu"); ok {
		t.Error("CatalogType(gpu) should be unknown")
	}
}

func TestValidateParamValueOptions(t *testing.T) {
	typ := storepb.MachineParamType_MACHINE_PARAM_TYPE_STRING
	if err := validateParamValue(typ, "ssd", []string{"ssd", "hdd"}); err != nil {
		t.Errorf("allowed option rejected: %v", err)
	}
	if err := validateParamValue(typ, "nvme", []string{"ssd", "hdd"}); err == nil {
		t.Error("disallowed option accepted")
	}
	if err := validateParamValue(typ, "ssd", nil); err != nil {
		t.Errorf("free-form string rejected: %v", err)
	}
}
