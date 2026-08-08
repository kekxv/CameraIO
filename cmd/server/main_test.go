package main

import (
	"errors"
	"reflect"
	"testing"
)

type startupRecorderFake struct {
	calls        *[]string
	reconcileErr error
}

func (f *startupRecorderFake) ReconcileSegments() error {
	*f.calls = append(*f.calls, "reconcile-segments")
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

	if err := startRecordingSubsystems(recorder, scheduler); err != nil {
		t.Fatalf("startRecordingSubsystems: %v", err)
	}
	want := []string{"reconcile-segments", "reconcile-legacy", "retention-once", "start-retention", "start-sweep", "start-scheduler"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("startup calls = %v, want %v", calls, want)
	}
}

func TestStartRecordingSubsystemsDoesNotStartSchedulerAfterReconcileFailure(t *testing.T) {
	var calls []string
	recorder := &startupRecorderFake{calls: &calls, reconcileErr: errors.New("recording root unavailable")}
	scheduler := &startupSchedulerFake{calls: &calls}

	if err := startRecordingSubsystems(recorder, scheduler); err == nil {
		t.Fatal("startRecordingSubsystems succeeded after reconciliation failure")
	}
	want := []string{"reconcile-segments"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("startup calls = %v, want %v", calls, want)
	}
}
