package api //nolint:revive

import (
	"fmt"
	"net/http"

	"github.com/akurosia/mediamtx/internal/conf"
	"github.com/akurosia/mediamtx/internal/conf/jsonwrapper"
	"github.com/gin-gonic/gin"
)

// Credentials are deliberately never returned. Empty fields in PATCH keep old credentials.
type authUserPatch struct {
	User        *conf.Credential                   `json:"user"`
	Pass        *conf.Credential                   `json:"pass"`
	StreamKey   *conf.Credential                   `json:"streamkey"`
	IPs         *conf.IPNetworks                   `json:"ips"`
	Permissions *[]conf.AuthInternalUserPermission `json:"permissions"`
	ForceStop   bool                               `json:"forceStop"`
}

func (a *API) onAuthUsersList(ctx *gin.Context) {
	users := a.Parent.APIConfigSnapshot().AuthInternalUsers
	out := make([]conf.AuthInternalUser, len(users))
	copy(out, users)
	for i := range out {
		if out[i].Pass != "" {
			out[i].Pass = "<redacted>"
		}
		if out[i].StreamKey != "" {
			out[i].StreamKey = "<redacted>"
		}
	}
	ctx.JSON(http.StatusOK, gin.H{"items": out, "itemCount": len(out)})
}

func (a *API) onAuthUsersAdd(ctx *gin.Context) {
	var user conf.AuthInternalUser
	if err := jsonwrapper.Decode(&customLimitReader{ctx.Request.Body, maxInboundConfigSize}, &user); err != nil {
		a.writeError(ctx, http.StatusBadRequest, err)
		return
	}
	if user.User == "" || user.User == "any" {
		a.writeError(ctx, http.StatusBadRequest, fmt.Errorf("a named user is required"))
		return
	}
	if err := a.Parent.APIAuthUserChange("add", string(user.User), &user); err != nil {
		a.writeError(ctx, http.StatusBadRequest, err)
		return
	}
	a.writeOK(ctx)
}

func (a *API) onAuthUsersPatch(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" || name == "any" {
		a.writeError(ctx, http.StatusBadRequest, fmt.Errorf("invalid name"))
		return
	}
	var patch authUserPatch
	if err := jsonwrapper.Decode(&customLimitReader{ctx.Request.Body, maxInboundConfigSize}, &patch); err != nil {
		a.writeError(ctx, http.StatusBadRequest, err)
		return
	}
	// Merge with the current user, allowing password/key rotation without resubmitting permissions.
	var current *conf.AuthInternalUser
	for _, u := range a.Parent.APIConfigSnapshot().AuthInternalUsers {
		if string(u.User) == name {
			tmp := u
			current = &tmp
			break
		}
	}
	if current == nil {
		a.writeError(ctx, http.StatusNotFound, fmt.Errorf("user not found"))
		return
	}
	if patch.User != nil {
		current.User = *patch.User
	}
	if patch.Pass != nil {
		current.Pass = *patch.Pass
	}
	if patch.StreamKey != nil {
		current.StreamKey = *patch.StreamKey
	}
	if patch.IPs != nil {
		current.IPs = *patch.IPs
	}
	if patch.Permissions != nil {
		current.Permissions = *patch.Permissions
	}
	if current.User == "" || current.User == "any" {
		a.writeError(ctx, http.StatusBadRequest, fmt.Errorf("invalid user"))
		return
	}
	var sessions []publisherSession
	if patch.ForceStop {
		if patch.StreamKey == nil && patch.Pass == nil {
			a.writeError(ctx, http.StatusBadRequest, fmt.Errorf("forceStop requires changing streamkey or password"))
			return
		}
		var err error
		sessions, err = a.publisherSessionsForUser(name)
		if err != nil {
			a.writeError(ctx, http.StatusServiceUnavailable, err)
			return
		}
	}
	if err := a.Parent.APIAuthUserChange("patch", name, current); err != nil {
		a.writeError(ctx, http.StatusBadRequest, err)
		return
	}
	if patch.ForceStop {
		disconnected, failed := disconnectPublisherSessions(sessions)
		ctx.JSON(http.StatusOK, gin.H{"status": "ok", "disconnected": disconnected, "disconnectFailed": failed})
		return
	}
	a.writeOK(ctx)
}

func (a *API) onAuthUsersDelete(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" || name == "any" {
		a.writeError(ctx, http.StatusBadRequest, fmt.Errorf("invalid name"))
		return
	}
	if err := a.Parent.APIAuthUserChange("delete", name, nil); err != nil {
		a.writeError(ctx, http.StatusBadRequest, err)
		return
	}
	a.writeOK(ctx)
}
