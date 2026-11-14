// Package config 负责从环境变量加载服务运行配置。
// 所有配置都有安全的默认值，保证本地 go run 即可启动。
package config

import (
	"os"
	"strconv"
	"time"
)

// Config 保存服务运行所需的全部配置项。
type Config struct {
	HTTPAddr        string        // HTTP 监听地址，例如 :8080
	DBPath          string        // SQLite 数据库文件路径
	JWTSecret       string        // JWT 签名密钥
	TokenTTL        time.Duration // 登录令牌有效期
	Kubeconfig      string        // kubeconfig 路径；为空时优先使用集群内配置
	Namespace       string        // 业务命名空间，平台上所有 Pod 都创建在这里
	StorageClass    string        // PVC 使用的 StorageClass；为空表示使用集群默认
	NodePortMin     int32         // 用户 Pod 自动创建 NodePort Service 的端口下限
	NodePortMax     int32         // 用户 Pod 自动创建 NodePort Service 的端口上限
	BaseDomain      string        // 子域名主域，例如 example.com，Ingress host 为 <prefix>.example.com
	IngressClass    string        // Ingress 使用的 IngressClass，k3s 内置 Traefik 为 traefik
	SharedDir       string        // 全局共享目录的本地路径，所有用户/所有 Pod 共用
	WebDir          string        // 前端静态资源目录；为空时不托管前端（本地开发由 Vite 提供）
	MetricsInterval time.Duration // metrics-server 采样间隔
	DefaultAdmin    string        // 首次启动时创建的管理员账号
	DefaultPassword string        // 首次启动时创建的管理员密码
	// SSH 网关信息：为每个 Pod 签发 SSH 凭证时使用，当前为云平台占位地址，
	// 后续更换网关只需修改环境变量，无需改动代码。
	SSHHost string
	SSHPort int32 // 端口分配起始值，每个 Pod 从该值开始分配唯一端口
	// SSHPortMax 是端口分配上限，耗尽后创建凭证会报错
	SSHPortMax int32
	SSHUser    string
}

// getEnv 读取环境变量，不存在时返回默认值。
func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getEnvDuration 读取时长类型的环境变量，例如 "15s"。
func getEnvDuration(key string, def time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return def
	}
	return d
}

// getEnvInt32 读取整数类型的环境变量，解析失败时回退默认值。
func getEnvInt32(key string, def int32) int32 {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return int32(n)
}

// Load 组装配置：环境变量优先，其次是默认值。
func Load() *Config {
	return &Config{
		HTTPAddr:     getEnv("SSD_HTTP_ADDR", ":8081"),
		DBPath:       getEnv("SSD_DB_PATH", "./data/ssedeploy.db"),
		JWTSecret:    getEnv("SSD_JWT_SECRET", "ssedeploy-dev-secret-please-change"),
		TokenTTL:     getEnvDuration("SSD_TOKEN_TTL", 24*time.Hour),
		Kubeconfig:   getEnv("SSD_KUBECONFIG", ""),
		Namespace:    getEnv("SSD_K8S_NAMESPACE", "ssedeploy"),
		StorageClass: getEnv("SSD_STORAGE_CLASS", ""),
		// 默认与离线交付 compose 中 k3s 容器发布的 30000-30300 端口段保持一致
		NodePortMin:  getEnvInt32("SSD_NODEPORT_MIN", 30000),
		NodePortMax:  getEnvInt32("SSD_NODEPORT_MAX", 30300),
		BaseDomain:   getEnv("SSD_BASE_DOMAIN", "example.com"),
		IngressClass: getEnv("SSD_INGRESS_CLASS", "traefik"),
		// 容器内由 compose 显式指定 /shared；本地 go run 用相对目录，避免在磁盘根目录建 /shared
		SharedDir: getEnv("SSD_SHARED_DIR", "./shared"),
		// 单进程部署（如云平台 deploy.sh）时指向 frontend/dist，由后端同时托管页面与接口
		WebDir:          getEnv("SSD_WEB_DIR", ""),
		MetricsInterval: getEnvDuration("SSD_METRICS_INTERVAL", 15*time.Second),
		DefaultAdmin:    getEnv("SSD_ADMIN_USER", "admin"),
		DefaultPassword: getEnv("SSD_ADMIN_PASSWORD", "admin123"),
		// SSH 凭证占位配置：云平台暂无独立域名，后续可通过环境变量替换
		SSHHost:    getEnv("SSD_SSH_HOST", "127.0.0.1"),
		SSHPort:    getEnvInt32("SSD_SSH_PORT", 22057),
		SSHPortMax: getEnvInt32("SSD_SSH_PORT_MAX", 22156),
		SSHUser:    getEnv("SSD_SSH_USER", "cloud"),
	}
}
