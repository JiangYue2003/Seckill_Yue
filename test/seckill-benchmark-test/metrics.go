package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	seckill "seckill-mall/seckill-service/seckill"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Metrics 性能指标收集器
type Metrics struct {
	mu sync.Mutex

	totalRequests  int64
	successCount   int64
	failCount      int64
	soldOutCount   int64
	duplicateCount int64
	systemErrCount int64

	failReasons      map[string]int64 // 失败原因明细（按响应码/错误类型）
	systemErrDetails map[string]int64 // 系统错误细分（按 message / rpc error）

	latencies []int64 // 毫秒

	firstSuccessTime time.Time // 第一个成功订单的时间
	lastSuccessTime  time.Time // 最后一个成功订单的时间
}

func NewMetrics() *Metrics {
	return &Metrics{
		latencies:        make([]int64, 0, 200000),
		failReasons:      make(map[string]int64, 16),
		systemErrDetails: make(map[string]int64, 16),
	}
}

// Record 记录单次请求结果
func (m *Metrics) Record(latencyMs int64, resp *seckill.SeckillResponse, err error) {
	atomic.AddInt64(&m.totalRequests, 1)

	// 记录延迟
	m.mu.Lock()
	m.latencies = append(m.latencies, latencyMs)
	m.mu.Unlock()

	if err != nil {
		atomic.AddInt64(&m.failCount, 1)
		atomic.AddInt64(&m.systemErrCount, 1)
		reason := classifyError(err)
		m.addFailReason(reason)
		m.addSystemErrDetail(reason)
		return
	}

	if resp == nil {
		atomic.AddInt64(&m.failCount, 1)
		atomic.AddInt64(&m.systemErrCount, 1)
		m.addFailReason("RESP_NIL")
		m.addSystemErrDetail("RESP_NIL")
		return
	}

	if resp.Success && resp.Code == "SUCCESS" {
		atomic.AddInt64(&m.successCount, 1)

		// 记录成功订单的时间戳
		now := time.Now()
		m.mu.Lock()
		if m.firstSuccessTime.IsZero() {
			m.firstSuccessTime = now
		}
		m.lastSuccessTime = now
		m.mu.Unlock()
	} else {
		atomic.AddInt64(&m.failCount, 1)
		code := resp.Code
		if code == "" {
			code = "EMPTY_CODE"
		}
		m.addFailReason("RESP_" + code)
		switch resp.Code {
		case "SOLD_OUT":
			atomic.AddInt64(&m.soldOutCount, 1)
		case "ALREADY_PURCHASED":
			atomic.AddInt64(&m.duplicateCount, 1)
		default:
			atomic.AddInt64(&m.systemErrCount, 1)
			m.addSystemErrDetail(classifySystemResponse(resp))
		}
	}
}

// Report 生成性能报告
func (m *Metrics) Report(totalDuration time.Duration, totalStock, actualSold int64) {
	total := atomic.LoadInt64(&m.totalRequests)
	success := atomic.LoadInt64(&m.successCount)
	fail := atomic.LoadInt64(&m.failCount)
	soldOut := atomic.LoadInt64(&m.soldOutCount)
	duplicate := atomic.LoadInt64(&m.duplicateCount)

	m.mu.Lock()
	latencies := make([]int64, len(m.latencies))
	copy(latencies, m.latencies)
	m.mu.Unlock()

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	// 计算延迟百分位数
	p50 := percentile(latencies, 50)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)

	// 计算平均延迟
	var totalLatency int64
	for _, l := range latencies {
		totalLatency += l
	}
	avgLatency := int64(0)
	if len(latencies) > 0 {
		avgLatency = totalLatency / int64(len(latencies))
	}

	// 计算 QPS 和 TPS
	qps := float64(total) / totalDuration.Seconds()
	tps := float64(success) / totalDuration.Seconds()

	// 计算秒杀阶段 TPS（只看成功订单的时间窗口）
	seckillPhaseTPS := float64(0)
	seckillPhaseDuration := float64(0)
	m.mu.Lock()
	if !m.firstSuccessTime.IsZero() && !m.lastSuccessTime.IsZero() {
		seckillPhaseDuration = m.lastSuccessTime.Sub(m.firstSuccessTime).Seconds()
		if seckillPhaseDuration > 0 {
			seckillPhaseTPS = float64(success) / seckillPhaseDuration
		} else {
			// 所有成功订单在同一毫秒内完成，使用总耗时
			seckillPhaseTPS = tps
		}
	}
	m.mu.Unlock()

	// 计算成功率
	successRate := float64(0)
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	// 计算超卖率
	oversellRate := float64(0)
	if totalStock > 0 {
		oversellRate = float64(actualSold) / float64(totalStock) * 100
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   性能报告")
	fmt.Println("========================================")

	fmt.Printf("  请求统计:\n")
	fmt.Printf("    总请求数:     %d\n", total)
	fmt.Printf("    成功数:       %d (%.2f%%)\n", success, successRate)
	fmt.Printf("    失败数:       %d\n", fail)
	fmt.Printf("      - 库存不足:  %d\n", soldOut)
	fmt.Printf("      - 用户重复:  %d\n", duplicate)
	fmt.Printf("      - 系统错误:  %d\n", atomic.LoadInt64(&m.systemErrCount))

	m.mu.Lock()
	reasonPairs := make([]reasonCount, 0, len(m.failReasons))
	for reason, count := range m.failReasons {
		reasonPairs = append(reasonPairs, reasonCount{reason: reason, count: count})
	}
	systemErrPairs := make([]reasonCount, 0, len(m.systemErrDetails))
	for reason, count := range m.systemErrDetails {
		systemErrPairs = append(systemErrPairs, reasonCount{reason: reason, count: count})
	}
	m.mu.Unlock()
	sort.Slice(reasonPairs, func(i, j int) bool {
		if reasonPairs[i].count == reasonPairs[j].count {
			return reasonPairs[i].reason < reasonPairs[j].reason
		}
		return reasonPairs[i].count > reasonPairs[j].count
	})
	if len(reasonPairs) > 0 {
		fmt.Printf("    失败原因明细:\n")
		for _, item := range reasonPairs {
			fmt.Printf("      - %s: %d\n", item.reason, item.count)
		}
	}
	sort.Slice(systemErrPairs, func(i, j int) bool {
		if systemErrPairs[i].count == systemErrPairs[j].count {
			return systemErrPairs[i].reason < systemErrPairs[j].reason
		}
		return systemErrPairs[i].count > systemErrPairs[j].count
	})
	if len(systemErrPairs) > 0 {
		fmt.Printf("    系统错误细分:\n")
		for _, item := range systemErrPairs {
			fmt.Printf("      - %s: %d\n", item.reason, item.count)
		}
	}

	fmt.Printf("\n  库存统计:\n")
	fmt.Printf("    初始库存:     %d\n", totalStock)
	fmt.Printf("    实际售出:     %d\n", actualSold)
	fmt.Printf("    超卖率:       %.2f%%\n", oversellRate)

	fmt.Printf("\n  性能指标:\n")
	fmt.Printf("    总耗时:       %.2fs\n", totalDuration.Seconds())
	fmt.Printf("    QPS:          %.2f req/s\n", qps)
	fmt.Printf("    TPS (整体):   %.2f orders/s\n", tps)
	if seckillPhaseTPS > 0 {
		fmt.Printf("    TPS (秒杀阶段): %.2f orders/s (%.2fs)\n", seckillPhaseTPS, seckillPhaseDuration)
	}

	fmt.Printf("\n  延迟统计 (ms):\n")
	fmt.Printf("    平均延迟:     %d\n", avgLatency)
	fmt.Printf("    P50:          %d\n", p50)
	fmt.Printf("    P95:          %d\n", p95)
	fmt.Printf("    P99:          %d\n", p99)

	if len(latencies) > 0 {
		fmt.Printf("    最小延迟:     %d\n", latencies[0])
		fmt.Printf("    最大延迟:     %d\n", latencies[len(latencies)-1])
	}

	fmt.Println("========================================")
}

// percentile 计算百分位数
func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * float64(p) / 100)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

type reasonCount struct {
	reason string
	count  int64
}

func (m *Metrics) addFailReason(reason string) {
	if reason == "" {
		reason = "UNKNOWN"
	}
	m.mu.Lock()
	m.failReasons[reason]++
	m.mu.Unlock()
}

func (m *Metrics) addSystemErrDetail(reason string) {
	if reason == "" {
		reason = "SYS_UNKNOWN"
	}
	m.mu.Lock()
	m.systemErrDetails[reason]++
	m.mu.Unlock()
}

func classifyError(err error) string {
	if err == nil {
		return "ERR_UNKNOWN"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "ERR_CONTEXT_DEADLINE_EXCEEDED"
	}
	if errors.Is(err, context.Canceled) {
		return "ERR_CONTEXT_CANCELED"
	}
	if st, ok := status.FromError(err); ok {
		code := st.Code()
		if code == codes.OK {
			return "ERR_RPC_OK"
		}
		return "ERR_RPC_" + code.String()
	}
	return "ERR_LOCAL"
}

func classifySystemResponse(resp *seckill.SeckillResponse) string {
	if resp == nil {
		return "RESP_NIL"
	}
	code := resp.Code
	if code == "" {
		code = "EMPTY_CODE"
	}
	msg := resp.Message
	if msg == "" {
		msg = "EMPTY_MESSAGE"
	}
	return "RESP_" + code + "|" + msg
}
