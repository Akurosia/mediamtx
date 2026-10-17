package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/akurosia/mediamtx/internal/conf"
	"github.com/stretchr/testify/require"
)

func TestAuthUserStore(t *testing.T) {
	dir := t.TempDir()
	p := &Core{confPath: filepath.Join(dir, "mediamtx.yml")}
	users := []conf.AuthInternalUser{{User: "admin", Pass: "original", StreamKey: "sk_first", Permissions: []conf.AuthInternalUserPermission{{Action: conf.AuthActionAPI}}}}
	require.NoError(t, p.saveAuthUsers(users))
	data, err := os.ReadFile(filepath.Join(dir, "users.json"))
	require.NoError(t, err)
	require.Contains(t, string(data), "sk_first")
	// Re-open and decode the persisted file to verify credentials round-trip.
	var restored []conf.AuthInternalUser
	require.NoError(t, json.Unmarshal(data, &restored))
	require.Equal(t, users, restored)
}
