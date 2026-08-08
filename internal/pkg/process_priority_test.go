package pkg

import "testing"

func TestLowerRecordingProcessPriorityRejectsInvalidPID(t *testing.T) {
	if err := LowerRecordingProcessPriority(0); err == nil {
		t.Fatal("invalid pid must fail")
	}
}
