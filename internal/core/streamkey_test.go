package core

import (
	"testing"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/stretchr/testify/require"

	"github.com/akurosia/mediamtx/internal/test"
)

func TestStreamKeyPublish(t *testing.T) {
	p, ok := newInstance(t, "rtmp: false\nhls: false\nwebrtc: false\nmoq: false\n"+
		"srt: false\nsrtla: false\nomt: false\nrtspTransports: [tcp]\n"+
		"authInternalUsers:\n- user: publisher\n  pass: password\n  streamkey: secret\n"+
		"  permissions:\n  - action: publish\n    path: live\npaths:\n  live:\n")
	require.True(t, ok)
	defer p.Close()

	for _, key := range []string{"secret", "wrong", ""} {
		t.Run(key, func(t *testing.T) {
			client := &gortsplib.Client{}
			err := client.StartRecording("rtsp://127.0.0.1:8554/live?key="+key,
				&description.Session{Medias: []*description.Media{{
					Type: description.MediaTypeVideo, Formats: []format.Format{test.FormatH264},
				}}})
			if key == "secret" {
				require.NoError(t, err)
				client.Close()
			} else {
				require.Error(t, err)
			}
		})
	}
}
