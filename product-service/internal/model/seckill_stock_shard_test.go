package model

import "testing"

func TestSplitStockIntoShardsPreservesTotal(t *testing.T) {
	shards := splitStockIntoShards(33)
	if len(shards) != 16 {
		t.Fatalf("expected 16 shards, got %d", len(shards))
	}

	total := 0
	for _, shardStock := range shards {
		total += shardStock
	}
	if total != 33 {
		t.Fatalf("expected total 33, got %d", total)
	}
}

func TestSplitStockIntoShardsDistributesRemainderToLeadingShards(t *testing.T) {
	shards := splitStockIntoShards(18)
	if shards[0] != 2 || shards[1] != 2 {
		t.Fatalf("expected leading shards to absorb remainder, got %+v", shards[:4])
	}
	if shards[15] != 1 {
		t.Fatalf("expected tail shard to remain base value, got %d", shards[15])
	}
}
