package srt

import (
	"fmt"
	"time"

	srt "github.com/datarhei/gosrt"

	"github.com/akurosia/mediamtx/internal/logger"
)

const timingDiagnosticsInterval = 2 * time.Second

// All synchronized publishers are released on the same UTC timeline with
// enough headroom for typical mobile SRT latency and jitter.
const moblinSyncPlayoutLatency = 5 * time.Second

const mpegtsTimestampMask = int64(1<<33 - 1)

func mpegtsTimestampDelta(a int64, b int64) int64 {
	delta := (a - b) & mpegtsTimestampMask
	if delta > 1<<32 {
		delta -= 1 << 33
	}
	return delta
}

func moblinTimecodeNearNow(value string, now time.Time, frameStep int64) (time.Time, bool) {
	var hour, minute, second, frame int
	if _, err := fmt.Sscanf(value, "%02d:%02d:%02d+frame:%d", &hour, &minute, &second, &frame); err != nil {
		return time.Time{}, false
	}

	now = now.UTC()
	out := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, second, 0, time.UTC)
	if out.Sub(now) > 12*time.Hour {
		out = out.Add(-24 * time.Hour)
	} else if now.Sub(out) > 12*time.Hour {
		out = out.Add(24 * time.Hour)
	}
	if frameStep > 0 {
		out = out.Add(time.Duration(int64(frame) * frameStep * int64(time.Second) / 90000))
	}
	return out, true
}

// SynchronizeMPEGTSTiming implements mpegts.TimingSynchronizer. The
// authenticated user is the synchronization group; authentication already
// limits membership to paths matching that user's publish permissions.
func (c *conn) SynchronizeMPEGTSTiming(kind string, pts int64, au [][]byte) (int64, time.Time, bool) {
	c.mutex.RLock()
	user := c.user
	path := c.pathName
	c.mutex.RUnlock()
	if user == "" {
		return 0, time.Time{}, false
	}

	c.syncMu.Lock()
	if kind == "H265" && !c.syncReady {
		if c.syncLastVideoPTS != 0 {
			step := mpegtsTimestampDelta(pts, c.syncLastVideoPTS)
			if step > 0 && step < 9000 {
				if c.syncFrameStep == 0 || step < c.syncFrameStep {
					c.syncFrameStep = step
				}
				c.syncFrameSamples++
			}
		}
		c.syncLastVideoPTS = pts

		// Observe several frames before selecting an anchor. This determines
		// the real frame interval even when encoded frames arrive in DTS order.
		if c.syncFrameSamples < 8 {
			c.syncMu.Unlock()
			return 0, time.Time{}, false
		}

		for _, nal := range au {
			value, ok := decodeMoblinHEVCTimecode(nal)
			if !ok {
				continue
			}
			utc, ok := moblinTimecodeNearNow(value, time.Now(), c.syncFrameStep)
			if !ok {
				continue
			}
			c.syncAnchorPTS = pts
			c.syncAnchorUTC = utc
			c.syncReady = true
			if !c.syncLogged {
				c.syncLogged = true
				c.Log(logger.Info, "Moblin synchronization enabled: group=%q path=%q playout_latency=%s",
					user, path, moblinSyncPlayoutLatency)
			}
			break
		}
	}

	if !c.syncReady {
		c.syncMu.Unlock()
		return 0, time.Time{}, false
	}

	ntp := c.syncAnchorUTC.Add(time.Duration(mpegtsTimestampDelta(pts, c.syncAnchorPTS)) * time.Second / 90000)
	// Keep the standard 33-bit MPEG-TS clock range while making its phase
	// depend only on UTC, not on the individual connection start time.
	synchronizedPTS := (ntp.Unix()*90000 + int64(ntp.Nanosecond())*90000/int64(time.Second)) & mpegtsTimestampMask
	c.syncMu.Unlock()

	deadline := ntp.Add(moblinSyncPlayoutLatency)
	if delay := time.Until(deadline); delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-c.ctx.Done():
			timer.Stop()
		}
	}

	return synchronizedPTS, ntp, true
}

func (c *conn) runTimingDiagnostics(sconn srt.Conn, path string, done <-chan struct{}) {
	ticker := time.NewTicker(timingDiagnosticsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			var stats srt.Statistics
			sconn.Stats(&stats)
			c.Log(logger.Debug,
				"timing: path=%q srt_elapsed_ms=%d rtt_ms=%.3f recv_tsbpd_delay_ms=%d send_tsbpd_delay_ms=%d recv_buffer_ms=%d recv_buffer_packets=%d retrans=%d loss=%d drop=%d belated=%d recv_rate_mbps=%.3f",
				path, stats.MsTimeStamp, stats.Instantaneous.MsRTT,
				stats.Instantaneous.MsRecvTsbPdDelay, stats.Instantaneous.MsSendTsbPdDelay,
				stats.Instantaneous.MsRecvBuf, stats.Instantaneous.PktRecvBuf,
				stats.Accumulated.PktRecvRetrans, stats.Accumulated.PktRecvLoss,
				stats.Accumulated.PktRecvDrop, stats.Accumulated.PktRecvBelated,
				stats.Instantaneous.MbpsRecvRate)
		}
	}
}

// LogMPEGTSTiming implements mpegts.TimingDiagnostics.
func (c *conn) LogMPEGTSTiming(track int, pid uint16, kind string, pts int64, dts int64, au [][]byte) {
	now := time.Now()
	c.diagMu.Lock()
	last := c.diagLast[pid]
	if now.Sub(last) < time.Second {
		c.diagMu.Unlock()
		return
	}
	c.diagLast[pid] = now
	c.diagMu.Unlock()

	c.mutex.RLock()
	path := c.pathName
	c.mutex.RUnlock()

	timecode := "none"
	if kind == "H265" {
		for _, nal := range au {
			if tc, ok := decodeMoblinHEVCTimecode(nal); ok {
				timecode = tc
				break
			}
		}
	}

	c.Log(logger.Debug,
		"timing: path=%q mpegts_track=%d pid=%d codec=%s pts90k=%d dts90k=%d pts_seconds=%.6f dts_seconds=%.6f moblin_utc_timecode=%s",
		path, track, pid, kind, pts, dts, float64(pts)/90000, float64(dts)/90000, timecode)
}

type bitReader struct {
	buf []byte
	pos int
}

func (r *bitReader) read(n int) (uint32, bool) {
	if n < 0 || r.pos+n > len(r.buf)*8 {
		return 0, false
	}
	var out uint32
	for range n {
		out = (out << 1) | uint32((r.buf[r.pos/8]>>(7-(r.pos%8)))&1)
		r.pos++
	}
	return out, true
}

func removeEmulationPrevention(buf []byte) []byte {
	out := make([]byte, 0, len(buf))
	zeros := 0
	for _, b := range buf {
		if zeros >= 2 && b == 3 {
			zeros = 0
			continue
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

func decodeMoblinHEVCTimecode(nal []byte) (string, bool) {
	if len(nal) < 4 || ((nal[0]>>1)&0x3f) != 39 { // prefix_sei_nut
		return "", false
	}
	rbsp := removeEmulationPrevention(nal[2:])
	for len(rbsp) >= 2 {
		payloadType := 0
		for len(rbsp) > 0 && rbsp[0] == 0xff {
			payloadType += 255
			rbsp = rbsp[1:]
		}
		if len(rbsp) < 2 {
			return "", false
		}
		payloadType += int(rbsp[0])
		rbsp = rbsp[1:]
		payloadSize := 0
		for len(rbsp) > 0 && rbsp[0] == 0xff {
			payloadSize += 255
			rbsp = rbsp[1:]
		}
		if len(rbsp) == 0 {
			return "", false
		}
		payloadSize += int(rbsp[0])
		rbsp = rbsp[1:]
		if payloadSize > len(rbsp) {
			return "", false
		}
		payload := rbsp[:payloadSize]
		rbsp = rbsp[payloadSize:]
		if payloadType != 136 {
			continue
		}

		br := bitReader{buf: payload}
		numClockTS, ok := br.read(2)
		if !ok || numClockTS != 1 {
			return "", false
		}
		clockFlag, ok := br.read(1)
		if !ok || clockFlag != 1 {
			return "", false
		}
		if _, ok = br.read(6); !ok { // unit_field_based_flag + counting_type
			return "", false
		}
		full, ok := br.read(1)
		if !ok || full != 1 {
			return "", false
		}
		if _, ok = br.read(2); !ok { // discontinuity + cnt_dropped
			return "", false
		}
		frame, ok := br.read(9)
		if !ok {
			return "", false
		}
		second, ok1 := br.read(6)
		minute, ok2 := br.read(6)
		hour, ok3 := br.read(5)
		if !ok1 || !ok2 || !ok3 || second > 59 || minute > 59 || hour > 23 {
			return "", false
		}
		return fmt.Sprintf("%02d:%02d:%02d+frame:%d", hour, minute, second, frame), true
	}
	return "", false
}
