package api //nolint:revive

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/akurosia/mediamtx/internal/conf"
	"github.com/gin-gonic/gin"
)

// This route is deliberately not guarded by the administrator-only API permission.
// It uses the named streamer's Basic username/password, with optional IP restrictions.
func (a *API) onAuthSelfRotateKey(ctx *gin.Context) {
	if ctx.GetHeader("Content-Type") != "application/json" {
		a.writeErrorNoLog(ctx, http.StatusUnsupportedMediaType, fmt.Errorf("application/json required"))
		return
	}
	if origin := ctx.GetHeader("Origin"); origin != "" {
		// Browsers may not send cross-origin authenticated requests to this endpoint.
		host := ctx.Request.Host
		if origin != "http://"+host && origin != "https://"+host {
			a.writeErrorNoLog(ctx, http.StatusForbidden, fmt.Errorf("invalid origin"))
			return
		}
	}
	name, password, ok := ctx.Request.BasicAuth()
	if !ok || name == "" || name == "any" || password == "" {
		a.writeErrorNoLog(ctx, http.StatusUnauthorized, fmt.Errorf("authentication required"))
		return
	}
	snapshot := a.Parent.APIConfigSnapshot()
	if snapshot.AuthMethod != conf.AuthMethodInternal {
		a.writeErrorNoLog(ctx, http.StatusBadRequest, fmt.Errorf("internal authentication required"))
		return
	}
	matched := false
	for _, user := range snapshot.AuthInternalUsers {
		if string(user.User) != name || user.Pass == "" || !user.Pass.Check(password) {
			continue
		}
		ip := net.ParseIP(ctx.ClientIP())
		if len(user.IPs) > 0 && !user.IPs.Contains(ip) {
			continue
		}
		matched = true
		break
	}
	if !matched {
		ctx.Header("WWW-Authenticate", `Basic realm="mediamtx"`)
		a.writeErrorNoLog(ctx, http.StatusUnauthorized, fmt.Errorf("invalid credentials"))
		return
	}
	var options struct {
		ForceStop bool `json:"forceStop"`
	}
	if err := json.NewDecoder(io.LimitReader(ctx.Request.Body, 4096)).Decode(&options); err != nil && err != io.EOF {
		a.writeErrorNoLog(ctx, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	var sessions []publisherSession
	if options.ForceStop {
		var err error
		sessions, err = a.publisherSessionsForUser(name)
		if err != nil {
			a.writeErrorNoLog(ctx, http.StatusServiceUnavailable, fmt.Errorf("could not enumerate publisher sessions: %w", err))
			return
		}
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		a.writeErrorNoLog(ctx, http.StatusInternalServerError, err)
		return
	}
	key := "sk_" + strings.TrimRight(base64.RawURLEncoding.EncodeToString(random), "=")
	if err := a.Parent.APIAuthUserRotateKey(name, password, key); err != nil {
		a.writeErrorNoLog(ctx, http.StatusBadRequest, err)
		return
	}
	ctx.Header("Cache-Control", "no-store")
	disconnected, failed := disconnectPublisherSessions(sessions)
	ctx.JSON(http.StatusOK, gin.H{"streamkey": key, "disconnected": disconnected, "disconnectFailed": failed})
}
