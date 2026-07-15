// Package omt contains the OMT static source.
package omt

import (
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"

	"github.com/bluenviron/mediamtx/internal/conf"
	"github.com/bluenviron/mediamtx/internal/defs"
	"github.com/bluenviron/mediamtx/internal/logger"
	"github.com/bluenviron/mediamtx/internal/protocols/omt"
	"github.com/bluenviron/mediamtx/internal/stream"
	"github.com/bluenviron/mediamtx/internal/unit"
)

type parent interface {
	logger.Writer
	SetReady(req defs.PathSourceStaticSetReadyReq) defs.PathSourceStaticSetReadyRes
	SetNotReady(req defs.PathSourceStaticSetNotReadyReq)
}

// Source is an OMT static source.
type Source struct {
	ReadTimeout conf.Duration
	Parent      parent
}

// Log implements logger.Writer.
func (s *Source) Log(level logger.Level, format string, args ...any) {
	s.Parent.Log(level, "[OMT source] "+format, args...)
}

// Run implements StaticSource.
func (s *Source) Run(params defs.StaticSourceRunParams) error {
	s.Log(logger.Debug, "connecting")

	u, err := url.Parse(params.ResolvedSource)
	if err != nil {
		return fmt.Errorf("invalid OMT URL: %w", err)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "6400"
	}
	address := net.JoinHostPort(host, port)

	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", address, err)
	}

	if err := omt.SetSocketBuffers(conn); err != nil {
		conn.Close()
		return fmt.Errorf("set socket buffers: %w", err)
	}

	s.Log(logger.Debug, "connected to %s", address)

	readDone := make(chan error)
	go func() {
		readDone <- s.runReader(conn)
	}()

	for {
		select {
		case err = <-readDone:
			conn.Close()
			return err

		case <-params.ReloadConf:

		case <-params.Context.Done():
			conn.Close()
			<-readDone
			return nil
		}
	}
}

func (s *Source) runReader(conn net.Conn) error {
	writer := omt.NewWriter(conn)

	// Send subscription commands.
	if err := writer.WriteMetadataCommand(omt.XMLSubscribeVideo); err != nil {
		return fmt.Errorf("subscribe video: %w", err)
	}
	if err := writer.WriteMetadataCommand(omt.XMLSubscribeAudio); err != nil {
		return fmt.Errorf("subscribe audio: %w", err)
	}
	if err := writer.WriteMetadataCommand(omt.XMLSettingsCodecVMX1); err != nil {
		return fmt.Errorf("set codec: %w", err)
	}
	if err := writer.WriteMetadataCommand(omt.XMLInfoTemplate); err != nil {
		return fmt.Errorf("send info: %w", err)
	}

	reader := omt.NewReader(conn)

	// Define media formats for OMT passthrough.
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

	res := s.Parent.SetReady(defs.PathSourceStaticSetReadyReq{
		Desc:          &description.Session{Medias: medias},
		UseRTPPackets: false,
		ReplaceNTP:    true,
	})
	if res.Err != nil {
		return res.Err
	}

	defer s.Parent.SetNotReady(defs.PathSourceStaticSetNotReadyReq{})

	subStream := res.SubStream

	return s.readLoop(reader, subStream, medias, videoFormat, audioFormat)
}

func (s *Source) readLoop(
	reader *omt.Reader,
	subStream *stream.SubStream,
	medias []*description.Media,
	videoFormat *format.Generic,
	audioFormat *format.Generic,
) error {
	for {
		if err := reader.SetReadDeadline(time.Now().Add(time.Duration(s.ReadTimeout))); err != nil {
			return err
		}

		frame, err := reader.ReadFrame()
		if err != nil {
			return err
		}

		switch frame.Header.FrameType {
		case omt.FrameTypeVideo:
			if frame.VideoHeader == nil {
				continue
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
				continue
			}
			subStream.WriteUnit(medias[1], audioFormat, &unit.Unit{
				PTS: frame.Header.Timestamp,
				Payload: &unit.PayloadOMTAudio{
					AudioHeader: *frame.AudioHeader,
					Data:        frame.Data,
					Metadata:    frame.Metadata,
				},
			})

		case omt.FrameTypeMetadata:
			// For alpha: log metadata, don't process.
			if len(frame.Metadata) > 0 {
				s.Log(logger.Debug, "metadata: %s", string(frame.Metadata))
			}
		}
	}
}

// APISourceDescribe implements StaticSource.
func (*Source) APISourceDescribe() *defs.APIPathSource {
	return &defs.APIPathSource{
		Type: defs.APIPathSourceTypeOMTSource,
		ID:   "",
	}
}
