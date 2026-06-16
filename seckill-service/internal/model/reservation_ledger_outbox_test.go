package model

import (
	"context"
	"testing"

	"seckill-mall/seckill-service/internal/model/entity"
)

type recordingReservationLedger struct {
	lastPersist *PersistReservationInput
}

func TestTrimOutboxError(t *testing.T) {
	short := trimOutboxError("short")
	if short != "short" {
		t.Fatalf("expected short string unchanged, got %q", short)
	}
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'x'
	}
	got := trimOutboxError(string(long))
	if len(got) != 512 {
		t.Fatalf("expected trimmed length 512, got %d", len(got))
	}
}

func TestCanAdvanceReservationStatus(t *testing.T) {
	if !canAdvanceReservationStatus(entity.ReservationStatusPaying, entity.ReservationStatusConsumed, false) {
		t.Fatal("expected forward advancement to be allowed")
	}
	if canAdvanceReservationStatus(entity.ReservationStatusConsumed, entity.ReservationStatusPaying, false) {
		t.Fatal("expected backward advancement to be rejected")
	}
	if !canAdvanceReservationStatus(entity.ReservationStatusFailed, entity.ReservationStatusConsumed, true) {
		t.Fatal("expected failed->consumed recovery to be allowed when allowRecover=true")
	}
}

func TestOutboxStoreNilSafety(t *testing.T) {
	store := NewOutboxStore(nil)
	if _, err := store.ClaimPending(context.Background(), 1); err == nil {
		t.Fatal("expected error when db is nil")
	}
}
