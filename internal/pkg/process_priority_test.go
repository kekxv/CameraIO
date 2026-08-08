package pkg

import "testing"

func TestLowerRecordingProcessPriorityRejectsInvalidPID(t *testing.T) {
	if err := LowerRecordingProcessPriority(0); err == nil {
		t.Fatal("invalid pid must fail")
	}
}

func TestLoweredNiceValueAddsTenWithoutExceedingNineteen(t *testing.T) {
	for _, test := range []struct {
		current int
		want    int
	}{
		{current: 0, want: 10},
		{current: 5, want: 15},
		{current: 15, want: 19},
	} {
		if got := loweredNiceValue(test.current); got != test.want {
			t.Fatalf("loweredNiceValue(%d) = %d, want %d", test.current, got, test.want)
		}
	}
}
