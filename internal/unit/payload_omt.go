package unit

import "github.com/bluenviron/mediamtx/internal/protocols/omt"

// PayloadOMTVideo is an OMT video frame payload.
type PayloadOMTVideo struct {
	VideoHeader omt.VideoHeader
	Data        []byte
	Metadata    []byte
}

func (PayloadOMTVideo) isPayload() {}

// PayloadOMTAudio is an OMT audio frame payload.
type PayloadOMTAudio struct {
	AudioHeader omt.AudioHeader
	Data        []byte
	Metadata    []byte
}

func (PayloadOMTAudio) isPayload() {}
