package svc

import (
	"math"
	"sync"
	"time"
)

const (
	defaultBloomExpectedItems            = int64(50000)
	defaultBloomFalsePositiveRate        = 0.001
	defaultBloomNegativeCacheTTL         = int64(5)
	defaultBloomMinBits           uint64 = 64
)

type ProductIDFilterConfig struct {
	Enabled                 bool
	ExpectedItems           int64
	FalsePositiveRate       float64
	NegativeCacheTTLSeconds int64
	FallbackVerifyEnabled   bool
}

type ProductIDFilter struct {
	enabled               bool
	fallbackVerifyEnabled bool
	negativeTTL           time.Duration

	expectedItems     uint64
	falsePositiveRate float64

	filterMu sync.RWMutex
	filter   *bloomBits

	negativeCache sync.Map // key: int64(seckillProductId), value: int64(expireUnixNano)
}

func NewProductIDFilter(cfg ProductIDFilterConfig) *ProductIDFilter {
	expected := cfg.ExpectedItems
	if expected <= 0 {
		expected = defaultBloomExpectedItems
	}
	fpr := cfg.FalsePositiveRate
	if fpr <= 0 || fpr >= 1 {
		fpr = defaultBloomFalsePositiveRate
	}
	negativeTTL := cfg.NegativeCacheTTLSeconds
	if negativeTTL <= 0 {
		negativeTTL = defaultBloomNegativeCacheTTL
	}

	f := &ProductIDFilter{
		enabled:               cfg.Enabled,
		fallbackVerifyEnabled: cfg.FallbackVerifyEnabled,
		negativeTTL:           time.Duration(negativeTTL) * time.Second,
		expectedItems:         uint64(expected),
		falsePositiveRate:     fpr,
	}

	if f.enabled {
		f.filter = newBloomBits(f.expectedItems, f.falsePositiveRate)
	}

	return f
}

func (f *ProductIDFilter) Enabled() bool {
	return f != nil && f.enabled
}

func (f *ProductIDFilter) FallbackVerifyEnabled() bool {
	return f != nil && f.fallbackVerifyEnabled
}

func (f *ProductIDFilter) Rebuild(productIDs []int64) {
	if f == nil || !f.enabled {
		return
	}

	expected := f.expectedItems
	if uint64(len(productIDs)) > expected {
		expected = uint64(len(productIDs))
	}
	next := newBloomBits(expected, f.falsePositiveRate)
	for _, id := range productIDs {
		if id > 0 {
			next.add(id)
		}
	}

	f.filterMu.Lock()
	f.filter = next
	f.filterMu.Unlock()

	f.clearNegativeCache()
}

func (f *ProductIDFilter) Add(productID int64) {
	if f == nil || !f.enabled || productID <= 0 {
		return
	}

	f.filterMu.RLock()
	current := f.filter
	f.filterMu.RUnlock()
	if current == nil {
		return
	}
	current.add(productID)
	f.negativeCache.Delete(productID)
}

func (f *ProductIDFilter) MayContain(productID int64) bool {
	if f == nil || !f.enabled {
		return true
	}
	if productID <= 0 {
		return false
	}

	f.filterMu.RLock()
	current := f.filter
	f.filterMu.RUnlock()
	if current == nil {
		return true
	}
	return current.mayContain(productID)
}

func (f *ProductIDFilter) MarkNotExist(productID int64) {
	if f == nil || !f.enabled || productID <= 0 || f.negativeTTL <= 0 {
		return
	}
	expireAt := time.Now().Add(f.negativeTTL).UnixNano()
	f.negativeCache.Store(productID, expireAt)
}

func (f *ProductIDFilter) IsKnownNotExist(productID int64) bool {
	if f == nil || !f.enabled || productID <= 0 {
		return false
	}

	raw, ok := f.negativeCache.Load(productID)
	if !ok {
		return false
	}
	expireAt, ok := raw.(int64)
	if !ok {
		f.negativeCache.Delete(productID)
		return false
	}
	if time.Now().UnixNano() > expireAt {
		f.negativeCache.Delete(productID)
		return false
	}
	return true
}

func (f *ProductIDFilter) clearNegativeCache() {
	f.negativeCache.Range(func(key, value any) bool {
		f.negativeCache.Delete(key)
		return true
	})
}

type bloomBits struct {
	m uint64
	k uint64

	mu   sync.RWMutex
	bits []uint64
}

func newBloomBits(expected uint64, falsePositiveRate float64) *bloomBits {
	m, k := calcBloomParams(expected, falsePositiveRate)
	return &bloomBits{
		m:    m,
		k:    k,
		bits: make([]uint64, (m+63)/64),
	}
}

func (b *bloomBits) add(v int64) {
	h1, h2 := bloomHashes(uint64(v))

	b.mu.Lock()
	defer b.mu.Unlock()

	for i := uint64(0); i < b.k; i++ {
		idx := (h1 + i*h2) % b.m
		word := idx / 64
		offset := idx % 64
		b.bits[word] |= uint64(1) << offset
	}
}

func (b *bloomBits) mayContain(v int64) bool {
	h1, h2 := bloomHashes(uint64(v))

	b.mu.RLock()
	defer b.mu.RUnlock()

	for i := uint64(0); i < b.k; i++ {
		idx := (h1 + i*h2) % b.m
		word := idx / 64
		offset := idx % 64
		if b.bits[word]&(uint64(1)<<offset) == 0 {
			return false
		}
	}
	return true
}

func calcBloomParams(expected uint64, falsePositiveRate float64) (uint64, uint64) {
	if expected == 0 {
		expected = uint64(defaultBloomExpectedItems)
	}
	if falsePositiveRate <= 0 || falsePositiveRate >= 1 {
		falsePositiveRate = defaultBloomFalsePositiveRate
	}

	mFloat := -float64(expected) * math.Log(falsePositiveRate) / (math.Ln2 * math.Ln2)
	if mFloat < float64(defaultBloomMinBits) {
		mFloat = float64(defaultBloomMinBits)
	}
	m := uint64(math.Ceil(mFloat))

	kFloat := (float64(m) / float64(expected)) * math.Ln2
	k := uint64(math.Ceil(kFloat))
	if k == 0 {
		k = 1
	}

	return m, k
}

func bloomHashes(v uint64) (uint64, uint64) {
	h1 := splitMix64(v + 0x9e3779b97f4a7c15)
	h2 := splitMix64(v + 0x243f6a8885a308d3)
	if h2 == 0 {
		h2 = 0x9e3779b97f4a7c15
	}
	return h1, h2
}

func splitMix64(x uint64) uint64 {
	z := x + 0x9e3779b97f4a7c15
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}
