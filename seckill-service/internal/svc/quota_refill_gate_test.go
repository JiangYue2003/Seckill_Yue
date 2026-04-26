package svc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuotaRefillGate_SameProductSingleExecution(t *testing.T) {
	gate := NewQuotaRefillGate()

	var executed int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := gate.Do(context.Background(), 1001, func() error {
				atomic.AddInt32(&executed, 1)
				time.Sleep(20 * time.Millisecond)
				return nil
			})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&executed); got != 1 {
		t.Fatalf("expected 1 execution, got %d", got)
	}
}

func TestQuotaRefillGate_DifferentProductsParallel(t *testing.T) {
	gate := NewQuotaRefillGate()

	var spid1Running int32
	var spid2Running int32
	var spid1Executed int32
	var spid2Executed int32

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := gate.Do(context.Background(), 2001, func() error {
			atomic.AddInt32(&spid1Executed, 1)
			atomic.StoreInt32(&spid1Running, 1)
			time.Sleep(30 * time.Millisecond)
			atomic.StoreInt32(&spid1Running, 0)
			return nil
		})
		if err != nil {
			t.Errorf("unexpected error for spid1: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := gate.Do(context.Background(), 2002, func() error {
			atomic.AddInt32(&spid2Executed, 1)
			atomic.StoreInt32(&spid2Running, 1)
			time.Sleep(30 * time.Millisecond)
			atomic.StoreInt32(&spid2Running, 0)
			return nil
		})
		if err != nil {
			t.Errorf("unexpected error for spid2: %v", err)
		}
	}()
	close(start)
	wg.Wait()

	if atomic.LoadInt32(&spid1Executed) != 1 || atomic.LoadInt32(&spid2Executed) != 1 {
		t.Fatalf("expected both products executed once, got spid1=%d spid2=%d", spid1Executed, spid2Executed)
	}
}

func TestQuotaRefillGate_ReleaseAfterFailure(t *testing.T) {
	gate := NewQuotaRefillGate()
	var executed int32

	_, err := gate.Do(context.Background(), 3001, func() error {
		atomic.AddInt32(&executed, 1)
		return context.DeadlineExceeded
	})
	if err == nil {
		t.Fatal("expected error on first call")
	}

	_, err = gate.Do(context.Background(), 3001, func() error {
		atomic.AddInt32(&executed, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error on second call, got %v", err)
	}

	if got := atomic.LoadInt32(&executed); got != 2 {
		t.Fatalf("expected 2 executions after failure and retry, got %d", got)
	}
}
