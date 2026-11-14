package model

// DomainMapping 是 Pod 的一条子域名映射，对应集群中的一个 Ingress。
type DomainMapping struct {
	Prefix      string `json:"prefix"`      // 子域名前缀，例如 my-app
	Host        string `json:"host"`        // 完整域名，例如 my-app.example.com
	Remark      string `json:"remark"`      // 备注，保存在 Ingress 注解中
	PodName     string `json:"podName"`     // 归属 Pod
	ServiceName string `json:"serviceName"` // Ingress 指向的后端 Service
	Port        int32  `json:"port"`        // 后端 Service 端口
	IngressName string `json:"ingressName"`
	CreatedAt   int64  `json:"createdAt"`
}

// CreateDomainRequest 创建 / 更新子域名映射的入参。
type CreateDomainRequest struct {
	Prefix string `json:"prefix"` // 子域名前缀，仅允许小写字母、数字、中划线
	Port   int32  `json:"port"`   // 后端端口，0 表示使用 Service 的第一个端口
	Remark string `json:"remark"` // 备注（可选）
}
