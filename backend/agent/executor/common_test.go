package executor

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyInputTooLarge(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"input too large", errors.New("prompt rejected: input too large for context window"), true},
		{"too many tokens", errors.New("too many tokens: 210000 exceeds the 200000 limit"), true},
		{"context length exceeded", errors.New("Request is too long: context length exceeded"), true},
		{"context length not exceeded", errors.New("context length is fine"), false},
		{"maximum context", errors.New("maximum context length of 200000 tokens reached"), true},
		{"token limit", errors.New("token limit exceeded"), true},
		{"unrelated", errors.New("permission denied"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ClassifyInputTooLarge(tc.err))
		})
	}
}
