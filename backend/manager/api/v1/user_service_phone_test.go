package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskPhoneNumber(t *testing.T) {
	tests := []struct {
		name  string
		phone string
		want  string
	}{
		{"empty", "", ""},
		{"short number fully masked", "1234", "****"},
		{"5 digits keep last 4", "12345", "*2345"},
		{"7 digits keep last 4", "1234567", "***4567"},
		{"8 digits keep first 3 and last 4", "12345678", "123*5678"},
		{"11 digits keep first 3 and last 4", "13812348000", "138****8000"},
		{"non-digit chars preserved", "+8613812348000", "+86*******8000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, maskPhoneNumber(tt.phone))
		})
	}
}
