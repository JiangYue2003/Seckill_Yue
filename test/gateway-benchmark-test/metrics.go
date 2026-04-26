package main

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Metrics struct {
	scheduled  int64
	dispatched int64
	completed  int64
	dropped    int64

	successCount   int64
	failCount      int64
	soldOutCount   int64
	duplicateCount int64
	systemErrCount int64

	mu               sync.Mutex
	latencies        []int64
	failReasons      map[string]int64
	systemErrDetails map[string]int64
	dropReasons      map[string]int64

	firstSuccessTime time.Time
	lastSuccessTime  time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{
		latencies:        make([]int64, 0, 200000),
		failReasons:      make(map[string]int64, 16),
		systemErrDetails: make(map[string]int64, 16),
		dropReasons:      make(map[string]int64, 8),
	}
}

func (m *Metrics) IncScheduled() {
	atomic.AddInt64(&m.scheduled, 1)
}

func (m *Metrics) IncDispatched() {
	atomic.AddInt64(&m.dispatched, 1)
}

func (m *Metrics) IncDropped(reason string) {
	atomic.AddInt64(&m.dropped, 1)
	m.mu.Lock()
	m.dropReasons[reason]++
	m.mu.Unlock()
}

func (m *Metrics) RecordSystemFail(latencyMs int64, reason string) {
	atomic.AddInt64(&m.completed, 1)
	atomic.AddInt64(&m.failCount, 1)
	atomic.AddInt64(&m.systemErrCount, 1)

	m.mu.Lock()
	m.latencies = append(m.latencies, latencyMs)
	m.failReasons[reason]++
	m.systemErrDetails[reason]++
	m.mu.Unlock()
}

func (m *Metrics) RecordResult(latencyMs int64, r result) {
	atomic.AddInt64(&m.completed, 1)
	m.mu.Lock()
	m.latencies = append(m.latencies, latencyMs)
	m.mu.Unlock()

	if r.success {
		atomic.AddInt64(&m.successCount, 1)
		now := time.Now()
		m.mu.Lock()
		if m.firstSuccessTime.IsZero() {
			m.firstSuccessTime = now
		}
		m.lastSuccessTime = now
		m.mu.Unlock()
		return
	}

	atomic.AddInt64(&m.failCount, 1)
	m.mu.Lock()
	if r.failReason != "" {
		m.failReasons[r.failReason]++
	}
	m.mu.Unlock()

	switch r.bizCode {
	case "SOLD_OUT":
		atomic.AddInt64(&m.soldOutCount, 1)
	case "ALREADY_PURCHASED":
		atomic.AddInt64(&m.duplicateCount, 1)
	}

	if r.systemErr {
		atomic.AddInt64(&m.systemErrCount, 1)
		key := r.systemErrCode
		if key == "" {
			key = "SYS_UNKNOWN"
		}
		m.mu.Lock()
		m.systemErrDetails[key]++
		m.mu.Unlock()
	}
}

func (m *Metrics) Report(totalDuration time.Duration, totalStock, actualSold int64) {
	scheduled := atomic.LoadInt64(&m.scheduled)
	dispatched := atomic.LoadInt64(&m.dispatched)
	completed := atomic.LoadInt64(&m.completed)
	dropped := atomic.LoadInt64(&m.dropped)
	success := atomic.LoadInt64(&m.successCount)
	fail := atomic.LoadInt64(&m.failCount)
	soldOut := atomic.LoadInt64(&m.soldOutCount)
	duplicate := atomic.LoadInt64(&m.duplicateCount)
	systemErr := atomic.LoadInt64(&m.systemErrCount)

	m.mu.Lock()
	latencies := make([]int64, len(m.latencies))
	copy(latencies, m.latencies)
	failReasons := copyReasonMap(m.failReasons)
	systemErrDetails := copyReasonMap(m.systemErrDetails)
	dropReasons := copyReasonMap(m.dropReasons)
	firstSuccess := m.firstSuccessTime
	lastSuccess := m.lastSuccessTime
	m.mu.Unlock()

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	p50 := percentile(latencies, 50)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)

	var sum int64
	for _, v := range latencies {
		sum += v
	}
	avg := int64(0)
	if len(latencies) > 0 {
		avg = sum / int64(len(latencies))
	}

	qps := float64(completed) / totalDuration.Seconds()
	tpsCompleted := float64(completed) / totalDuration.Seconds()
	tpsBusinessSuccess := float64(success) / totalDuration.Seconds()

	seckillPhaseTPS := float64(0)
	seckillPhaseDuration := float64(0)
	if !firstSuccess.IsZero() && !lastSuccess.IsZero() {
		seckillPhaseDuration = lastSuccess.Sub(firstSuccess).Seconds()
		if seckillPhaseDuration > 0 {
			seckillPhaseTPS = float64(success) / seckillPhaseDuration
		} else {
			seckillPhaseTPS = tpsBusinessSuccess
		}
	}

	successRate := float64(0)
	if completed > 0 {
		successRate = float64(success) / float64(completed) * 100
	}
	oversellRate := float64(0)
	if totalStock > 0 {
		oversellRate = float64(actualSold) / float64(totalStock) * 100
	}
	dispatchRate := float64(0)
	if scheduled > 0 {
		dispatchRate = float64(dispatched) / float64(scheduled) * 100
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   半链路性能报告 (Go Open-Loop)")
	fmt.Println("========================================")
	fmt.Printf("  调度统计:\n")
	fmt.Printf("    计划请求数:   %d\n", scheduled)
	fmt.Printf("    成功入队数:   %d (%.2f%%)\n", dispatched, dispatchRate)
	fmt.Printf("    队列丢弃数:   %d\n", dropped)
	if len(dropReasons) > 0 {
		printReasonMap("    丢弃原因明细", dropReasons)
	}

	fmt.Printf("\n  请求统计:\n")
	fmt.Printf("    完成请求数:   %d\n", completed)
	fmt.Printf("    成功数:       %d (%.2f%%)\n", success, successRate)
	fmt.Printf("    失败数:       %d\n", fail)
	fmt.Printf("      - 库存不足:  %d\n", soldOut)
	fmt.Printf("      - 用户重复:  %d\n", duplicate)
	fmt.Printf("      - 系统错误:  %d\n", systemErr)
	printReasonMap("    失败原因明细", failReasons)
	printReasonMap("    系统错误细分", systemErrDetails)

	fmt.Printf("\n  库存统计:\n")
	if totalStock > 0 {
		fmt.Printf("    初始库存:     %d\n", totalStock)
		fmt.Printf("    实际售出:     %d\n", actualSold)
		fmt.Printf("    超卖率:       %.2f%%\n", oversellRate)
	} else {
		fmt.Printf("    N/A (gateway mode)\n")
	}

	fmt.Printf("\n  性能指标:\n")
	fmt.Printf("    总耗时:       %.2fs\n", totalDuration.Seconds())
	fmt.Printf("    QPS:          %.2f req/s\n", qps)
	fmt.Printf("    TPS (完成请求): %.2f orders/s\n", tpsCompleted)
	fmt.Printf("    TPS (业务成功): %.2f orders/s\n", tpsBusinessSuccess)
	if seckillPhaseTPS > 0 {
		fmt.Printf("    TPS (秒杀阶段): %.2f orders/s (%.2fs)\n", seckillPhaseTPS, seckillPhaseDuration)
	}

	fmt.Printf("\n  延迟统计 (ms):\n")
	fmt.Printf("    平均延迟:     %d\n", avg)
	fmt.Printf("    P50:          %d\n", p50)
	fmt.Printf("    P95:          %d\n", p95)
	fmt.Printf("    P99:          %d\n", p99)
	if len(latencies) > 0 {
		fmt.Printf("    最小延迟:     %d\n", latencies[0])
		fmt.Printf("    最大延迟:     %d\n", latencies[len(latencies)-1])
	}
	fmt.Println("========================================")
}

func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * float64(p) / 100)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func printReasonMap(title string, m map[string]int64) {
	if len(m) == 0 {
		return
	}
	type item struct {
		k string
		v int64
	}
	items := make([]item, 0, len(m))
	for k, v := range m {
		items = append(items, item{k: k, v: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v == items[j].v {
			return items[i].k < items[j].k
		}
		return items[i].v > items[j].v
	})
	fmt.Printf("%s:\n", title)
	for _, it := range items {
		fmt.Printf("      - %s: %d\n", it.k, it.v)
	}
}

func copyReasonMap(src map[string]int64) map[string]int64 {
	dst := make(map[string]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
