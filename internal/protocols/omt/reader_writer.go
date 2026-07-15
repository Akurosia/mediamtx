package omt

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// Reader reads OMT frames from a TCP connection.
type Reader struct {
	conn net.Conn
	br   *bufio.Reader
	hdr  [MainHeaderSize]byte
}

// NewReader creates a new OMT frame reader.
func NewReader(conn net.Conn) *Reader {
	return &Reader{
		conn: conn,
		br:   bufio.NewReaderSize(conn, 512*1024),
	}
}

// SetReadDeadline sets the read deadline on the underlying connection.
func (r *Reader) SetReadDeadline(t time.Time) error {
	return r.conn.SetReadDeadline(t)
}

// ReadFrame reads a single OMT frame from the connection.
func (r *Reader) ReadFrame() (*Frame, error) {
	// Read main header.
	if _, err := io.ReadFull(r.br, r.hdr[:]); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	var f Frame
	if err := f.Header.Unmarshal(r.hdr[:]); err != nil {
		return nil, err
	}

	dataLen := int(f.Header.DataLength)
	if dataLen < 0 {
		return nil, fmt.Errorf("negative data length: %d", dataLen)
	}

	// Sanity check size.
	maxSize := MaxVideoSize
	switch f.Header.FrameType {
	case FrameTypeAudio:
		maxSize = MaxAudioSize
	case FrameTypeMetadata:
		maxSize = MaxMetadataSize
	}
	if dataLen > maxSize {
		return nil, fmt.Errorf("frame too large: %d bytes (max %d)", dataLen, maxSize)
	}

	if dataLen == 0 {
		return &f, nil
	}

	// Read the full data section.
	payload := make([]byte, dataLen)
	if _, err := io.ReadFull(r.br, payload); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}

	offset := 0

	// Parse extended headers.
	switch f.Header.FrameType {
	case FrameTypeVideo:
		if dataLen < VideoHeaderSize {
			return nil, fmt.Errorf("video frame too short for header: %d", dataLen)
		}
		f.VideoHeader = &VideoHeader{}
		if err := f.VideoHeader.Unmarshal(payload[:VideoHeaderSize]); err != nil {
			return nil, err
		}
		offset = VideoHeaderSize

	case FrameTypeAudio:
		if dataLen < AudioHeaderSize {
			return nil, fmt.Errorf("audio frame too short for header: %d", dataLen)
		}
		f.AudioHeader = &AudioHeader{}
		if err := f.AudioHeader.Unmarshal(payload[:AudioHeaderSize]); err != nil {
			return nil, err
		}
		offset = AudioHeaderSize
	}

	// Split data and metadata.
	remaining := payload[offset:]
	metaLen := int(f.Header.MetadataLength)
	if metaLen > len(remaining) {
		return nil, fmt.Errorf("metadata length %d exceeds remaining %d", metaLen, len(remaining))
	}

	if metaLen > 0 {
		f.Metadata = remaining[len(remaining)-metaLen:]
		f.Data = remaining[:len(remaining)-metaLen]
	} else {
		f.Data = remaining
	}

	return &f, nil
}

// Writer writes OMT frames to a TCP connection.
type Writer struct {
	conn net.Conn
	bw   *bufio.Writer
}

// NewWriter creates a new OMT frame writer.
func NewWriter(conn net.Conn) *Writer {
	return &Writer{
		conn: conn,
		bw:   bufio.NewWriterSize(conn, 512*1024),
	}
}

// SetWriteDeadline sets the write deadline on the underlying connection.
func (w *Writer) SetWriteDeadline(t time.Time) error {
	return w.conn.SetWriteDeadline(t)
}

// WriteFrame writes a single OMT frame to the connection.
func (w *Writer) WriteFrame(f *Frame) error {
	// Calculate data length.
	extHeaderSize := 0
	switch f.Header.FrameType {
	case FrameTypeVideo:
		extHeaderSize = VideoHeaderSize
	case FrameTypeAudio:
		extHeaderSize = AudioHeaderSize
	}

	dataLen := extHeaderSize + len(f.Data) + len(f.Metadata)
	f.Header.DataLength = int32(dataLen)
	f.Header.MetadataLength = uint16(len(f.Metadata))
	f.Header.Version = Version

	// Write main header.
	var hdr [MainHeaderSize]byte
	f.Header.Marshal(hdr[:])
	if _, err := w.bw.Write(hdr[:]); err != nil {
		return err
	}

	// Write extended header.
	switch f.Header.FrameType {
	case FrameTypeVideo:
		if f.VideoHeader != nil {
			var vh [VideoHeaderSize]byte
			f.VideoHeader.Marshal(vh[:])
			if _, err := w.bw.Write(vh[:]); err != nil {
				return err
			}
		}
	case FrameTypeAudio:
		if f.AudioHeader != nil {
			var ah [AudioHeaderSize]byte
			f.AudioHeader.Marshal(ah[:])
			if _, err := w.bw.Write(ah[:]); err != nil {
				return err
			}
		}
	}

	// Write data.
	if len(f.Data) > 0 {
		if _, err := w.bw.Write(f.Data); err != nil {
			return err
		}
	}

	// Write metadata at end.
	if len(f.Metadata) > 0 {
		if _, err := w.bw.Write(f.Metadata); err != nil {
			return err
		}
	}

	return w.bw.Flush()
}

// WriteMetadataCommand writes an XML command as a metadata frame.
func (w *Writer) WriteMetadataCommand(xml string) error {
	return w.WriteFrame(&Frame{
		Header: Header{
			FrameType: FrameTypeMetadata,
		},
		Metadata: []byte(xml),
	})
}

// SetSocketBuffers configures TCP socket buffer sizes for OMT.
func SetSocketBuffers(conn net.Conn) error {
	if tc, ok := conn.(*net.TCPConn); ok {
		if err := tc.SetNoDelay(true); err != nil {
			return err
		}
		if err := tc.SetWriteBuffer(SendBufferSize); err != nil {
			return err
		}
		if err := tc.SetReadBuffer(ReceiveBufferSize); err != nil {
			return err
		}
	}
	return nil
}

// XML subscription commands.
const (
	XMLSubscribeVideo    = `<OMTSubscribe Video="true" />`
	XMLSubscribeAudio    = `<OMTSubscribe Audio="true" />`
	XMLSubscribeMetadata = `<OMTSubscribe Metadata="true" />`
	XMLSettingsCodecVMX1 = `<OMTSettings Codec="vmx1" />`
	XMLInfoTemplate      = `<OMTInfo ProductName="mediamtx" Manufacturer="bluenviron" Version="1.0" />`
)

// RawFrame is an unparsed frame for opaque relay (header bytes + payload).
// Used for OMT-to-OMT passthrough without parsing extended headers.
type RawFrame struct {
	HeaderBytes [MainHeaderSize]byte
	Payload     []byte // everything after the 16-byte header (DataLength bytes)
}

// Timestamp extracts the timestamp from a raw frame header.
func (rf *RawFrame) Timestamp() int64 {
	return int64(binary.LittleEndian.Uint64(rf.HeaderBytes[2:10]))
}

// Type extracts the frame type from a raw frame header.
func (rf *RawFrame) Type() FrameType {
	return FrameType(rf.HeaderBytes[1])
}
