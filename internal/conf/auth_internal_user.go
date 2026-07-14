package conf

// AuthInternalUser is an user.
type AuthInternalUser struct {
	User        Credential                   `json:"user"`
	Pass        Credential                   `json:"pass"`
	StreamKey   Credential                   `json:"streamkey"`
	IPs         IPNetworks                   `json:"ips"`
	Permissions []AuthInternalUserPermission `json:"permissions"`
}
