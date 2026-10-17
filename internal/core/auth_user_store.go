package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"

	"github.com/akurosia/mediamtx/internal/conf"
)

// The JSON file is the complete authoritative internal-user list once created.
// Until then, users are imported from the YAML configuration.
func (p *Core) authUsersFile() string {
	if env := os.Getenv("MTX_AUTH_USERS_FILE"); env != "" {
		return env
	}
	if p.confPath != "" {
		return filepath.Join(filepath.Dir(p.confPath), "users.json")
	}
	return "users.json"
}

func (p *Core) loadAuthUsers(c *conf.Conf) error {
	if c.AuthMethod != conf.AuthMethodInternal {
		return nil
	}
	path := p.authUsersFile()
	// Recover from an interrupted Windows replacement if necessary.
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if _, backupErr := os.Stat(path + ".bak"); backupErr == nil {
				if err := os.Rename(path+".bak", path); err != nil {
					return fmt.Errorf("recovering users backup: %w", err)
				}
			}
		}
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading user store: %w", err)
	}
	var users []conf.AuthInternalUser
	if err := json.Unmarshal(b, &users); err != nil {
		return fmt.Errorf("decoding user store: %w", err)
	}
	if users == nil {
		return fmt.Errorf("user store must be a JSON array")
	}
	c.AuthInternalUsers = users
	if err := c.Validate(nil); err != nil {
		return fmt.Errorf("invalid user store: %w", err)
	}
	return nil
}

func (p *Core) saveAuthUsers(users []conf.AuthInternalUser) error {
	path := p.authUsersFile()
	b, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".users-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// On Windows, rename cannot replace an existing file. Keep a backup during replacement.
	if runtime.GOOS == "windows" {
		backup := path + ".bak"
		_ = os.Remove(backup)
		if err := os.Rename(path, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(f.Name(), path); err != nil {
			_ = os.Rename(backup, path)
			return err
		}
		_ = os.Remove(backup)
		return nil
	}
	return os.Rename(f.Name(), path)
}

// reloadAuthUsersFromFile applies edited credentials without reloading the
// MediaMTX configuration or disconnecting active media sessions. It is called
// only from the core event loop, which also serializes API changes.
func (p *Core) reloadAuthUsersFromFile(contents []byte) error {
	current := p.conf.Load()
	if current.AuthMethod != conf.AuthMethodInternal {
		return nil
	}
	var users []conf.AuthInternalUser
	if err := json.Unmarshal(contents, &users); err != nil {
		return fmt.Errorf("decoding JSON: %w", err)
	}
	if users == nil {
		return fmt.Errorf("user store must contain a JSON array")
	}
	next := current.Clone()
	next.AuthInternalUsers = users
	if err := next.Validate(nil); err != nil {
		return fmt.Errorf("validating users: %w", err)
	}
	if reflect.DeepEqual(next.AuthInternalUsers, current.AuthInternalUsers) {
		return nil
	}
	p.conf.Store(next)
	p.authManager.ReloadInternalUsers(next.AuthInternalUsers)
	return nil
}
