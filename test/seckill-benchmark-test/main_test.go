package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	seckillpb "seckill-mall/common/seckill"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestParseTargetsTrimsAndSkipsEmpty(t *testing.T) {
	got := parseTargets(" 127.0.0.1:9083, ,127.0.0.1:19083 ")

	if len(got) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(got))
	}
	if got[0] != "127.0.0.1:9083" {
		t.Fatalf("unexpected first target: %q", got[0])
	}
	if got[1] != "127.0.0.1:19083" {
		t.Fatalf("unexpected second target: %q", got[1])
	}
}

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "default empty", in: "", want: benchmarkModeLegacy},
		{name: "legacy", in: "legacy", want: benchmarkModeLegacy},
		{name: "burst", in: "burst", want: benchmarkModeBurst},
		{name: "uppercase", in: "BURST", want: benchmarkModeBurst},
		{name: "invalid", in: "foo", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeMode(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeMode(%q) returned error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveExpectedRequestsLegacy(t *testing.T) {
	prevTotal := *totalRequests
	prevConcurrency := *concurrency
	t.Cleanup(func() {
		*totalRequests = prevTotal
		*concurrency = prevConcurrency
	})

	*totalRequests = 1234
	*concurrency = 32

	got, err := resolveExpectedRequests(benchmarkModeLegacy)
	if err != nil {
		t.Fatalf("resolveExpectedRequests(legacy) returned error: %v", err)
	}
	if got != 1234 {
		t.Fatalf("resolveExpectedRequests(legacy) = %d, want 1234", got)
	}
}

func TestResolveExpectedRequestsBurstUsesDispatchWindow(t *testing.T) {
	prevRate := *rps
	prevDuration := *duration
	prevBurstWindow := *burstWindow
	t.Cleanup(func() {
		*rps = prevRate
		*duration = prevDuration
		*burstWindow = prevBurstWindow
	})

	*rps = 200
	*duration = 10 * time.Second
	*burstWindow = 1500 * time.Millisecond

	got, err := resolveExpectedRequests(benchmarkModeBurst)
	if err != nil {
		t.Fatalf("resolveExpectedRequests(burst) returned error: %v", err)
	}
	if got != 300 {
		t.Fatalf("resolveExpectedRequests(burst) = %d, want 300", got)
	}
}

func TestClassifySeckillResponseSuccess(t *testing.T) {
	got := classifySeckillResponse(&seckillpb.SeckillResponse{
		Success: true,
		Code:    "SUCCESS",
		Message: "抢购成功，订单正在处理中",
		OrderId: "S123",
	}, nil)

	if !got.success {
		t.Fatalf("expected success result, got %#v", got)
	}
	if got.bizCode != "SUCCESS" {
		t.Fatalf("expected bizCode SUCCESS, got %q", got.bizCode)
	}
	if got.systemErr {
		t.Fatalf("expected no system error, got %#v", got)
	}
}

func TestClassifySeckillResponseSoldOut(t *testing.T) {
	got := classifySeckillResponse(&seckillpb.SeckillResponse{
		Success: false,
		Code:    "SOLD_OUT",
		Message: "商品已售罄",
	}, nil)

	if got.success {
		t.Fatalf("expected failed result, got %#v", got)
	}
	if got.bizCode != "SOLD_OUT" {
		t.Fatalf("expected bizCode SOLD_OUT, got %q", got.bizCode)
	}
	if got.failReason != "RESP_SOLD_OUT" {
		t.Fatalf("expected failReason RESP_SOLD_OUT, got %q", got.failReason)
	}
	if got.systemErr {
		t.Fatalf("expected SOLD_OUT not to be treated as system error, got %#v", got)
	}
}

func TestClassifySeckillResponseRPCError(t *testing.T) {
	got := classifySeckillResponse(nil, status.Error(codes.Unavailable, "transport is closing"))

	if !got.systemErr {
		t.Fatalf("expected system error, got %#v", got)
	}
	if got.systemErrCode != "ERR_RPC_UNAVAILABLE" {
		t.Fatalf("expected ERR_RPC_UNAVAILABLE, got %q", got.systemErrCode)
	}
}

func TestClassifyRPCErrorContextDeadlineExceeded(t *testing.T) {
	got := classifyRPCError(context.DeadlineExceeded)
	if got != "ERR_CONTEXT_DEADLINE_EXCEEDED" {
		t.Fatalf("expected ERR_CONTEXT_DEADLINE_EXCEEDED, got %q", got)
	}
}

func TestClassifyRPCErrorCanceled(t *testing.T) {
	got := classifyRPCError(context.Canceled)
	if got != "ERR_CONTEXT_CANCELED" {
		t.Fatalf("expected ERR_CONTEXT_CANCELED, got %q", got)
	}
}

func TestClassifyRPCErrorUnknown(t *testing.T) {
	got := classifyRPCError(errors.New("boom"))
	if got != "ERR_LOCAL" {
		t.Fatalf("expected ERR_LOCAL, got %q", got)
	}
}

func TestMetricsReportRemovesGatewayLabel(t *testing.T) {
	metrics := NewMetrics()
	metrics.IncScheduled()
	metrics.IncDispatched()
	metrics.RecordResult(12, result{success: true, bizCode: "SUCCESS"})

	output := captureStdout(t, func() {
		metrics.Report(time.Second, 10, 1)
	})

	if !strings.Contains(output, "同步入口性能报告") {
		t.Fatalf("expected report title in output, got %q", output)
	}
	if strings.Contains(output, "Gateway Open-Loop") {
		t.Fatalf("expected report title without gateway label, got %q", output)
	}
}

func TestSplitStockIntoShardsPreservesTotal(t *testing.T) {
	shards := splitStockIntoShards(33)
	if len(shards) != defaultShardCount {
		t.Fatalf("expected %d shards, got %d", defaultShardCount, len(shards))
	}

	var sum int64
	for _, stock := range shards {
		sum += stock
	}
	if sum != 33 {
		t.Fatalf("expected total 33, got %d", sum)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	os.Stdout = w
	defer func() {
		os.Stdout = original
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}

	return string(out)
}
