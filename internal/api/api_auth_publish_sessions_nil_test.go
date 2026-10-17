package api //nolint:revive

import (
    "testing"

    "github.com/akurosia/mediamtx/internal/servers/rtmp"
    "github.com/akurosia/mediamtx/internal/servers/rtsp"
    "github.com/akurosia/mediamtx/internal/servers/srt"
    "github.com/akurosia/mediamtx/internal/servers/webrtc"
    "github.com/stretchr/testify/require"
)

func TestPublisherSessionsForUserDisabledServers(t *testing.T) {
    // Typed nil pointers inside interfaces must not be queried.
    var rtmpServer *rtmp.Server
    var rtspServer *rtsp.Server
    var srtServer *srt.Server
    var webrtcServer *webrtc.Server
    a := &API{
        RTMPServer: rtmpServer,
        RTMPSServer: rtmpServer,
        RTSPServer: rtspServer,
        RTSPSServer: rtspServer,
        SRTServer: srtServer,
        WebRTCServer: webrtcServer,
    }
    sessions, err := a.publisherSessionsForUser("testuser")
    require.NoError(t, err)
    require.Empty(t, sessions)
}
