//go:build !linux || (!arm && !arm64)

package rpicamera

import (
	"fmt"

	"github.com/akurosia/mediamtx/internal/defs"
)

// Run implements StaticSource.
func (s *Source) Run(_ defs.StaticSourceRunParams) error {
	return fmt.Errorf("server was compiled without support for the Raspberry Pi Camera")
}
