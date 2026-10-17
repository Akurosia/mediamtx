package api //nolint:revive

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/akurosia/mediamtx/internal/conf"
	"github.com/akurosia/mediamtx/internal/test"
	"github.com/stretchr/testify/require"
)

func TestAuthUsersCRUD(t *testing.T) {
	cnf := tempConf(t, "api: yes\n")
	parent := &testParent{conf: cnf}
	api := API{
		Address:      "localhost:9997",
		ReadTimeout:  conf.Duration(10 * time.Second),
		WriteTimeout: conf.Duration(10 * time.Second),
		AuthManager:  test.NilAuthManager,
		Parent:       parent,
	}
	require.NoError(t, api.Initialize())
	defer api.Close()
	client := &http.Client{}
	base := "http://localhost:9997/v3/config/auth-users/"
	httpRequest(t, client, http.MethodPost, base+"add", map[string]any{
		"user": "alice", "pass": "original", "streamkey": "firstkey",
		"permissions": []map[string]any{{"action": "publish", "path": "live/alice"}},
	}, nil)
	require.Equal(t, conf.Credential("firstkey"), parent.conf.AuthInternalUsers[len(parent.conf.AuthInternalUsers)-1].StreamKey)
	var out struct {
		Items     []conf.AuthInternalUser `json:"items"`
		ItemCount int                     `json:"itemCount"`
	}
	httpRequest(t, client, http.MethodGet, base+"list", nil, &out)
	require.Equal(t, conf.Credential("<redacted>"), out.Items[len(out.Items)-1].StreamKey)
	require.Equal(t, conf.Credential("<redacted>"), out.Items[len(out.Items)-1].Pass)
	httpRequest(t, client, http.MethodPatch, base+"patch/alice", map[string]any{"streamkey": "secondkey"}, nil)
	got := parent.conf.AuthInternalUsers[len(parent.conf.AuthInternalUsers)-1]
	require.Equal(t, conf.Credential("original"), got.Pass)
	require.Equal(t, conf.Credential("secondkey"), got.StreamKey)
	require.Len(t, got.Permissions, 1)
	httpRequest(t, client, http.MethodDelete, base+"delete/alice", nil, nil)
	for _, user := range parent.conf.AuthInternalUsers {
		require.NotEqual(t, conf.Credential("alice"), user.User)
	}
}

func TestAuthUserSelfRotate(t *testing.T) {
	cnf := tempConf(t, "api: yes\n")
	cnf.AuthInternalUsers = append(cnf.AuthInternalUsers, conf.AuthInternalUser{
		User: "streamer", Pass: "mypass", StreamKey: "before",
		Permissions: []conf.AuthInternalUserPermission{{Action: conf.AuthActionPublish, Path: "live/streamer"}},
	})
	parent := &testParent{conf: cnf}
	server := API{
		Address: "localhost:9997", ReadTimeout: conf.Duration(10 * time.Second), WriteTimeout: conf.Duration(10 * time.Second),
		AuthManager: test.NilAuthManager, Parent: parent,
	}
	require.NoError(t, server.Initialize())
	defer server.Close()
	url := "http://localhost:9997/v3/auth/self/streamkey/rotate"
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	require.NoError(t, err)
	req.SetBasicAuth("streamer", "mypass")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result struct {
		StreamKey string `json:"streamkey"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NotEmpty(t, result.StreamKey)
	require.NotEqual(t, "before", result.StreamKey)
	require.Equal(t, conf.Credential(result.StreamKey), parent.conf.AuthInternalUsers[len(parent.conf.AuthInternalUsers)-1].StreamKey)
	// A wrong password cannot rotate the key.
	bad, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	require.NoError(t, err)
	bad.SetBasicAuth("streamer", "wrong")
	bad.Header.Set("Content-Type", "application/json")
	denied, err := http.DefaultClient.Do(bad)
	require.NoError(t, err)
	defer denied.Body.Close()
	require.Equal(t, http.StatusUnauthorized, denied.StatusCode)
}
