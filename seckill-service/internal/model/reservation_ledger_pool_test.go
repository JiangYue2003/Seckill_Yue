package model

import (
	"testing"
	"time"

	"seckill-mall/seckill-service/internal/config"
)

func TestNormalizeMySQLPoolConfigUsesSafeDefaultsForReplicaDeployment(t *testing.T) {
	cfg := config.Config{}

	pool := normalizeMySQLPoolConfig(cfg)

	if pool.MaxIdleConns != 12 {
		t.Fatalf("MaxIdleConns = %d, want 12", pool.MaxIdleConns)
	}
	if pool.MaxOpenConns != 48 {
		t.Fatalf("MaxOpenConns = %d, want 48", pool.MaxOpenConns)
	}
	if pool.ConnMaxLifetime != time.Hour {
		t.Fatalf("ConnMaxLifetime = %v, want %v", pool.ConnMaxLifetime, time.Hour)
	}
}

func TestNormalizeMySQLPoolConfigRespectsExplicitValues(t *testing.T) {
	cfg := config.Config{}
	cfg.MySQL.MaxIdleConns = 18
	cfg.MySQL.MaxOpenConns = 36
	cfg.MySQL.ConnMaxLifetimeSeconds = 1800

	pool := normalizeMySQLPoolConfig(cfg)

	if pool.MaxIdleConns != 18 {
		t.Fatalf("MaxIdleConns = %d, want 18", pool.MaxIdleConns)
	}
	if pool.MaxOpenConns != 36 {
		t.Fatalf("MaxOpenConns = %d, want 36", pool.MaxOpenConns)
	}
	if pool.ConnMaxLifetime != 30*time.Minute {
		t.Fatalf("ConnMaxLifetime = %v, want %v", pool.ConnMaxLifetime, 30*time.Minute)
	}
}

func TestNormalizeMySQLPoolConfigClampsIdleToOpenConns(t *testing.T) {
	cfg := config.Config{}
	cfg.MySQL.MaxIdleConns = 32
	cfg.MySQL.MaxOpenConns = 16

	pool := normalizeMySQLPoolConfig(cfg)

	if pool.MaxIdleConns != 16 {
		t.Fatalf("MaxIdleConns = %d, want 16", pool.MaxIdleConns)
	}
	if pool.MaxOpenConns != 16 {
		t.Fatalf("MaxOpenConns = %d, want 16", pool.MaxOpenConns)
	}
}
