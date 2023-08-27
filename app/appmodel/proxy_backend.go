package appmodel

type ProxyBackend struct {
	NodeId string `json:"node_id"`
	ServerAddr string `json:"server_addr"`
	ServerPort int `json:"server_port"`
	Username string `json:"username"`
	Password string `json:"password"`
}
