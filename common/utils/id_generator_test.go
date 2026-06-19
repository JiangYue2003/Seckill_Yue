package utils

import "testing"

func TestInitSnowflakeSetsConfiguredWorkerID(t *testing.T) {
	resetGlobalSnowflakeForTest()

	if err := InitSnowflake(7); err != nil {
		t.Fatalf("InitSnowflake returned error: %v", err)
	}

	sf := GetSnowflake()
	_, workerID, _ := sf.ParseId(sf.NextId())
	if workerID != 7 {
		t.Fatalf("expected workerID 7, got %d", workerID)
	}
}

func TestInitSnowflakeRejectsDifferentWorkerIDReconfigure(t *testing.T) {
	resetGlobalSnowflakeForTest()

	if err := InitSnowflake(7); err != nil {
		t.Fatalf("InitSnowflake returned error: %v", err)
	}
	if err := InitSnowflake(8); err == nil {
		t.Fatal("expected reconfigure with different workerID to fail")
	}
}

func TestWorkerIDFromPortUsesStableDistinctValues(t *testing.T) {
	first, err := WorkerIDFromPort(9083)
	if err != nil {
		t.Fatalf("WorkerIDFromPort returned error: %v", err)
	}
	second, err := WorkerIDFromPort(19083)
	if err != nil {
		t.Fatalf("WorkerIDFromPort returned error: %v", err)
	}

	if first == second {
		t.Fatalf("expected distinct worker ids for replica ports, got %d", first)
	}
	if first < 0 || first > maxWorkerId {
		t.Fatalf("first worker id out of range: %d", first)
	}
	if second < 0 || second > maxWorkerId {
		t.Fatalf("second worker id out of range: %d", second)
	}
}

func resetGlobalSnowflakeForTest() {
	snowflakeMu.Lock()
	defer snowflakeMu.Unlock()
	globalSnowflake = nil
}
