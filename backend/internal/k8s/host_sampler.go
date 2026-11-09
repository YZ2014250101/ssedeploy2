package k8s

import (
	"context"
	"sync"
	"time"

	"ssedeploy/internal/model"
)

// maxSamples 是时序数据保留的采样点数量，对应页面上的「近120采样点」。
const maxSamples = 120

// rawStats 是采集器一次读取到的累计计数器原始值。
// 所有速率类指标都由相邻两次采样的差值计算得到。
type rawStats struct {
	cpuTotal uint64 // CPU 总时间片
	cpuIdle  uint64 // CPU 空闲时间片（含 iowait）

	memTotal  uint64 // 内存总量（字节）
	memAvail  uint64 // 可用内存（字节）
	swapTotal uint64 // Swap 总量（字节）
	swapFree  uint64 // Swap 空闲（字节）

	load1  float64 // 1 分钟负载
	load5  float64 // 5 分钟负载
	load15 float64 // 15 分钟负载

	uptime uint64 // 主机运行秒数

	netRxBytes uint64 // 累计接收字节
	netTxBytes uint64 // 累计发送字节
	netRxPkts  uint64 // 累计接收包
	netTxPkts  uint64 // 累计发送包
	netRxDrop  uint64 // 累计接收丢包
	netTxDrop  uint64 // 累计发送丢包

	diskReadBytes  uint64 // 累计读取字节
	diskWriteBytes uint64 // 累计写入字节
	diskWrites     uint64 // 累计写次数
	diskReads      uint64 // 累计读次数
	diskReadTicks  uint64 // 累计读耗时（毫秒）
	diskWritTicks  uint64 // 累计写耗时（毫秒）
	diskIoTicks    uint64 // 累计磁盘繁忙时间（毫秒）
	collected      bool   // 是否成功采集

	rootUsed  uint64 // 根分区已用（字节）
	rootTotal uint64 // 根分区总量（字节）

	partitions []model.DiskPartition // 分区使用情况
}

// hostSampler 周期性采样并把结果保存在内存环形缓冲区中。
type hostSampler struct {
	mu        sync.Mutex
	samples   []model.MetricSample
	latest    model.MetricSample
	raw       rawStats
	prev      *rawStats
	prevAt    time.Time
	updatedAt time.Time
	available bool
}

// start 启动后台采样：立即采一次，之后按 interval 周期执行。
func (s *hostSampler) start(ctx context.Context, interval time.Duration) {
	go func() {
		s.collect()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.collect()
			}
		}
	}()
}

// collect 采集一次原始计数器并换算成速率指标。
func (s *hostSampler) collect() {
	raw, err := collectRaw()
	if err != nil {
		s.mu.Lock()
		s.available = false
		s.mu.Unlock()
		return
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.available = true
	s.raw = raw
	s.updatedAt = now

	// 第一次采集没有对比基准，只记录原始值
	if s.prev != nil {
		elapsed := now.Sub(s.prevAt).Seconds()
		if elapsed > 0.5 {
			sample := computeSample(s.prev, &raw, elapsed)
			sample.TS = now.Unix()
			s.samples = append(s.samples, sample)
			// 超过上限时丢弃最旧的采样点
			if len(s.samples) > maxSamples {
				s.samples = s.samples[len(s.samples)-maxSamples:]
			}
			s.latest = sample
		}
	}

	prev := raw
	s.prev = &prev
	s.prevAt = now
}

// snapshot 返回当前采样结果的副本，避免调用方读到正在写入的切片。
func (s *hostSampler) snapshot() (model.MetricSample, rawStats, []model.MetricSample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := make([]model.MetricSample, len(s.samples))
	copy(samples, s.samples)
	return s.latest, s.raw, samples, s.available
}

// computeSample 由两次累计计数器差值计算速率与使用率。
func computeSample(prev, cur *rawStats, elapsed float64) model.MetricSample {
	var sample model.MetricSample

	// CPU 使用率 = 1 - 空闲增量 / 总增量
	if cur.cpuTotal > prev.cpuTotal {
		total := cur.cpuTotal - prev.cpuTotal
		idle := safeDelta(prev.cpuIdle, cur.cpuIdle)
		sample.CPUPercent = clampPercent((1 - float64(idle)/float64(total)) * 100)
	}

	// 内存使用率
	if cur.memTotal > 0 {
		used := cur.memTotal - cur.memAvail
		sample.MemoryPercent = clampPercent(float64(used) / float64(cur.memTotal) * 100)
	}

	// 网络吞吐与包速率
	sample.NetRxBps = float64(safeDelta(prev.netRxBytes, cur.netRxBytes)) / elapsed
	sample.NetTxBps = float64(safeDelta(prev.netTxBytes, cur.netTxBytes)) / elapsed
	rxPktRate := float64(safeDelta(prev.netRxPkts, cur.netRxPkts)) / elapsed
	txPktRate := float64(safeDelta(prev.netTxPkts, cur.netTxPkts)) / elapsed
	sample.PktRx = rxPktRate
	sample.PktTx = txPktRate

	// 丢包率 = 丢包增量 / 包增量（按网卡 drop 计数估算）
	sample.RxDropPercent = ratioPercent(safeDelta(prev.netRxDrop, cur.netRxDrop), safeDelta(prev.netRxPkts, cur.netRxPkts))
	sample.TxDropPercent = ratioPercent(safeDelta(prev.netTxDrop, cur.netTxDrop), safeDelta(prev.netTxPkts, cur.netTxPkts))

	// 磁盘吞吐、IOPS
	sample.DiskReadBps = float64(safeDelta(prev.diskReadBytes, cur.diskReadBytes)) / elapsed
	sample.DiskWriteBps = float64(safeDelta(prev.diskWriteBytes, cur.diskWriteBytes)) / elapsed
	readOps := safeDelta(prev.diskReads, cur.diskReads)
	writeOps := safeDelta(prev.diskWrites, cur.diskWrites)
	sample.IOPSRead = float64(readOps) / elapsed
	sample.IOPSWrite = float64(writeOps) / elapsed

	// 磁盘 await = 读写总耗时 / 读写总次数（毫秒/次）
	ticks := safeDelta(prev.diskReadTicks, cur.diskReadTicks) + safeDelta(prev.diskWritTicks, cur.diskWritTicks)
	if ops := readOps + writeOps; ops > 0 {
		sample.DiskAwaitMs = float64(ticks) / float64(ops)
	}

	// 磁盘繁忙度 = 繁忙时间增量 / 采样间隔
	busy := safeDelta(prev.diskIoTicks, cur.diskIoTicks)
	if busy > 0 {
		sample.DiskBusyPercent = clampPercent(float64(busy) / (elapsed * 1000) * 100)
	}

	return sample
}

// safeDelta 计算两个累计计数器的差值；计数器重置时返回 0。
func safeDelta(prev, cur uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}

// ratioPercent 计算百分比，分母为 0 时返回 0。
func ratioPercent(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// clampPercent 把百分比限制在 0~100。
func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return float64(int(value*100+0.5)) / 100
}
