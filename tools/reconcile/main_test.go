package main

import "testing"

func TestBuildRedisOrderKeysPrefersNewClusterKey(t *testing.T) {
	keys := buildRedisOrderKeys("S105_S50001")
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d (%v)", len(keys), keys)
	}
	if keys[0] != "{105}:sk:order:S105_S50001" {
		t.Fatalf("unexpected primary key: %s", keys[0])
	}
	if keys[1] != "seckill:order:S105_S50001" {
		t.Fatalf("unexpected fallback key: %s", keys[1])
	}
}

func TestBuildRedisOrderKeysFallsBackForLegacyOrderID(t *testing.T) {
	keys := buildRedisOrderKeys("S50001")
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d (%v)", len(keys), keys)
	}
	if keys[0] != "seckill:order:S50001" {
		t.Fatalf("unexpected legacy key: %s", keys[0])
	}
}
