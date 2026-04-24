package svc

import (
	"testing"
	"time"
)

func TestProductIDFilterRebuildAndAdd(t *testing.T) {
	filter := NewProductIDFilter(ProductIDFilterConfig{
		Enabled:                 true,
		ExpectedItems:           1024,
		FalsePositiveRate:       0.000001,
		NegativeCacheTTLSeconds: 1,
		FallbackVerifyEnabled:   true,
	})

	filter.Rebuild([]int64{1001, 1002, 1003})
	if !filter.MayContain(1001) {
		t.Fatalf("expected product 1001 to be present after rebuild")
	}
	if !filter.MayContain(1002) {
		t.Fatalf("expected product 1002 to be present after rebuild")
	}

	filter.Add(2001)
	if !filter.MayContain(2001) {
		t.Fatalf("expected product 2001 to be present after add")
	}
}

func TestProductIDFilterNegativeCacheTTL(t *testing.T) {
	filter := NewProductIDFilter(ProductIDFilterConfig{
		Enabled:                 true,
		ExpectedItems:           128,
		FalsePositiveRate:       0.001,
		NegativeCacheTTLSeconds: 1,
		FallbackVerifyEnabled:   true,
	})

	filter.MarkNotExist(9999)
	if !filter.IsKnownNotExist(9999) {
		t.Fatalf("expected product 9999 to be in negative cache")
	}

	time.Sleep(1100 * time.Millisecond)
	if filter.IsKnownNotExist(9999) {
		t.Fatalf("expected product 9999 negative cache to expire")
	}
}

func TestProductIDFilterDisabledBypass(t *testing.T) {
	filter := NewProductIDFilter(ProductIDFilterConfig{
		Enabled: false,
	})

	if !filter.MayContain(12345) {
		t.Fatalf("disabled filter should bypass and allow requests")
	}
}

func TestProductIDFilterCuckooRebuildAndAdd(t *testing.T) {
	filter := NewProductIDFilter(ProductIDFilterConfig{
		Type:                  "cuckoo",
		Enabled:               true,
		FallbackVerifyEnabled: true,
	})

	filter.Rebuild([]int64{3001, 3002, 3003})
	if !filter.MayContain(3001) {
		t.Fatalf("expected product 3001 to be present after cuckoo rebuild")
	}

	filter.Add(4001)
	if !filter.MayContain(4001) {
		t.Fatalf("expected product 4001 to be present after cuckoo add")
	}
}

func TestProductIDFilterInvalidTypeFallbackToBloom(t *testing.T) {
	filter := NewProductIDFilter(ProductIDFilterConfig{
		Type:              "unknown-filter",
		Enabled:           true,
		ExpectedItems:     128,
		FalsePositiveRate: 0.001,
	})

	if filter.Type() != "bloom" {
		t.Fatalf("expected fallback type bloom, got %s", filter.Type())
	}
}
