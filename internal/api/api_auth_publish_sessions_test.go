package api //nolint:revive

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDisconnectPublisherSessions(t *testing.T) {
	called := 0
	sessions := []publisherSession{
		{kick: func() error { called++; return nil }},
		{kick: func() error { called++; return errors.New("already closed") }},
	}
	ok, failed := disconnectPublisherSessions(sessions)
	require.Equal(t, 2, called)
	require.Equal(t, 1, ok)
	require.Equal(t, 1, failed)
}
