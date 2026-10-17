package api //nolint:revive

import (
	"fmt"
	"strings"

	"github.com/akurosia/mediamtx/internal/defs"
)

// publisherSession is a snapshot of an already authenticated publishing session.
// Capture IDs BEFORE rotating the key, so clients reconnecting with the NEW key
// cannot accidentally be disconnected by the same rotation request.
type publisherSession struct {
	kick func() error
}

func (a *API) publisherSessionsForUser(name string) ([]publisherSession, error) {
	var sessions []publisherSession
	if strings.TrimSpace(name) == "" || name == "any" {
		return nil, fmt.Errorf("invalid username")
	}
	if !interfaceIsEmpty(a.RTMPServer) {
		list, err := a.RTMPServer.APIConnsList()
		if err != nil {
			return nil, err
		}
		for _, entry := range list.Items {
			if entry.User == name && entry.State == defs.APIRTMPConnStatePublish {
				id := entry.ID
				server := a.RTMPServer
				sessions = append(sessions, publisherSession{kick: func() error { return server.APIConnsKick(id) }})
			}
		}
	}
	if !interfaceIsEmpty(a.RTMPSServer) {
		list, err := a.RTMPSServer.APIConnsList()
		if err != nil {
			return nil, err
		}
		for _, entry := range list.Items {
			if entry.User == name && entry.State == defs.APIRTMPConnStatePublish {
				id := entry.ID
				server := a.RTMPSServer
				sessions = append(sessions, publisherSession{kick: func() error { return server.APIConnsKick(id) }})
			}
		}
	}
	if !interfaceIsEmpty(a.RTSPServer) {
		list, err := a.RTSPServer.APISessionsList()
		if err != nil {
			return nil, err
		}
		for _, entry := range list.Items {
			if entry.User == name && entry.State == defs.APIRTSPSessionStatePublish {
				id := entry.ID
				server := a.RTSPServer
				sessions = append(sessions, publisherSession{kick: func() error { return server.APISessionsKick(id) }})
			}
		}
	}
	if !interfaceIsEmpty(a.RTSPSServer) {
		list, err := a.RTSPSServer.APISessionsList()
		if err != nil {
			return nil, err
		}
		for _, entry := range list.Items {
			if entry.User == name && entry.State == defs.APIRTSPSessionStatePublish {
				id := entry.ID
				server := a.RTSPSServer
				sessions = append(sessions, publisherSession{kick: func() error { return server.APISessionsKick(id) }})
			}
		}
	}
	if !interfaceIsEmpty(a.SRTServer) {
		list, err := a.SRTServer.APIConnsList()
		if err != nil {
			return nil, err
		}
		for _, entry := range list.Items {
			if entry.User == name && entry.State == defs.APISRTConnStatePublish {
				id := entry.ID
				server := a.SRTServer
				sessions = append(sessions, publisherSession{kick: func() error { return server.APIConnsKick(id) }})
			}
		}
	}
	if !interfaceIsEmpty(a.WebRTCServer) {
		list, err := a.WebRTCServer.APISessionsList()
		if err != nil {
			return nil, err
		}
		for _, entry := range list.Items {
			if entry.User == name && entry.State == defs.APIWebRTCSessionStatePublish {
				id := entry.ID
				server := a.WebRTCServer
				sessions = append(sessions, publisherSession{kick: func() error { return server.APISessionsKick(id) }})
			}
		}
	}
	return sessions, nil
}

func disconnectPublisherSessions(sessions []publisherSession) (int, int) {
	disconnected, failed := 0, 0
	for _, session := range sessions {
		if err := session.kick(); err != nil {
			failed++
		} else {
			disconnected++
		}
	}
	return disconnected, failed
}
