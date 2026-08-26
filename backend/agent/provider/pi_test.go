package provider

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPiProviderDetectAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	p := &PiProvider{}
	_, present, err := p.Detect(context.Background())
	require.NoError(t, err)
	assert.False(t, present)
}

func TestPiProviderDetectCompatible(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "pi", `echo "0.82.1"`)
	t.Setenv("PATH", dir)
	p := &PiProvider{}
	info, present, err := p.Detect(context.Background())
	require.NoError(t, err)
	require.True(t, present)
	require.NotNil(t, info)
	assert.True(t, info.Compatible)
	assert.Empty(t, info.IncompatibilityReason)
	assert.Equal(t, filepath.Join(dir, "pi"), info.ExecutablePath)
}

func TestPiProviderDetectIncompatible(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "pi", `echo "0.80.0"`)
	t.Setenv("PATH", dir)
	p := &PiProvider{}
	info, present, err := p.Detect(context.Background())
	require.NoError(t, err)
	require.True(t, present)
	require.NotNil(t, info)
	assert.False(t, info.Compatible)
	assert.Contains(t, info.IncompatibilityReason, "0.82.1")
}

func TestPiProviderProbeModels(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "pi", `printf "anthropic/claude-sonnet-4-5\nopenai/gpt-5\n"`)
	t.Setenv("PATH", dir)
	p := &PiProvider{}
	models, supports, err := p.ProbeModels(context.Background(), "")
	require.NoError(t, err)
	assert.False(t, supports)
	require.Len(t, models, 2)
	assert.Equal(t, "anthropic/claude-sonnet-4-5", models[0].Value)
	assert.Equal(t, "openai/gpt-5", models[1].Value)
}

func TestPiVersionAtLeast(t *testing.T) {
	assert.True(t, piVersionAtLeast("0.82.1", "0.82.1"))
	assert.True(t, piVersionAtLeast("0.83.0", "0.82.1"))
	assert.False(t, piVersionAtLeast("0.82.0", "0.82.1"))
	assert.False(t, piVersionAtLeast("garbage", "0.82.1"))
}

func TestPiProviderProbeModelsTable(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "pi", `printf "provider model context max-out thinking images\ndeepseek deepseek-v4-flash 1M 384K yes no\ndeepseek deepseek-v4-pro 1M 384K yes no\n"`)
	t.Setenv("PATH", dir)
	p := &PiProvider{}
	models, _, err := p.ProbeModels(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[0].Value)
	assert.Equal(t, "deepseek-v4-flash", models[0].Name)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[1].Value)
	assert.Equal(t, "deepseek-v4-pro", models[1].Name)
}
