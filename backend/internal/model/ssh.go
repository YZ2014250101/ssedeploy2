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

// SSH 连接事件类型。
const (
	SSHEventAccepted   = "accepted"   // 密码认证成功
	SSHEventFailed     = "failed"     // 密码认证失败
	SSHEventDisconnect = "disconnect" // 连接断开（含认证前断开）
)

// SSHEvent 是一条 SSH 连接记录，由平台内置 SSH 网关在认证/断开时写入。
// 记录内容对齐 sshd 日志格式，便于排查谁在何时从哪里登录过该 Pod。
type SSHEvent struct {
	ID         int64  `json:"id"`
	Namespace  string `json:"namespace"`
	PodName    string `json:"podName"`
	Username   string `json:"username"`   // 尝试登录的用户名
	RemoteAddr string `json:"remoteAddr"` // 来源地址，例如 10.42.0.1:22643
	Event      string `json:"event"`      // accepted / failed / disconnect
	Message    string `json:"message"`    // sshd 风格详情
	CreatedAt  int64  `json:"createdAt"`
}

// SSHEventText 把事件类型翻译成界面文案。
func SSHEventText(event string) string {
	switch event {
	case SSHEventAccepted:
		return "登录成功"
	case SSHEventFailed:
		return "认证失败"
	case SSHEventDisconnect:
		return "连接断开"
	default:
		return event
	}
}
