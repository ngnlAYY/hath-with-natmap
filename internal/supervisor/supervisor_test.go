package supervisor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
)

type fakeHath struct {
	mu              sync.Mutex
	running         bool
	starts          []int
	stops           int
	stopCalls       int
	stopHadDeadline bool
	errStart        error
	errStop         error
	events          *[]string
}

func (f *fakeHath) Start(ctx context.Context, port int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events != nil {
		*f.events = append(*f.events, "start")
	}
	if f.errStart != nil {
		return f.errStart
	}
	f.running = true
	f.starts = append(f.starts, port)
	return nil
}

func (f *fakeHath) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	_, f.stopHadDeadline = ctx.Deadline()
	if f.events != nil {
		*f.events = append(*f.events, "stop")
	}
	if f.errStop != nil {
		return f.errStop
	}
	if f.running {
		f.stops++
	}
	f.running = false
	return nil
}

func (f *fakeHath) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func (f *fakeHath) startPorts() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.starts...)
}

func (f *fakeHath) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops
}

func (f *fakeHath) totalStopCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCalls
}

func (f *fakeHath) stoppedWithDeadline() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopHadDeadline
}

type fakeUpdater struct {
	mu        sync.Mutex
	ports     []int
	errUpdate error
	events    *[]string
}

func (f *fakeUpdater) UpdatePort(ctx context.Context, port int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events != nil {
		*f.events = append(*f.events, "update")
	}
	if f.errUpdate != nil {
		return f.errUpdate
	}
	f.ports = append(f.ports, port)
	return nil
}

func (f *fakeUpdater) updatedPorts() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.ports...)
}

type fakeNatmapProcess struct {
	mu              sync.Mutex
	starts          int
	stops           int
	stopHadDeadline bool
	done            chan error
	err             error
}

func newFakeNatmapProcess() *fakeNatmapProcess {
	return &fakeNatmapProcess{done: make(chan error, 1)}
}

func (f *fakeNatmapProcess) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	return f.err
}

func (f *fakeNatmapProcess) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	_, f.stopHadDeadline = ctx.Deadline()
	return nil
}

func (f *fakeNatmapProcess) Done() <-chan error {
	return f.done
}

func (f *fakeNatmapProcess) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}

func (f *fakeNatmapProcess) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops
}

func (f *fakeNatmapProcess) stoppedWithDeadline() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopHadDeadline
}

func TestRuntimeRunHandlesMappingThroughCoordinator(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan natmap.Mapping)
	natmapProc := newFakeNatmapProcess()
	hath := &fakeHath{}
	updater := &fakeUpdater{}
	runtime := Runtime{
		Natmap:     natmapProc,
		Hath:       hath,
		Updater:    updater,
		Events:     events,
		BindPort:   7000,
		RetryDelay: time.Millisecond,
	}
	runDone := make(chan error, 1)
	go func() {
		runDone <- runtime.Run(ctx)
	}()

	waitUntil(t, func() bool { return natmapProc.startCount() == 1 })
	events <- testMapping("203.0.113.10", 50000, 7000)
	waitUntil(t, func() bool { return len(updater.updatedPorts()) == 1 && len(hath.startPorts()) == 1 })

	cancel()
	if err := waitForRun(t, runDone); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertInts(t, updater.updatedPorts(), []int{50000})
	assertInts(t, hath.startPorts(), []int{7000})
	if natmapProc.stopCount() == 0 {
		t.Fatal("natmap Stop was not called")
	}
	if hath.totalStopCalls() == 0 {
		t.Fatal("hath Stop was not called")
	}
}

func TestRuntimeRunRetriesNatmapStartFailureUntilContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	natmapProc := newFakeNatmapProcess()
	natmapProc.err = errors.New("start failed")
	runtime := Runtime{
		Natmap:     natmapProc,
		Hath:       &fakeHath{},
		Updater:    &fakeUpdater{},
		Events:     make(chan natmap.Mapping),
		BindPort:   7000,
		RetryDelay: time.Millisecond,
	}
	runDone := make(chan error, 1)
	go func() {
		runDone <- runtime.Run(ctx)
	}()

	waitUntil(t, func() bool { return natmapProc.startCount() >= 2 })
	cancel()
	if err := waitForRun(t, runDone); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRuntimeRunUsesRestartDelayAfterNatmapExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	natmapProc := newFakeNatmapProcess()
	runtime := Runtime{
		Natmap:       natmapProc,
		Hath:         &fakeHath{},
		Updater:      &fakeUpdater{},
		Events:       make(chan natmap.Mapping),
		BindPort:     7000,
		RetryDelay:   time.Millisecond,
		RestartDelay: 50 * time.Millisecond,
	}
	runDone := make(chan error, 1)
	go func() {
		runDone <- runtime.Run(ctx)
	}()

	waitUntil(t, func() bool { return natmapProc.startCount() == 1 })
	natmapProc.done <- errors.New("natmap exited")
	time.Sleep(10 * time.Millisecond)
	if natmapProc.startCount() != 1 {
		t.Fatalf("natmap restarted before RestartDelay elapsed; starts = %d", natmapProc.startCount())
	}
	waitUntil(t, func() bool { return natmapProc.startCount() >= 2 })

	cancel()
	if err := waitForRun(t, runDone); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRuntimeRunUsesShutdownTimeoutWhenContextCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	natmapProc := newFakeNatmapProcess()
	hath := &fakeHath{running: true}
	runtime := Runtime{
		Natmap:          natmapProc,
		Hath:            hath,
		Updater:         &fakeUpdater{},
		Events:          make(chan natmap.Mapping),
		BindPort:        7000,
		RetryDelay:      time.Millisecond,
		ShutdownTimeout: time.Second,
	}
	runDone := make(chan error, 1)
	go func() {
		runDone <- runtime.Run(ctx)
	}()

	waitUntil(t, func() bool { return natmapProc.startCount() == 1 })
	cancel()
	if err := waitForRun(t, runDone); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !natmapProc.stoppedWithDeadline() {
		t.Fatal("natmap Stop did not receive a deadline context")
	}
	if !hath.stoppedWithDeadline() {
		t.Fatal("hath Stop did not receive a deadline context")
	}
}

func TestRuntimeRunStopsProcessesOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	natmapProc := newFakeNatmapProcess()
	hath := &fakeHath{running: true}
	runtime := Runtime{
		Natmap:     natmapProc,
		Hath:       hath,
		Updater:    &fakeUpdater{},
		Events:     make(chan natmap.Mapping),
		BindPort:   7000,
		RetryDelay: time.Millisecond,
	}
	runDone := make(chan error, 1)
	go func() {
		runDone <- runtime.Run(ctx)
	}()

	waitUntil(t, func() bool { return natmapProc.startCount() == 1 })
	cancel()
	if err := waitForRun(t, runDone); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if natmapProc.stopCount() == 0 {
		t.Fatal("natmap Stop was not called")
	}
	if hath.totalStopCalls() == 0 {
		t.Fatal("hath Stop was not called")
	}
}

func TestRuntimeRunRejectsNilDependencies(t *testing.T) {
	tests := []struct {
		name    string
		runtime *Runtime
		want    string
	}{
		{name: "nil runtime", runtime: nil, want: "runtime 未初始化"},
		{name: "nil natmap", runtime: &Runtime{Hath: &fakeHath{}, Updater: &fakeUpdater{}, Events: make(chan natmap.Mapping), BindPort: 7000}, want: "natmap 进程未配置"},
		{name: "nil hath", runtime: &Runtime{Natmap: newFakeNatmapProcess(), Updater: &fakeUpdater{}, Events: make(chan natmap.Mapping), BindPort: 7000}, want: "hath 控制器未配置"},
		{name: "nil updater", runtime: &Runtime{Natmap: newFakeNatmapProcess(), Hath: &fakeHath{}, Events: make(chan natmap.Mapping), BindPort: 7000}, want: "端口更新器未配置"},
		{name: "nil events", runtime: &Runtime{Natmap: newFakeNatmapProcess(), Hath: &fakeHath{}, Updater: &fakeUpdater{}, BindPort: 7000}, want: "natmap 事件通道未配置"},
		{name: "missing bind port", runtime: &Runtime{Natmap: newFakeNatmapProcess(), Hath: &fakeHath{}, Updater: &fakeUpdater{}, Events: make(chan natmap.Mapping)}, want: "本地固定端口未配置"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.runtime.Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestHandleMappingNilCoordinatorReturnsErrorWithoutPanic(t *testing.T) {
	var coordinator *Coordinator

	assertHandleMappingError(t, coordinator, "supervisor 协调器未初始化")
}

func TestHandleMappingNilHathReturnsErrorWithoutPanic(t *testing.T) {
	coordinator := &Coordinator{Updater: &fakeUpdater{}}

	assertHandleMappingError(t, coordinator, "hath 控制器未配置")
}

func TestHandleMappingNilUpdaterReturnsErrorWithoutPanic(t *testing.T) {
	coordinator := &Coordinator{Hath: &fakeHath{}}

	assertHandleMappingError(t, coordinator, "端口更新器未配置")
}

func TestHandleMappingFirstMappingUpdatesPublicPortAndStartsWithBindPort(t *testing.T) {
	ctx := context.Background()
	mapping := testMapping("203.0.113.10", 50000, 7000)
	hath := &fakeHath{}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, BindPort: 7000}

	if err := coordinator.HandleMapping(ctx, mapping); err != nil {
		t.Fatalf("HandleMapping returned error: %v", err)
	}

	assertInts(t, updater.updatedPorts(), []int{50000})
	assertInts(t, hath.startPorts(), []int{7000})
	if hath.stopCount() != 0 {
		t.Fatalf("expected no stops, got %d", hath.stopCount())
	}
	if !coordinator.CurrentMapping.SamePublicEndpoint(mapping) {
		t.Fatalf("expected current mapping to be updated")
	}
}

func TestHandleMappingRejectsMismatchedPrivatePort(t *testing.T) {
	ctx := context.Background()
	mapping := testMapping("203.0.113.10", 50000, 7100)
	hath := &fakeHath{}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, BindPort: 7000}

	err := coordinator.HandleMapping(ctx, mapping)
	if err == nil || !strings.Contains(err.Error(), "natmap 本地端口") {
		t.Fatalf("HandleMapping error = %v, want private port mismatch", err)
	}

	assertInts(t, updater.updatedPorts(), nil)
	assertInts(t, hath.startPorts(), nil)
}

func TestHandleMappingSameMappingWhileRunningDoesNothing(t *testing.T) {
	ctx := context.Background()
	mapping := testMapping("203.0.113.10", 50000, 7000)
	hath := &fakeHath{running: true}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: mapping, BindPort: 7000}

	if err := coordinator.HandleMapping(ctx, mapping); err != nil {
		t.Fatalf("HandleMapping returned error: %v", err)
	}

	assertInts(t, updater.updatedPorts(), nil)
	assertInts(t, hath.startPorts(), nil)
	if hath.stopCount() != 0 {
		t.Fatalf("expected no stops, got %d", hath.stopCount())
	}
}

func TestHandleMappingChangedMappingStopsUpdatesThenStarts(t *testing.T) {
	ctx := context.Background()
	oldMapping := testMapping("203.0.113.10", 50000, 7000)
	newMapping := testMapping("203.0.113.20", 51000, 7000)
	events := []string{}
	hath := &fakeHath{running: true, events: &events}
	updater := &fakeUpdater{events: &events}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: oldMapping, BindPort: 7000}

	if err := coordinator.HandleMapping(ctx, newMapping); err != nil {
		t.Fatalf("HandleMapping returned error: %v", err)
	}

	assertStrings(t, events, []string{"stop", "update", "start"})
	if hath.stopCount() != 1 {
		t.Fatalf("expected one stop, got %d", hath.stopCount())
	}
	assertInts(t, updater.updatedPorts(), []int{51000})
	assertInts(t, hath.startPorts(), []int{7000})
	if !coordinator.CurrentMapping.SamePublicEndpoint(newMapping) {
		t.Fatalf("expected current mapping to be updated")
	}
}

func TestHandleMappingSameMappingWhileStoppedStartsWithoutUpdate(t *testing.T) {
	ctx := context.Background()
	mapping := testMapping("203.0.113.10", 50000, 7000)
	hath := &fakeHath{running: false}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: mapping, BindPort: 7000}

	if err := coordinator.HandleMapping(ctx, mapping); err != nil {
		t.Fatalf("HandleMapping returned error: %v", err)
	}

	assertInts(t, updater.updatedPorts(), nil)
	assertInts(t, hath.startPorts(), []int{7000})
	if hath.stopCount() != 0 {
		t.Fatalf("expected no stops, got %d", hath.stopCount())
	}
}

func TestHandleMappingStopFailureReturnsContextErrorAndStopsWorkflow(t *testing.T) {
	ctx := context.Background()
	oldMapping := testMapping("203.0.113.10", 50000, 7000)
	newMapping := testMapping("203.0.113.20", 51000, 7000)
	stopErr := errors.New("stop failed")
	hath := &fakeHath{running: true, errStop: stopErr}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: oldMapping, BindPort: 7000}

	err := coordinator.HandleMapping(ctx, newMapping)

	assertWrappedError(t, err, stopErr, "停止 hath-rust 失败")
	assertInts(t, updater.updatedPorts(), nil)
	assertInts(t, hath.startPorts(), nil)
	if !coordinator.CurrentMapping.SamePublicEndpoint(oldMapping) {
		t.Fatalf("expected current mapping to remain unchanged")
	}
}

func TestHandleMappingUpdateFailureReturnsContextErrorAndStopsWorkflow(t *testing.T) {
	ctx := context.Background()
	oldMapping := testMapping("203.0.113.10", 50000, 7000)
	newMapping := testMapping("203.0.113.20", 51000, 7000)
	updateErr := errors.New("update failed")
	hath := &fakeHath{running: false}
	updater := &fakeUpdater{errUpdate: updateErr}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: oldMapping, BindPort: 7000}

	err := coordinator.HandleMapping(ctx, newMapping)

	assertWrappedError(t, err, updateErr, "更新 H@H 公网端口失败")
	assertInts(t, hath.startPorts(), nil)
	if !coordinator.CurrentMapping.SamePublicEndpoint(oldMapping) {
		t.Fatalf("expected current mapping to remain unchanged")
	}
}

func TestHandleMappingStartFailureReturnsContextError(t *testing.T) {
	ctx := context.Background()
	oldMapping := testMapping("203.0.113.10", 50000, 7000)
	newMapping := testMapping("203.0.113.20", 51000, 7000)
	startErr := errors.New("start failed")
	hath := &fakeHath{running: false, errStart: startErr}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: oldMapping, BindPort: 7000}

	err := coordinator.HandleMapping(ctx, newMapping)

	assertWrappedError(t, err, startErr, "启动 hath-rust 失败")
	assertInts(t, updater.updatedPorts(), []int{51000})
	if !coordinator.CurrentMapping.SamePublicEndpoint(oldMapping) {
		t.Fatalf("expected current mapping to remain unchanged")
	}
}

func testMapping(publicAddress string, publicPort int, privatePort int) natmap.Mapping {
	return natmap.Mapping{
		PublicAddress:  publicAddress,
		PublicPort:     publicPort,
		IP4P:           "198.51.100.1",
		PrivatePort:    privatePort,
		Protocol:       "TCP",
		PrivateAddress: "192.0.2.10",
	}
}

func assertHandleMappingError(t *testing.T, coordinator *Coordinator, message string) {
	t.Helper()

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("HandleMapping panicked: %v", recovered)
		}
	}()

	err := coordinator.HandleMapping(context.Background(), testMapping("203.0.113.10", 50000, 7000))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), message) {
		t.Fatalf("expected error %q to contain %q", err.Error(), message)
	}
}

func assertInts(t *testing.T, got []int, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func assertStrings(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for condition")
		case <-tick.C:
		}
	}
}

func waitForRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to exit")
		return nil
	}
}

func assertWrappedError(t *testing.T, got error, want error, message string) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("expected error to wrap %v, got %v", want, got)
	}
	if !strings.Contains(got.Error(), message) {
		t.Fatalf("expected error %q to contain %q", got.Error(), message)
	}
}
