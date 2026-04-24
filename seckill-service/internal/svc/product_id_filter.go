package svc

import (
	"encoding/binary"
	"math"
	"strings"
	"sync"
	"time"

	cuckoo "github.com/seiflotfy/cuckoofilter"
)

const (
	defaultFilterTypeBloom  = "bloom"
	defaultFilterTypeCuckoo = "cuckoo"

	defaultBloomExpectedItems            = int64(50000)
	defaultBloomFalsePositiveRate        = 0.001
	defaultBloomNegativeCacheTTL         = int64(5)
	defaultBloomMinBits           uint64 = 64
)

type ProductIDFilterConfig struct {
	Type                    string
	Enabled                 bool
	ExpectedItems           int64
	FalsePositiveRate       float64
	NegativeCacheTTLSeconds int64
	FallbackVerifyEnabled   bool
}

type ProductIDFilter struct {
	filterType            string
	enabled               bool
	fallbackVerifyEnabled bool
	negativeTTL           time.Duration

	engine productFilterEngine

	negativeCache sync.Map // key: int64(seckillProductId), value: int64(expireUnixNano)
}

type productFilterEngine interface {
	Type() string
	Rebuild(productIDs []int64)
	Add(productID int64)
	MayContain(productID int64) bool
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

	filterType := normalizeFilterType(cfg.Type)
	filter := &ProductIDFilter{
		filterType:            filterType,
		enabled:               cfg.Enabled,
		fallbackVerifyEnabled: cfg.FallbackVerifyEnabled,
		negativeTTL:           time.Duration(negativeTTL) * time.Second,
	}
	if filter.enabled {
		filter.engine = newProductFilterEngine(filterType, uint64(expected), fpr)
		if filter.engine == nil {
			filter.filterType = defaultFilterTypeBloom
			filter.engine = newBloomEngine(uint64(expected), fpr)
		}
	}

	return filter
}

func (f *ProductIDFilter) Enabled() bool {
	return f != nil && f.enabled
}

func (f *ProductIDFilter) Type() string {
	if f == nil {
		return defaultFilterTypeBloom
	}
	return f.filterType
}

func (f *ProductIDFilter) FallbackVerifyEnabled() bool {
	return f != nil && f.fallbackVerifyEnabled
}

func (f *ProductIDFilter) Rebuild(productIDs []int64) {
	if f == nil || !f.enabled || f.engine == nil {
		return
	}

	f.engine.Rebuild(productIDs)
	f.clearNegativeCache()
}

func (f *ProductIDFilter) Add(productID int64) {
	if f == nil || !f.enabled || productID <= 0 || f.engine == nil {
		return
	}

	f.engine.Add(productID)
	f.negativeCache.Delete(productID)
}

func (f *ProductIDFilter) MayContain(productID int64) bool {
	if f == nil || !f.enabled {
		return true
	}
	if productID <= 0 {
		return false
	}
	if f.engine == nil {
		return true
	}

	return f.engine.MayContain(productID)
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

func normalizeFilterType(filterType string) string {
	switch strings.ToLower(strings.TrimSpace(filterType)) {
	case defaultFilterTypeCuckoo:
		return defaultFilterTypeCuckoo
	case defaultFilterTypeBloom, "":
		return defaultFilterTypeBloom
	default:
		return defaultFilterTypeBloom
	}
}

func newProductFilterEngine(filterType string, expected uint64, falsePositiveRate float64) productFilterEngine {
	switch normalizeFilterType(filterType) {
	case defaultFilterTypeCuckoo:
		return newCuckooEngine()
	default:
		return newBloomEngine(expected, falsePositiveRate)
	}
}

type bloomEngine struct {
	expectedItems     uint64
	falsePositiveRate float64

	mu     sync.RWMutex
	filter *bloomBits
}

func newBloomEngine(expected uint64, falsePositiveRate float64) *bloomEngine {
	if expected == 0 {
		expected = uint64(defaultBloomExpectedItems)
	}
	if falsePositiveRate <= 0 || falsePositiveRate >= 1 {
		falsePositiveRate = defaultBloomFalsePositiveRate
	}
	return &bloomEngine{
		expectedItems:     expected,
		falsePositiveRate: falsePositiveRate,
		filter:            newBloomBits(expected, falsePositiveRate),
	}
}

func (b *bloomEngine) Type() string {
	return defaultFilterTypeBloom
}

func (b *bloomEngine) Rebuild(productIDs []int64) {
	expected := b.expectedItems
	if uint64(len(productIDs)) > expected {
		expected = uint64(len(productIDs))
	}

	next := newBloomBits(expected, b.falsePositiveRate)
	for _, id := range productIDs {
		if id > 0 {
			next.add(id)
		}
	}

	b.mu.Lock()
	b.filter = next
	b.mu.Unlock()
}

func (b *bloomEngine) Add(productID int64) {
	if productID <= 0 {
		return
	}

	b.mu.RLock()
	current := b.filter
	b.mu.RUnlock()
	if current == nil {
		return
	}
	current.add(productID)
}

func (b *bloomEngine) MayContain(productID int64) bool {
	if productID <= 0 {
		return false
	}

	b.mu.RLock()
	current := b.filter
	b.mu.RUnlock()
	if current == nil {
		return true
	}
	return current.mayContain(productID)
}

type cuckooEngine struct {
	mu     sync.RWMutex
	filter *cuckoo.ScalableCuckooFilter
}

func newCuckooEngine() *cuckooEngine {
	return &cuckooEngine{filter: cuckoo.NewScalableCuckooFilter()}
}

func (c *cuckooEngine) Type() string {
	return defaultFilterTypeCuckoo
}

func (c *cuckooEngine) Rebuild(productIDs []int64) {
	next := cuckoo.NewScalableCuckooFilter()
	for _, id := range productIDs {
		if id <= 0 {
			continue
		}
		key := toFilterKey(id)
		next.InsertUnique(key[:])
	}

	c.mu.Lock()
	c.filter = next
	c.mu.Unlock()
}

func (c *cuckooEngine) Add(productID int64) {
	if productID <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	current := c.filter
	if current == nil {
		return
	}

	key := toFilterKey(productID)
	current.InsertUnique(key[:])
}

func (c *cuckooEngine) MayContain(productID int64) bool {
	if productID <= 0 {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	current := c.filter
	if current == nil {
		return true
	}

	key := toFilterKey(productID)
	return current.Lookup(key[:])
}

func toFilterKey(v int64) [8]byte {
	var key [8]byte
	binary.LittleEndian.PutUint64(key[:], uint64(v))
	return key
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
