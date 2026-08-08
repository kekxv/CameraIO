package pkg

import "testing"

func TestDefaultConfigUsesSingleHostSafeRecordingDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RecordingSegmentSeconds != 300 {
		t.Fatalf("segment seconds = %d", cfg.RecordingSegmentSeconds)
	}
	if cfg.RecordingCleanupFreePercent != 15 {
		t.Fatalf("cleanup percent = %d", cfg.RecordingCleanupFreePercent)
	}
	if cfg.RecordingStopFreePercent != 5 {
		t.Fatalf("stop percent = %d", cfg.RecordingStopFreePercent)
	}
}

func TestRecordingEnvironmentOverrides(t *testing.T) {
	t.Setenv("CAMERAIO_RECORDING_SEGMENT_SECONDS", "600")
	t.Setenv("CAMERAIO_RECORDING_RETENTION_DAYS", "14")
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	if cfg.RecordingSegmentSeconds != 600 || cfg.RecordingRetentionDays != 14 {
		t.Fatalf("recording overrides not applied: %+v", cfg)
	}
}
