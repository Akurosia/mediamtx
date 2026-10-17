package mpegts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMPEGTSTimestampDelta(t *testing.T) {
	const wrap = int64(1) << 33
	require.Equal(t, int64(-3000), mpegtsTimestampDelta(87000, 90000))
	require.Equal(t, int64(3000), mpegtsTimestampDelta(93000, 90000))
	require.Equal(t, int64(-3000), mpegtsTimestampDelta(wrap-1000, 2000))
	require.Equal(t, int64(3000), mpegtsTimestampDelta(2000, wrap-1000))
}
