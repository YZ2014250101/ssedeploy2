package model

// SSHCredential 是平台为单个 Pod 自动签发的 SSH 连接凭证。
// Host/Port/Username 来自平台配置（当前为占位域名与端口），
// Password 在创建 Pod 时随机生成并落库，每个 Pod 一份，互不相同。
type SSHCredential struct {
	ID        int64  `json:"id"`
	Namespace string `json:"namespace"`
	PodName   string `json:"podName"`
	Host      string `json:"host"`     // SSH 网关地址，例如 ssemarket.cn
	Port      int32  `json:"port"`     // 该 Pod 专属的 SSH 端口，创建时从起始端口自动分配
	Username  string `json:"username"` // 登录用户名，例如 cloud
	Password  string `json:"password"` // 自动生成的高强度专属密码
	CreatedAt int64  `json:"createdAt"`
}
