package settingsstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/assistant-service/internal/application"
)

func TestFile_RoundTripPrivateAndClearable(t *testing.T) {
	f := NewFile(filepath.Join(t.TempDir(), "sub", "ai.json"))

	got, err := f.Load()
	require.NoError(t, err)
	assert.Nil(t, got, "nothing saved yet")

	in := application.AISettings{Provider: "anthropic", APIKey: "sk-ant-x", Model: "m", TimeoutSeconds: 30}
	require.NoError(t, f.Save(in))
	got, err = f.Load()
	require.NoError(t, err)
	assert.Equal(t, in, *got)

	info, err := os.Stat(f.path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the file holds a secret")

	require.NoError(t, f.Clear())
	require.NoError(t, f.Clear(), "clearing twice is fine")
	got, _ = f.Load()
	assert.Nil(t, got)
}
