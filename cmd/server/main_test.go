package main

import (
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"
)

type startupRecorderFake struct {
	calls                 *[]string
	reconcileErr          error
	reconcileErrors       []error
	reconcileCalls        int
	startRetentionEntered chan struct{}
	startRetentionRelease chan struct{}
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
	if f.startRetentionEntered != nil {
		close(f.startRetentionEntered)
	}
	if f.startRetentionRelease != nil {
		<-f.startRetentionRelease
	}
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

func waitForRecordingStartupStopRequest(t *testing.T, startup *recordingStartupCoordinator) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for !startup.stopRequested.Load() {
		select {
		case <-deadline.C:
			t.Fatal("recording startup cancellation was not requested")
		default:
			runtime.Gosched()
		}
	}
}

func TestStartRecordingSubsystemsReconcilesBeforeScheduler(t *testing.T) {
	var calls []string
	recorder := &startupRecorderFake{calls: &calls}
	scheduler := &startupSchedulerFake{calls: &calls}

	startup := startRecordingSubsystems(recorder, scheduler, time.Millisecond, nil)
	select {
	case <-startup.done:
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
	startup := startRecordingSubsystems(recorder, scheduler, time.Millisecond, func(err error) { reported <- err })
	select {
	case err := <-reported:
		if !errors.Is(err, probeUnavailable) {
			t.Fatalf("reported error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initial reconciliation failure was not reported")
	}
	select {
	case <-startup.done:
	case <-time.After(time.Second):
		t.Fatal("recording startup did not retry")
	}
	want := []string{"reconcile-segments", "reconcile-segments", "reconcile-legacy", "retention-once", "start-retention", "start-sweep", "start-scheduler"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("startup calls = %v, want %v", calls, want)
	}
}

func TestRecordingStartupShutdownWaitsForCoordinatorAndPreventsLaterStarts(t *testing.T) {
	var calls []string
	retentionEntered := make(chan struct{})
	retentionRelease := make(chan struct{})
	recorder := &startupRecorderFake{
		calls:                 &calls,
		startRetentionEntered: retentionEntered,
		startRetentionRelease: retentionRelease,
	}
	scheduler := &startupSchedulerFake{calls: &calls}
	startup := startRecordingSubsystems(recorder, scheduler, time.Millisecond, nil)

	select {
	case <-retentionEntered:
	case <-time.After(time.Second):
		t.Fatal("recording startup did not reach retention start")
	}

	shutdownStarted := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		close(shutdownStarted)
		cancelAndWaitRecordingStartup(startup)
		calls = append(calls, "stop-scheduler", "stop-recorder")
		close(shutdownDone)
	}()

	<-shutdownStarted
	waitForRecordingStartupStopRequest(t, startup)
	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned while recording startup was still running")
	default:
	}

	close(retentionRelease)
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not return after recording startup completed")
	}

	want := []string{
		"reconcile-segments",
		"reconcile-legacy",
		"retention-once",
		"start-retention",
		"stop-scheduler",
		"stop-recorder",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("shutdown calls = %v, want %v", calls, want)
	}
}

func TestRecordingStartupCoordinatorSerializesCancellationWithActivation(t *testing.T) {
	startup := newRecordingStartupCoordinator()
	activationEntered := make(chan struct{})
	activationRelease := make(chan struct{})
	activationDone := make(chan struct{})
	go func() {
		startup.activate(func() {
			close(activationEntered)
			<-activationRelease
		})
		close(activationDone)
	}()

	<-activationEntered
	cancelDone := make(chan struct{})
	go func() {
		startup.cancel()
		close(cancelDone)
	}()
	waitForRecordingStartupStopRequest(t, startup)
	select {
	case <-cancelDone:
		t.Fatal("cancellation interleaved between activation authorization and start completion")
	default:
	}

	close(activationRelease)
	select {
	case <-activationDone:
	case <-time.After(time.Second):
		t.Fatal("authorized activation did not complete")
	}
	select {
	case <-cancelDone:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not complete after activation")
	}

	startedAfterCancel := false
	if startup.activate(func() { startedAfterCancel = true }) {
		t.Fatal("activation was authorized after cancellation")
	}
	if startedAfterCancel {
		t.Fatal("start callback ran after cancellation")
	}
}
