package forward

import (
	"context"

	"github.com/akurosia/mediamtx/internal/defs"
)

// Dest is a protocol-specific forward destination.
type Dest interface {
	Run(context.Context) error
	Info() defs.ForwardDestInfo
}
