package dnssd

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"sync"

	"github.com/bluenviron/mediamtx/internal/logger"
)

const (
	serviceType = "_omt._tcp"
	domain      = "local."
)

// Parent is implemented by the caller (OMT server).
type Parent interface {
	logger.Writer
}

// Manager handles DNS-SD service registration using native system tools.
// On macOS it delegates to dns-sd (which talks to mDNSResponder).
// On Linux it delegates to avahi-publish-service (which talks to avahi-daemon).
type Manager struct {
	InstanceName string
	Port         int
	Parent       Parent

	mu        sync.Mutex
	cmd       *exec.Cmd
	ctx       context.Context
	ctxCancel context.CancelFunc
}

// Initialize prepares the manager context.
func (m *Manager) Initialize() error {
	m.ctx, m.ctxCancel = context.WithCancel(context.Background())
	return nil
}

// Publish registers the service via the platform's native DNS-SD tool.
func (m *Manager) Publish() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil {
		return nil
	}

	portStr := strconv.Itoa(m.Port)

	switch runtime.GOOS {
	case "darwin":
		// dns-sd -R <name> <type> <domain> <port>
		m.cmd = exec.CommandContext(m.ctx, "dns-sd", "-R",
			m.InstanceName, serviceType, domain, portStr)

	case "linux":
		// avahi-publish-service <name> <type> <port>
		m.cmd = exec.CommandContext(m.ctx, "avahi-publish-service",
			m.InstanceName, serviceType, portStr)

	default:
		m.Parent.Log(logger.Warn, "DNS-SD: unsupported platform %s, skipping registration", runtime.GOOS)
		return nil
	}

	// Both commands run as daemons (block until killed).
	// Start them in the background; context cancellation will terminate them.
	if err := m.cmd.Start(); err != nil {
		m.cmd = nil
		return fmt.Errorf("DNS-SD register: %w", err)
	}

	// Reap the process in background to avoid zombies.
	go func() {
		_ = m.cmd.Wait()
	}()

	m.Parent.Log(logger.Info, "DNS-SD: published %s on port %d", m.InstanceName, m.Port)
	return nil
}

// Close stops the registration process.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ctxCancel != nil {
		m.ctxCancel()
	}
	m.cmd = nil
}
