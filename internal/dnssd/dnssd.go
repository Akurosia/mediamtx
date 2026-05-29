package dnssd

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/bluenviron/mediamtx/internal/logger"
	"github.com/grandcat/zeroconf"
)

const (
	serviceType = "_omt._tcp"
	domain      = "local."
	browseTTL   = 5 * time.Second
)

type Parent interface {
	logger.Writer
}

type ServiceEntry struct {
	Name string
	Host string
	Port int
	Addr net.IP
}

type Manager struct {
	InstanceName string
	Port         int
	Parent       Parent

	mu       sync.Mutex
	server   *zeroconf.Server
	ctx      context.Context
	ctxCancel func()
}

func (m *Manager) Initialize() error {
	m.ctx, m.ctxCancel = context.WithCancel(context.Background())
	return nil
}

func (m *Manager) Publish() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.server != nil {
		return nil
	}

	var err error
	m.server, err = zeroconf.Register(
		m.InstanceName,
		serviceType,
		domain,
		m.Port,
		nil,
		nil,
	)
	if err != nil {
		return fmt.Errorf("DNS-SD register: %w", err)
	}

	m.Parent.Log(logger.Info, "DNS-SD: published %s on port %d", m.InstanceName, m.Port)
	return nil
}

func (m *Manager) Unpublish() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.server != nil {
		m.server.Shutdown()
		m.server = nil
	}
}

func (m *Manager) Browse() ([]ServiceEntry, error) {
	if m.ctx == nil {
		return nil, fmt.Errorf("DNS-SD manager not initialized")
	}

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("DNS-SD resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	var results []ServiceEntry
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for e := range entries {
			var addr net.IP
			if len(e.AddrIPv4) > 0 {
				addr = e.AddrIPv4[0]
			} else if len(e.AddrIPv6) > 0 {
				addr = e.AddrIPv6[0]
			}
			results = append(results, ServiceEntry{
				Name: e.Instance,
				Host: net.JoinHostPort(e.HostName, strconv.Itoa(e.Port)),
				Port: e.Port,
				Addr: addr,
			})
		}
	}()

	browseCtx, browseCancel := context.WithTimeout(m.ctx, browseTTL)
	defer browseCancel()

	err = resolver.Browse(browseCtx, serviceType, domain, entries)
	if err != nil {
		return nil, fmt.Errorf("DNS-SD browse: %w", err)
	}

	<-browseCtx.Done()
	wg.Wait()

	return results, nil
}

func (m *Manager) Close() {
	m.Unpublish()
	if m.ctxCancel != nil {
		m.ctxCancel()
	}
}
