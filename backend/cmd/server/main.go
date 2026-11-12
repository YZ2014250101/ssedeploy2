// SSEDeploy 后端入口：加载配置 → 初始化数据库 → 连接 K8s → 启动指标采样 → 启动 HTTP 服务。
package main

import (
	"context"
	"log"
	"time"

	"ssedeploy/internal/config"
	"ssedeploy/internal/handler"
	"ssedeploy/internal/k8s"
	"ssedeploy/internal/router"
	"ssedeploy/internal/shared"
	"ssedeploy/internal/store"
)

func main() {
	cfg := config.Load()

	// 1. 初始化 SQLite（建表 + 首次创建管理员）
	st, err := store.New(cfg)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer st.Close()

	// 2. 连接 Kubernetes
	client, err := k8s.New(cfg)
	if err != nil {
		log.Fatalf("初始化 Kubernetes 客户端失败: %v", err)
	}
	podService := k8s.NewPodService(client, st)
	deployService := k8s.NewDeployService(client, st, podService)
	overviewService := k8s.NewOverviewService(client)
	hostService := k8s.NewHostService(client)
	gpuService := k8s.NewGPUService(client)
	clusterService := k8s.NewClusterService(client)
	storageService := k8s.NewStorageService(client)

	// 全局共享目录服务：所有用户、所有 Pod 共用同一个 /shared 数据卷
	sharedService, err := shared.New(cfg.SharedDir)
	if err != nil {
		log.Fatalf("初始化共享目录失败: %v", err)
	}
	log.Printf("共享目录：%s", sharedService.Root())

	// 3. 修复历史 Pod 的工作区挂载（PVC -> /home/workspace），幂等执行
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if repaired, err := podService.RepairWorkspaces(ctx); err != nil {
		log.Printf("修复历史 Pod 工作区失败: %v", err)
	} else if repaired > 0 {
		log.Printf("已修复 %d 个 Pod 的工作区挂载", repaired)
	}

	// 4. 后台定时采样 metrics-server，供监控折线图使用
	go runMetricsSampler(ctx, podService, st, cfg.MetricsInterval)

	// 主机指标采样：每 3 秒一次，保留最近 120 个采样点
	hostService.Start(ctx)

	// GPU 指标采样：每 3 秒一次，保留最近 120 个采样点
	gpuService.Start(ctx)

	// 集群页指标采样：每 15 秒一次，保留 7 天数据供多粒度聚合
	clusterService.Start(ctx)

	// 4. 启动 HTTP 服务
	h := handler.New(podService, deployService, overviewService, hostService, gpuService, clusterService, storageService, sharedService, st, cfg)
	r := router.New(cfg, h)
	log.Printf("SSEDeploy 已启动：%s（命名空间 %s）", cfg.HTTPAddr, cfg.Namespace)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("HTTP 服务启动失败: %v", err)
	}
}

// runMetricsSampler 周期性写入资源采样并清理 24 小时前的数据。
func runMetricsSampler(ctx context.Context, pods *k8s.PodService, st *store.Store, interval time.Duration) {
	// 启动后立即采样一次，避免首屏图表为空
	pods.SampleAll(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pods.SampleAll(ctx)
			_ = st.CleanupMetrics(time.Now().Add(-24 * time.Hour).Unix())
		}
	}
}
