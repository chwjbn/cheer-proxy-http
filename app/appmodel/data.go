package appmodel

type ProxyAccount struct {
	AccountId string `json:"account_id"`
	Token  string `json:"token"`
	SubUrlData string `json:"sub_url_data"`
}

type ProxyNode struct {
	NodeId string `json:"node_id"`
	ServerAddr string `json:"server_addr"`
	ServerPort int `json:"server_port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Status string `json:"status"`
}

type ProxyInboundNode struct {
	InboundId string `json:"inbound_id"`
	NodeId string `json:"node_id"`
	BalanceWeight int `json:"balance_weight"`
	BeginTime string `json:"begin_time"`
	EndTime string `json:"end_time"`
}

type ProxyInbound struct {
	InboundId string `json:"inbound_id"`
	PasswordHash string `json:"password_hash"`
	BalanceType string `json:"balance_type"`
	NodePool []ProxyInboundNode `json:"node_pool"`
}

type ProxyInboundIndex struct {
	LastIndex int `json:"last_index"`
	LastTime int64 `json:"last_time"`
}