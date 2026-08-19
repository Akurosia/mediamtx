package srt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func appendBits(dst []bool, value uint32, count int) []bool {
	for i := count - 1; i >= 0; i-- {
		dst = append(dst, ((value>>i)&1) != 0)
	}
	return dst
}

func bitsToBytes(bits []bool) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, bit := range bits {
		if bit {
			out[i/8] |= 1 << (7 - (i % 8))
		}
	}
	return out
}

func TestDecodeMoblinHEVCTimecode(t *testing.T) {
	var bits []bool
	bits = appendBits(bits, 1, 2)  // num_clock_ts
	bits = appendBits(bits, 1, 1)  // clock_timestamp_flag
	bits = appendBits(bits, 1, 1)  // unit_field_based_flag
	bits = appendBits(bits, 0, 5)  // counting_type
	bits = appendBits(bits, 1, 1)  // full_timestamp_flag
	bits = appendBits(bits, 0, 1)  // discontinuity_flag
	bits = appendBits(bits, 0, 1)  // cnt_dropped_flag
	bits = appendBits(bits, 42, 9) // frame
	bits = appendBits(bits, 56, 6) // seconds
	bits = appendBits(bits, 34, 6) // minutes
	bits = appendBits(bits, 12, 5) // hours
	payload := bitsToBytes(bits)

	// HEVC prefix_sei_nut header, payload type 136, payload size, payload.
	nal := append([]byte{39 << 1, 1, 136, byte(len(payload))}, payload...)
	timecode, ok := decodeMoblinHEVCTimecode(nal)
	require.True(t, ok)
	require.Equal(t, "12:34:56+frame:42", timecode)
}

func TestDecodeMoblinHEVCTimecodeOtherNAL(t *testing.T) {
	_, ok := decodeMoblinHEVCTimecode([]byte{1 << 1, 1, 0, 0})
	require.False(t, ok)
}
