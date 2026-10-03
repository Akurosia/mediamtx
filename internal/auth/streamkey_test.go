package auth

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/akurosia/mediamtx/internal/conf"
)

func TestAuthInternalStreamKey(t *testing.T) {
	for _, key := range []conf.Credential{
		"testpass",
		"sha256:E9JJ8stBJ7QM+nV4ZoUCeHk/gU3tPFh/5YieiJp6n2w=",
		"argon2:$argon2i$v=19$m=4096,t=3,p=1$MTIzNDU2Nzg$/mrZ42TiTv1mcPnpMUera5oi0SFYbbyueAbdx5sUvWo",
	} {
		for _, outcome := range []string{"valid", "wrong key", "missing key", "wrong path", "wrong action", "wrong IP"} {
			t.Run(string(key)+"/"+outcome, func(t *testing.T) {
				m := &Manager{Method: conf.AuthMethodInternal, InternalUsers: []conf.AuthInternalUser{{
					User: "publisher", Pass: "password", StreamKey: key,
					IPs:         conf.IPNetworks{mustParseCIDR("127.0.0.0/8")},
					Permissions: []conf.AuthInternalUserPermission{{Action: conf.AuthActionPublish, Path: "live"}},
				}}}
				req := &Request{Action: conf.AuthActionPublish, Path: "live", Query: "key=testpass",
					IP: net.ParseIP("127.0.0.1"), Credentials: &Credentials{}}
				switch outcome {
				case "wrong key":
					req.Query = "key=wrong"
				case "missing key":
					req.Query = ""
				case "wrong path":
					req.Path = "private"
				case "wrong action":
					req.Action = conf.AuthActionRead
				case "wrong IP":
					req.IP = net.ParseIP("192.0.2.1")
				}
				user, err := m.Authenticate(req)
				if outcome == "valid" {
					require.Nil(t, err)
					require.Equal(t, "publisher", user)
				} else {
					require.NotNil(t, err)
				}
			})
		}
	}
}
