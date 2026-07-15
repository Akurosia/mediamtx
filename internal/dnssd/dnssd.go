package dnssd

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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

// registration tracks a single dns-sd/avahi process for one service instance.
type registration struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// Manager handles DNS-SD service registration using native system tools.
// It supports multiple concurrent registrations (one per path/stream).
// On macOS it delegates to dns-sd (which talks to mDNSResponder).
// On Linux it delegates to avahi-publish-service (which talks to avahi-daemon).
type Manager struct {
	Port   int
	Parent Parent

	mu            sync.Mutex
	registrations map[string]*registration
	hostname      string
}

// Initialize prepares the manager.
func (m *Manager) Initialize() error {
	m.registrations = make(map[string]*registration)

	h, err := os.Hostname()
	if err != nil {
		h = "mediamtx"
	}
	m.hostname = strings.TrimSuffix(h, ".local")

	return nil
}

// instanceName builds the DNS-SD instance name for a given path.
// Format: "hostname (pathName)" — matches OMT Signal Generator convention.
func (m *Manager) instanceName(pathName string) string {
	return m.hostname + " (" + pathName + ")"
}

// Register registers a DNS-SD service for the given path name.
func (m *Manager) Register(pathName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.registrations[pathName]; exists {
		return
	}

	name := m.instanceName(pathName)
	portStr := strconv.Itoa(m.Port)

	ctx, cancel := context.WithCancel(context.Background())

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "dns-sd", "-R", name, serviceType, domain, portStr)
	case "linux":
		cmd = exec.CommandContext(ctx, "avahi-publish-service", name, serviceType, portStr)
	default:
		cancel()
		m.Parent.Log(logger.Warn, "DNS-SD: unsupported platform %s, skipping registration", runtime.GOOS)
		return
	}

	if err := cmd.Start(); err != nil {
		cancel()
		m.Parent.Log(logger.Warn, "DNS-SD register %q: %v", name, err)
		return
	}

	// Reap process in background.
	go func() {
		_ = cmd.Wait()
	}()

	m.registrations[pathName] = &registration{cmd: cmd, cancel: cancel}
	m.Parent.Log(logger.Info, "DNS-SD: registered %q on port %d", name, m.Port)
}

// Deregister removes the DNS-SD service for the given path name.
func (m *Manager) Deregister(pathName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reg, exists := m.registrations[pathName]
	if !exists {
		return
	}

	reg.cancel()
	delete(m.registrations, pathName)

	name := m.instanceName(pathName)
	m.Parent.Log(logger.Info, "DNS-SD: deregistered %q", name)
}

// Close stops all registrations.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for pathName, reg := range m.registrations {
		reg.cancel()
		name := m.instanceName(pathName)
		m.Parent.Log(logger.Info, "DNS-SD: deregistered %q", name)
	}
	m.registrations = nil
}
