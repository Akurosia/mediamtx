// Package omt contains an OMT server.
package omt

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/bluenviron/mediamtx/internal/conf"
	"github.com/bluenviron/mediamtx/internal/defs"
	"github.com/bluenviron/mediamtx/internal/dnssd"
	"github.com/bluenviron/mediamtx/internal/externalcmd"
	"github.com/bluenviron/mediamtx/internal/logger"
	"github.com/bluenviron/mediamtx/internal/protocols/omt"
)

type serverPathManager interface {
	FindPathConf(req defs.PathFindPathConfReq) (*defs.PathFindPathConfRes, error)
	AddPublisher(req defs.PathAddPublisherReq) (*defs.PathAddPublisherRes, error)
	AddReader(req defs.PathAddReaderReq) (*defs.PathAddReaderRes, error)
}

type serverParent interface {
	logger.Writer
}

// Server is an OMT server.
type Server struct {
	Address             string
	RTSPAddress         string
	ReadTimeout         conf.Duration
	WriteTimeout        conf.Duration
	RunOnConnect        string
	RunOnConnectRestart bool
	RunOnDisconnect     string
	ExternalCmdPool     *externalcmd.Pool
	PathManager         serverPathManager
	Parent              serverParent

	ctx       context.Context
	ctxCancel func()
	wg        sync.WaitGroup
	ln        net.Listener
	conns     map[*conn]struct{}
	dnsSD     *dnssd.Manager

	// in
	chNewConn  chan net.Conn
	chAcceptErr chan error
	chCloseConn chan *conn
}

// Initialize initializes the server.
func (s *Server) Initialize() error {
	var err error
	s.ln, err = net.Listen("tcp", s.Address)
	if err != nil {
		return err
	}

	s.ctx, s.ctxCancel = context.WithCancel(context.Background())
	s.conns = make(map[*conn]struct{})
	s.chNewConn = make(chan net.Conn)
	s.chAcceptErr = make(chan error)
	s.chCloseConn = make(chan *conn)

	_, portStr, _ := net.SplitHostPort(s.ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	instanceName := "mediamtx-omt"
	if h, err := os.Hostname(); err == nil {
		h = strings.TrimSuffix(h, ".local")
		instanceName = h + " (mediamtx)"
	}

	s.dnsSD = &dnssd.Manager{
		InstanceName: instanceName,
		Port:         port,
		Parent:       s,
	}
	if err := s.dnsSD.Initialize(); err != nil {
		s.ln.Close()
		return err
	}
	if err := s.dnsSD.Publish(); err != nil {
		s.Log(logger.Warn, "DNS-SD publish failed: %v", err)
	}

	s.Log(logger.Info, "listener opened on "+s.Address+" (TCP)")

	s.wg.Add(1)
	go s.runAccept()

	s.wg.Add(1)
	go s.run()

	return nil
}

// Log implements logger.Writer.
func (s *Server) Log(level logger.Level, format string, args ...any) {
	s.Parent.Log(level, "[OMT] "+format, args...)
}

// Close closes the server.
func (s *Server) Close() {
	s.Log(logger.Info, "listener is closing")
	s.ctxCancel()
	s.ln.Close()
	s.wg.Wait()
	s.dnsSD.Close()
}

func (s *Server) runAccept() {
	defer s.wg.Done()

	for {
		netConn, err := s.ln.Accept()
		if err != nil {
			select {
			case s.chAcceptErr <- err:
			case <-s.ctx.Done():
			}
			return
		}

		select {
		case s.chNewConn <- netConn:
		case <-s.ctx.Done():
			netConn.Close()
			return
		}
	}
}

func (s *Server) run() {
	defer s.wg.Done()

outer:
	for {
		select {
		case err := <-s.chAcceptErr:
			s.Log(logger.Error, "accept error: %v", err)
			break outer

		case netConn := <-s.chNewConn:
			if err := omt.SetSocketBuffers(netConn); err != nil {
				s.Log(logger.Warn, "set socket buffers: %v", err)
			}

			c := &conn{
				parentCtx:           s.ctx,
				rtspAddress:         s.RTSPAddress,
				readTimeout:         s.ReadTimeout,
				writeTimeout:        s.WriteTimeout,
				netConn:             netConn,
				runOnConnect:        s.RunOnConnect,
				runOnConnectRestart: s.RunOnConnectRestart,
				runOnDisconnect:     s.RunOnDisconnect,
				externalCmdPool:     s.ExternalCmdPool,
				pathManager:         s.PathManager,
				parent:              s,
			}
			c.initialize()
			s.conns[c] = struct{}{}

		case c := <-s.chCloseConn:
			delete(s.conns, c)

		case <-s.ctx.Done():
			break outer
		}
	}

	// Close all connections.
	for c := range s.conns {
		c.Close()
	}
}

func (s *Server) closeConn(c *conn) {
	select {
	case s.chCloseConn <- c:
	case <-s.ctx.Done():
	}
}
