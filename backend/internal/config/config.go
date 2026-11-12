// Package config 负责从环境变量加载服务运行配置。
// 所有配置都有安全的默认值，保证本地 go run 即可启动。
package config

import (
	"os"
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
	SharedDir       string        // 全局共享目录的本地路径，所有用户/所有 Pod 共用
	MetricsInterval time.Duration // metrics-server 采样间隔
	DefaultAdmin    string        // 首次启动时创建的管理员账号
	DefaultPassword string        // 首次启动时创建的管理员密码
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

// Load 组装配置：环境变量优先，其次是默认值。
func Load() *Config {
	return &Config{
		HTTPAddr:        getEnv("SSD_HTTP_ADDR", ":8080"),
		DBPath:          getEnv("SSD_DB_PATH", "./data/ssedeploy.db"),
		JWTSecret:       getEnv("SSD_JWT_SECRET", "ssedeploy-dev-secret-please-change"),
		TokenTTL:        getEnvDuration("SSD_TOKEN_TTL", 24*time.Hour),
		Kubeconfig:      getEnv("SSD_KUBECONFIG", ""),
		Namespace:       getEnv("SSD_K8S_NAMESPACE", "ssedeploy"),
		StorageClass:    getEnv("SSD_STORAGE_CLASS", ""),
		// 容器内由 compose 显式指定 /shared；本地 go run 用相对目录，避免在磁盘根目录建 /shared
		SharedDir:       getEnv("SSD_SHARED_DIR", "./shared"),
		MetricsInterval: getEnvDuration("SSD_METRICS_INTERVAL", 15*time.Second),
		DefaultAdmin:    getEnv("SSD_ADMIN_USER", "admin"),
		DefaultPassword: getEnv("SSD_ADMIN_PASSWORD", "admin123"),
	}
}
