package conf

import "github.com/akurosia/mediamtx/internal/conf/jsonwrapper"

// OptionalAuthInternalUser is an internal user with optional fields for patching.
type OptionalAuthInternalUser struct {
	User        *Credential                   `json:"user"`
	Pass        *Credential                   `json:"pass"`
	StreamKey   *Credential                   `json:"streamkey"`
	IPs         *IPNetworks                   `json:"ips"`
	Permissions *[]AuthInternalUserPermission `json:"permissions"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (u *OptionalAuthInternalUser) UnmarshalJSON(b []byte) error {
	type plain OptionalAuthInternalUser
	return jsonwrapper.Unmarshal(b, (*plain)(u))
}
