// Package omt contains the Open Media Transport protocol implementation.
package omt

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Protocol constants.
const (
	Version = 1

	// Timestamp units per second (10 million).
	TimestampFrequency = 10_000_000

	// Default port range.
	DefaultPort  = 6400
	MaxPort      = 6600
	URLScheme    = "omt"
	DNSSDService = "_omt._tcp"

	// Socket buffer sizes.
	SendBufferSize    = 1 << 20 // 1 MB
	ReceiveBufferSize = 8 << 20 // 8 MB

	// Maximum frame sizes.
	MaxVideoSize    = 10 << 20 // 10 MB
	MaxAudioSize    = 1 << 20  // 1 MB
	MaxMetadataSize = 64 << 10 // 64 KB

	// Header sizes.
	MainHeaderSize  = 16
	VideoHeaderSize = 32
	AudioHeaderSize = 24
)

// FrameType identifies the type of an OMT frame.
type FrameType uint8

// Frame types.
const (
	FrameTypeNone     FrameType = 0
	FrameTypeMetadata FrameType = 1
	FrameTypeVideo    FrameType = 2
	FrameTypeAudio    FrameType = 4
)

func (ft FrameType) String() string {
	switch ft {
	case FrameTypeNone:
		return "none"
	case FrameTypeMetadata:
		return "metadata"
	case FrameTypeVideo:
		return "video"
	case FrameTypeAudio:
		return "audio"
	default:
		return fmt.Sprintf("unknown(%d)", ft)
	}
}

// Codec is a FourCC codec identifier.
type Codec uint32

// Known codecs.
const (
	CodecVMX1 Codec = 0x31584D56
	CodecFPA1 Codec = 0x31415046
	CodecUYVY Codec = 0x59565955
	CodecYUY2 Codec = 0x32595559
	CodecBGRA Codec = 0x41524742
	CodecNV12 Codec = 0x3231564E
	CodecYV12 Codec = 0x32315659
	CodecUYVA Codec = 0x41565955
	CodecP216 Codec = 0x36313250
	CodecPA16 Codec = 0x36314150
	CodecH264 Codec = 0x34363248
	CodecH265 Codec = 0x35363248
)

func (c Codec) String() string {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(c))
	return string(b)
}

// VideoFlags contains video frame flags.
type VideoFlags uint32

// Video flag bits.
const (
	VideoFlagInterlaced    VideoFlags = 1
	VideoFlagAlpha         VideoFlags = 2
	VideoFlagPreMultiplied VideoFlags = 4
	VideoFlagPreview       VideoFlags = 8
	VideoFlagHighBitDepth  VideoFlags = 16
)

// ColorSpace identifies the color space.
type ColorSpace int32

// Color spaces.
const (
	ColorSpaceBT601 ColorSpace = 601
	ColorSpaceBT709 ColorSpace = 709
)

// Header is the 16-byte main frame header.
type Header struct {
	Version        uint8
	FrameType      FrameType
	Timestamp      int64
	MetadataLength uint16
	DataLength     int32
}

// Marshal serializes the header to bytes.
func (h *Header) Marshal(buf []byte) {
	buf[0] = h.Version
	buf[1] = uint8(h.FrameType)
	binary.LittleEndian.PutUint64(buf[2:10], uint64(h.Timestamp))
	binary.LittleEndian.PutUint16(buf[10:12], h.MetadataLength)
	binary.LittleEndian.PutUint32(buf[12:16], uint32(h.DataLength))
}

// Unmarshal deserializes the header from bytes.
func (h *Header) Unmarshal(buf []byte) error {
	if len(buf) < MainHeaderSize {
		return fmt.Errorf("header too short: %d bytes", len(buf))
	}
	h.Version = buf[0]
	if h.Version != Version {
		return fmt.Errorf("unsupported version: %d", h.Version)
	}
	h.FrameType = FrameType(buf[1])
	h.Timestamp = int64(binary.LittleEndian.Uint64(buf[2:10]))
	h.MetadataLength = binary.LittleEndian.Uint16(buf[10:12])
	h.DataLength = int32(binary.LittleEndian.Uint32(buf[12:16]))
	return nil
}

// VideoHeader is the 32-byte extended header for video frames.
type VideoHeader struct {
	Codec       Codec
	Width       int32
	Height      int32
	FrameRateN  int32
	FrameRateD  int32
	AspectRatio float32
	Flags       VideoFlags
	ColorSpace  ColorSpace
}

// Marshal serializes the video header to bytes.
func (vh *VideoHeader) Marshal(buf []byte) {
	binary.LittleEndian.PutUint32(buf[0:4], uint32(vh.Codec))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(vh.Width))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(vh.Height))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(vh.FrameRateN))
	binary.LittleEndian.PutUint32(buf[16:20], uint32(vh.FrameRateD))
	binary.LittleEndian.PutUint32(buf[20:24], math.Float32bits(vh.AspectRatio))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(vh.Flags))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(vh.ColorSpace))
}

// Unmarshal deserializes the video header from bytes.
func (vh *VideoHeader) Unmarshal(buf []byte) error {
	if len(buf) < VideoHeaderSize {
		return fmt.Errorf("video header too short: %d bytes", len(buf))
	}
	vh.Codec = Codec(binary.LittleEndian.Uint32(buf[0:4]))
	vh.Width = int32(binary.LittleEndian.Uint32(buf[4:8]))
	vh.Height = int32(binary.LittleEndian.Uint32(buf[8:12]))
	vh.FrameRateN = int32(binary.LittleEndian.Uint32(buf[12:16]))
	vh.FrameRateD = int32(binary.LittleEndian.Uint32(buf[16:20]))
	vh.AspectRatio = math.Float32frombits(binary.LittleEndian.Uint32(buf[20:24]))
	vh.Flags = VideoFlags(binary.LittleEndian.Uint32(buf[24:28]))
	vh.ColorSpace = ColorSpace(int32(binary.LittleEndian.Uint32(buf[28:32])))
	return nil
}

// AudioHeader is the 24-byte extended header for audio frames.
type AudioHeader struct {
	Codec             Codec
	SampleRate        int32
	SamplesPerChannel int32
	Channels          int32
	ActiveChannels    uint32
	Reserved1         int32
}

// Marshal serializes the audio header to bytes.
func (ah *AudioHeader) Marshal(buf []byte) {
	binary.LittleEndian.PutUint32(buf[0:4], uint32(ah.Codec))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(ah.SampleRate))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(ah.SamplesPerChannel))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(ah.Channels))
	binary.LittleEndian.PutUint32(buf[16:20], ah.ActiveChannels)
	binary.LittleEndian.PutUint32(buf[20:24], uint32(ah.Reserved1))
}

// Unmarshal deserializes the audio header from bytes.
func (ah *AudioHeader) Unmarshal(buf []byte) error {
	if len(buf) < AudioHeaderSize {
		return fmt.Errorf("audio header too short: %d bytes", len(buf))
	}
	ah.Codec = Codec(binary.LittleEndian.Uint32(buf[0:4]))
	ah.SampleRate = int32(binary.LittleEndian.Uint32(buf[4:8]))
	ah.SamplesPerChannel = int32(binary.LittleEndian.Uint32(buf[8:12]))
	ah.Channels = int32(binary.LittleEndian.Uint32(buf[12:16]))
	ah.ActiveChannels = binary.LittleEndian.Uint32(buf[16:20])
	ah.Reserved1 = int32(binary.LittleEndian.Uint32(buf[20:24]))
	return nil
}

// Frame is a complete OMT frame with parsed headers and raw payload.
type Frame struct {
	Header      Header
	VideoHeader *VideoHeader // non-nil for video frames
	AudioHeader *AudioHeader // non-nil for audio frames
	Data        []byte       // raw payload (video/audio data)
	Metadata    []byte       // XML metadata (may be nil)
}
