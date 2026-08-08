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

func TestRecordingConfigClampsBoundaryOverrides(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		segment int
		retain  int
		cleanup int
		stop    int
	}{
		{"segment lower bound", "CAMERAIO_RECORDING_SEGMENT_SECONDS", "1", 60, 30, 15, 5},
		{"segment upper bound", "CAMERAIO_RECORDING_SEGMENT_SECONDS", "1801", 1800, 30, 15, 5},
		{"retention lower bound", "CAMERAIO_RECORDING_RETENTION_DAYS", "0", 300, 1, 15, 5},
		{"retention upper bound", "CAMERAIO_RECORDING_RETENTION_DAYS", "3651", 300, 3650, 15, 5},
		{"cleanup lower bound restores safe watermarks", "CAMERAIO_RECORDING_CLEANUP_FREE_PERCENT", "0", 300, 30, 15, 5},
		{"cleanup upper bound", "CAMERAIO_RECORDING_CLEANUP_FREE_PERCENT", "100", 300, 30, 99, 5},
		{"stop lower bound", "CAMERAIO_RECORDING_STOP_FREE_PERCENT", "0", 300, 30, 15, 1},
		{"stop upper bound restores safe watermarks", "CAMERAIO_RECORDING_STOP_FREE_PERCENT", "100", 300, 30, 15, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			cfg := DefaultConfig()
			applyEnvOverrides(cfg)
			if cfg.RecordingSegmentSeconds != tt.segment ||
				cfg.RecordingRetentionDays != tt.retain ||
				cfg.RecordingCleanupFreePercent != tt.cleanup ||
				cfg.RecordingStopFreePercent != tt.stop {
				t.Fatalf("recording config = %+v", cfg)
			}
		})
	}
}

func TestRecordingConfigRestoresSafeWatermarksWhenStopIsNotBelowCleanup(t *testing.T) {
	t.Setenv("CAMERAIO_RECORDING_CLEANUP_FREE_PERCENT", "40")
	t.Setenv("CAMERAIO_RECORDING_STOP_FREE_PERCENT", "40")
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	if cfg.RecordingCleanupFreePercent != 15 || cfg.RecordingStopFreePercent != 5 {
		t.Fatalf("unsafe watermarks = cleanup %d, stop %d", cfg.RecordingCleanupFreePercent, cfg.RecordingStopFreePercent)
	}
}
