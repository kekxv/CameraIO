package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type startupRecorderFake struct {
	calls           *[]string
	reconcileErr    error
	reconcileErrors []error
	reconcileCalls  int
}

func (f *startupRecorderFake) ReconcileSegments() error {
	*f.calls = append(*f.calls, "reconcile-segments")
	if f.reconcileCalls < len(f.reconcileErrors) {
		err := f.reconcileErrors[f.reconcileCalls]
		f.reconcileCalls++
		return err
	}
	return f.reconcileErr
}

func (f *startupRecorderFake) ReconcileLegacyOrphaned() {
	*f.calls = append(*f.calls, "reconcile-legacy")
}

func (f *startupRecorderFake) RunRetentionOnce() error {
	*f.calls = append(*f.calls, "retention-once")
	return nil
}

func (f *startupRecorderFake) StartRetention() {
	*f.calls = append(*f.calls, "start-retention")
}

func (f *startupRecorderFake) StartSweep() {
	*f.calls = append(*f.calls, "start-sweep")
}

type startupSchedulerFake struct {
	calls *[]string
}

func (f *startupSchedulerFake) Start() {
	*f.calls = append(*f.calls, "start-scheduler")
}

func TestStartRecordingSubsystemsReconcilesBeforeScheduler(t *testing.T) {
	var calls []string
	recorder := &startupRecorderFake{calls: &calls}
	scheduler := &startupSchedulerFake{calls: &calls}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startRecordingSubsystems(ctx, recorder, scheduler, time.Millisecond, nil)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recording startup did not complete")
	}
	want := []string{"reconcile-segments", "reconcile-legacy", "retention-once", "start-retention", "start-sweep", "start-scheduler"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("startup calls = %v, want %v", calls, want)
	}
}

func TestStartRecordingSubsystemsRetriesReconciliationBeforeStartingScheduler(t *testing.T) {
	var calls []string
	probeUnavailable := errors.New("ffprobe downloading")
	recorder := &startupRecorderFake{calls: &calls, reconcileErrors: []error{probeUnavailable, nil}}
	scheduler := &startupSchedulerFake{calls: &calls}
	reported := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startRecordingSubsystems(ctx, recorder, scheduler, time.Millisecond, func(err error) { reported <- err })
	select {
	case err := <-reported:
		if !errors.Is(err, probeUnavailable) {
			t.Fatalf("reported error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initial reconciliation failure was not reported")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recording startup did not retry")
	}
	want := []string{"reconcile-segments", "reconcile-segments", "reconcile-legacy", "retention-once", "start-retention", "start-sweep", "start-scheduler"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("startup calls = %v, want %v", calls, want)
	}
}
