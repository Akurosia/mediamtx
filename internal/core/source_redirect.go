package core

import (
	"github.com/akurosia/mediamtx/internal/defs"
	"github.com/akurosia/mediamtx/internal/logger"
)

// sourceRedirect is a source that redirects to another one.
type sourceRedirect struct{}

func (*sourceRedirect) Log(logger.Level, string, ...any) {
}

// APISourceDescribe implements source.
func (*sourceRedirect) APISourceDescribe() *defs.APIPathSource {
	return &defs.APIPathSource{
		Type: defs.APIPathSourceTypeRedirect,
		ID:   "",
	}
}
