package omt

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/google/uuid"

	"github.com/bluenviron/mediamtx/internal/auth"
	"github.com/bluenviron/mediamtx/internal/conf"
	"github.com/bluenviron/mediamtx/internal/defs"
	"github.com/bluenviron/mediamtx/internal/externalcmd"
	"github.com/bluenviron/mediamtx/internal/hooks"
	"github.com/bluenviron/mediamtx/internal/logger"
	"github.com/bluenviron/mediamtx/internal/protocols/omt"
	"github.com/bluenviron/mediamtx/internal/stream"
	"github.com/bluenviron/mediamtx/internal/unit"
)

// defaultPathName is used when the OMT connection doesn't specify a path.
// In OMT protocol, the path is typically derived from the DNS-SD service name
// or configured in mediamtx.yml.
const defaultPathName = "omt"

type conn struct {
	parentCtx           context.Context
	rtspAddress         string
	readTimeout         conf.Duration
	writeTimeout        conf.Duration
	netConn             net.Conn
	runOnConnect        string
	runOnConnectRestart bool
	runOnDisconnect     string
	externalCmdPool     *externalcmd.Pool
	pathManager         serverPathManager
	parent              *Server

	ctx       context.Context
	ctxCancel func()
	created   time.Time
	uuid      uuid.UUID
	mutex     sync.RWMutex
	pathName  string
}

func (c *conn) initialize() {
	c.ctx, c.ctxCancel = context.WithCancel(c.parentCtx)
	c.created = time.Now()
	c.uuid = uuid.New()

	c.Log(logger.Info, "opened")

	c.parent.wg.Add(1)
	go c.run()
}

func (c *conn) Close() {
	c.ctxCancel()
}

// Log implements logger.Writer.
func (c *conn) Log(level logger.Level, format string, args ...any) {
	c.parent.Log(level, "[conn %v] "+format, append([]any{c.netConn.RemoteAddr()}, args...)...)
}

func (c *conn) ip() net.IP {
	return c.netConn.RemoteAddr().(*net.TCPAddr).IP
}

func (c *conn) run() {
	defer c.parent.wg.Done()

	onDisconnectHook := hooks.OnConnect(hooks.OnConnectParams{
		Logger:              c,
		ExternalCmdPool:     c.externalCmdPool,
		RunOnConnect:        c.runOnConnect,
		RunOnConnectRestart: c.runOnConnectRestart,
		RunOnDisconnect:     c.runOnDisconnect,
		RTSPAddress:         c.rtspAddress,
		Desc:                *c.APIReaderDescribe(),
	})
	defer onDisconnectHook()

	err := c.runInner()

	c.ctxCancel()
	_ = c.netConn.Close()
	c.parent.closeConn(c)

	c.Log(logger.Info, "closed: %v", err)
}

func (c *conn) runInner() error {
	// In OMT, we determine mode by peeking at the first frame.
	// If it's a metadata frame with subscription commands, the remote is a reader.
	// If it's a video/audio frame, the remote is a publisher.
	reader := omt.NewReader(c.netConn)
	if err := reader.SetReadDeadline(time.Now().Add(time.Duration(c.readTimeout))); err != nil {
		return err
	}

	frame, err := reader.ReadFrame()
	if err != nil {
		return fmt.Errorf("read first frame: %w", err)
	}

	// Determine mode from first frame.
	if frame.Header.FrameType == omt.FrameTypeMetadata {
		// Remote sent metadata (subscription) → it's a reader requesting data from us.
		return c.runRead(reader, frame)
	}
	// Remote sent video/audio → it's a publisher sending data to us.
	return c.runPublish(reader, frame)
}

func (c *conn) runPublish(reader *omt.Reader, firstFrame *omt.Frame) error {
	c.mutex.Lock()
	c.pathName = defaultPathName
	c.mutex.Unlock()

	// Define OMT media formats.
	videoFormat := &format.Generic{
		PayloadTyp: 96,
		RTPMa:      "OMT-video/10000000",
		ClockRat:   omt.TimestampFrequency,
	}
	audioFormat := &format.Generic{
		PayloadTyp: 97,
		RTPMa:      "OMT-audio/48000",
		ClockRat:   48000,
	}

	medias := []*description.Media{
		{
			Type:    description.MediaTypeVideo,
			Formats: []format.Format{videoFormat},
		},
		{
			Type:    description.MediaTypeAudio,
			Formats: []format.Format{audioFormat},
		},
	}

	res, err := c.pathManager.AddPublisher(defs.PathAddPublisherReq{
		Author:        c,
		Desc:          &description.Session{Medias: medias},
		UseRTPPackets: false,
		ReplaceNTP:    true,
		AccessRequest: defs.PathAccessRequest{
			Name:        c.pathName,
			Publish:     true,
			Proto:       auth.ProtocolOMT,
			ID:          &c.uuid,
			Credentials: &auth.Credentials{},
			IP:          c.ip(),
		},
	})
	if err != nil {
		return err
	}

	defer res.Path.RemovePublisher(defs.PathRemovePublisherReq{Author: c})

	subStream := res.SubStream

	c.Log(logger.Info, "is publishing to path '%s'", c.pathName)

	// Process first frame.
	c.processPublishFrame(firstFrame, subStream, medias, videoFormat, audioFormat)

	// Read loop.
	for {
		select {
		case <-c.ctx.Done():
			return errors.New("terminated")
		default:
		}

		if deadlineErr := reader.SetReadDeadline(time.Now().Add(time.Duration(c.readTimeout))); deadlineErr != nil {
			return deadlineErr
		}
		frame, readErr := reader.ReadFrame()
		if readErr != nil {
			return readErr
		}

		c.processPublishFrame(frame, subStream, medias, videoFormat, audioFormat)
	}
}

func (c *conn) processPublishFrame(
	frame *omt.Frame,
	subStream *stream.SubStream,
	medias []*description.Media,
	videoFormat *format.Generic,
	audioFormat *format.Generic,
) {
	switch frame.Header.FrameType {
	case omt.FrameTypeVideo:
		if frame.VideoHeader == nil {
			return
		}
		subStream.WriteUnit(medias[0], videoFormat, &unit.Unit{
			PTS: frame.Header.Timestamp,
			Payload: &unit.PayloadOMTVideo{
				VideoHeader: *frame.VideoHeader,
				Data:        frame.Data,
				Metadata:    frame.Metadata,
			},
		})

	case omt.FrameTypeAudio:
		if frame.AudioHeader == nil {
			return
		}
		subStream.WriteUnit(medias[1], audioFormat, &unit.Unit{
			PTS: frame.Header.Timestamp,
			Payload: &unit.PayloadOMTAudio{
				AudioHeader: *frame.AudioHeader,
				Data:        frame.Data,
				Metadata:    frame.Metadata,
			},
		})
	}
}

func (c *conn) runRead(reader *omt.Reader, firstMetadataFrame *omt.Frame) error {
	c.mutex.Lock()
	c.pathName = defaultPathName
	c.mutex.Unlock()

	// The remote wants to read from us. Register as a reader.
	res, err := c.pathManager.AddReader(defs.PathAddReaderReq{
		Author: c,
		AccessRequest: defs.PathAccessRequest{
			Name:        c.pathName,
			Proto:       auth.ProtocolOMT,
			ID:          &c.uuid,
			Credentials: &auth.Credentials{},
			IP:          c.ip(),
		},
	})
	if err != nil {
		return err
	}

	defer res.Path.RemoveReader(defs.PathRemoveReaderReq{Author: c})

	writer := omt.NewWriter(c.netConn)

	// Send our info.
	if writeErr := writer.WriteMetadataCommand(omt.XMLInfoTemplate); writeErr != nil {
		return writeErr
	}

	// Set up stream reader.
	r := &stream.Reader{Parent: c}

	desc := res.Stream.OutDescCopy()

	for _, medi := range desc.Medias {
		for _, forma := range medi.Formats {
			if _, isGeneric := forma.(*format.Generic); isGeneric {
				rtpMap := forma.RTPMap()
				switch {
				case len(rtpMap) >= 9 && rtpMap[:9] == "OMT-video":
					r.OnData(medi, forma, func(u *unit.Unit) error {
						videoPayload, isVideoPayload := u.Payload.(*unit.PayloadOMTVideo)
						if !isVideoPayload {
							return nil
						}
						f := &omt.Frame{
							Header: omt.Header{
								FrameType: omt.FrameTypeVideo,
								Timestamp: u.PTS,
							},
							VideoHeader: &videoPayload.VideoHeader,
							Data:        videoPayload.Data,
							Metadata:    videoPayload.Metadata,
						}
						if deadlineErr := writer.SetWriteDeadline(time.Now().Add(time.Duration(c.writeTimeout))); deadlineErr != nil {
							return deadlineErr
						}
						return writer.WriteFrame(f)
					})

				case len(rtpMap) >= 9 && rtpMap[:9] == "OMT-audio":
					r.OnData(medi, forma, func(u *unit.Unit) error {
						audioPayload, isAudioPayload := u.Payload.(*unit.PayloadOMTAudio)
						if !isAudioPayload {
							return nil
						}
						f := &omt.Frame{
							Header: omt.Header{
								FrameType: omt.FrameTypeAudio,
								Timestamp: u.PTS,
							},
							AudioHeader: &audioPayload.AudioHeader,
							Data:        audioPayload.Data,
							Metadata:    audioPayload.Metadata,
						}
						if deadlineErr := writer.SetWriteDeadline(time.Now().Add(time.Duration(c.writeTimeout))); deadlineErr != nil {
							return deadlineErr
						}
						return writer.WriteFrame(f)
					})
				}
			}
		}
	}

	c.Log(logger.Info, "is reading from path '%s', %s",
		res.Path.Name(), defs.FormatsInfo(r.Formats()))

	onUnreadHook := hooks.OnRead(hooks.OnReadParams{
		Logger:          c,
		ExternalCmdPool: c.externalCmdPool,
		Conf:            res.Path.SafeConf(),
		ExternalCmdEnv:  res.Path.ExternalCmdEnv(),
		Reader:          *c.APIReaderDescribe(),
		Query:           "",
	})
	defer onUnreadHook()

	// Disable read deadline for the reading side — we only write.
	if deadlineErr := c.netConn.SetReadDeadline(time.Time{}); deadlineErr != nil {
		return deadlineErr
	}

	res.Stream.AddReader(r)
	defer res.Stream.RemoveReader(r)

	select {
	case <-c.ctx.Done():
		return errors.New("terminated")
	case err = <-r.Error():
		return err
	}
}

// APIReaderDescribe implements reader.
func (c *conn) APIReaderDescribe() *defs.APIPathReader {
	return &defs.APIPathReader{
		Type: defs.APIPathReaderTypeOMTConn,
		ID:   c.uuid.String(),
	}
}

// APISourceDescribe implements source.
func (c *conn) APISourceDescribe() *defs.APIPathSource {
	return &defs.APIPathSource{
		Type: defs.APIPathSourceTypeOMTConn,
		ID:   c.uuid.String(),
	}
}
