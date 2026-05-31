package supervisor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
)

type fakeHath struct {
	running  bool
	starts   []int
	stops    int
	errStart error
	errStop  error
	events   *[]string
}

func (f *fakeHath) Start(ctx context.Context, port int) error {
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
	return f.running
}

type fakeUpdater struct {
	ports     []int
	errUpdate error
	events    *[]string
}

func (f *fakeUpdater) UpdatePort(ctx context.Context, port int) error {
	if f.events != nil {
		*f.events = append(*f.events, "update")
	}
	if f.errUpdate != nil {
		return f.errUpdate
	}
	f.ports = append(f.ports, port)
	return nil
}

func TestHandleMappingFirstMappingUpdatesPublicPortAndStartsWithPrivatePort(t *testing.T) {
	ctx := context.Background()
	mapping := testMapping("203.0.113.10", 50000, 7000)
	hath := &fakeHath{}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater}

	if err := coordinator.HandleMapping(ctx, mapping); err != nil {
		t.Fatalf("HandleMapping returned error: %v", err)
	}

	assertInts(t, updater.ports, []int{50000})
	assertInts(t, hath.starts, []int{7000})
	if hath.stops != 0 {
		t.Fatalf("expected no stops, got %d", hath.stops)
	}
	if !coordinator.CurrentMapping.SamePublicEndpoint(mapping) {
		t.Fatalf("expected current mapping to be updated")
	}
}

func TestHandleMappingSameMappingWhileRunningDoesNothing(t *testing.T) {
	ctx := context.Background()
	mapping := testMapping("203.0.113.10", 50000, 7000)
	hath := &fakeHath{running: true}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: mapping}

	if err := coordinator.HandleMapping(ctx, mapping); err != nil {
		t.Fatalf("HandleMapping returned error: %v", err)
	}

	assertInts(t, updater.ports, nil)
	assertInts(t, hath.starts, nil)
	if hath.stops != 0 {
		t.Fatalf("expected no stops, got %d", hath.stops)
	}
}

func TestHandleMappingChangedMappingStopsUpdatesThenStarts(t *testing.T) {
	ctx := context.Background()
	oldMapping := testMapping("203.0.113.10", 50000, 7000)
	newMapping := testMapping("203.0.113.20", 51000, 7100)
	events := []string{}
	hath := &fakeHath{running: true, events: &events}
	updater := &fakeUpdater{events: &events}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: oldMapping}

	if err := coordinator.HandleMapping(ctx, newMapping); err != nil {
		t.Fatalf("HandleMapping returned error: %v", err)
	}

	assertStrings(t, events, []string{"stop", "update", "start"})
	if hath.stops != 1 {
		t.Fatalf("expected one stop, got %d", hath.stops)
	}
	assertInts(t, updater.ports, []int{51000})
	assertInts(t, hath.starts, []int{7100})
	if !coordinator.CurrentMapping.SamePublicEndpoint(newMapping) {
		t.Fatalf("expected current mapping to be updated")
	}
}

func TestHandleMappingSameMappingWhileStoppedStartsWithoutUpdate(t *testing.T) {
	ctx := context.Background()
	mapping := testMapping("203.0.113.10", 50000, 7000)
	hath := &fakeHath{running: false}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: mapping}

	if err := coordinator.HandleMapping(ctx, mapping); err != nil {
		t.Fatalf("HandleMapping returned error: %v", err)
	}

	assertInts(t, updater.ports, nil)
	assertInts(t, hath.starts, []int{7000})
	if hath.stops != 0 {
		t.Fatalf("expected no stops, got %d", hath.stops)
	}
}

func TestHandleMappingStopFailureReturnsContextErrorAndStopsWorkflow(t *testing.T) {
	ctx := context.Background()
	oldMapping := testMapping("203.0.113.10", 50000, 7000)
	newMapping := testMapping("203.0.113.20", 51000, 7100)
	stopErr := errors.New("stop failed")
	hath := &fakeHath{running: true, errStop: stopErr}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: oldMapping}

	err := coordinator.HandleMapping(ctx, newMapping)

	assertWrappedError(t, err, stopErr, "停止 hath-rust 失败")
	assertInts(t, updater.ports, nil)
	assertInts(t, hath.starts, nil)
	if !coordinator.CurrentMapping.SamePublicEndpoint(oldMapping) {
		t.Fatalf("expected current mapping to remain unchanged")
	}
}

func TestHandleMappingUpdateFailureReturnsContextErrorAndStopsWorkflow(t *testing.T) {
	ctx := context.Background()
	oldMapping := testMapping("203.0.113.10", 50000, 7000)
	newMapping := testMapping("203.0.113.20", 51000, 7100)
	updateErr := errors.New("update failed")
	hath := &fakeHath{running: false}
	updater := &fakeUpdater{errUpdate: updateErr}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: oldMapping}

	err := coordinator.HandleMapping(ctx, newMapping)

	assertWrappedError(t, err, updateErr, "更新 H@H 公网端口失败")
	assertInts(t, hath.starts, nil)
	if !coordinator.CurrentMapping.SamePublicEndpoint(oldMapping) {
		t.Fatalf("expected current mapping to remain unchanged")
	}
}

func TestHandleMappingStartFailureReturnsContextError(t *testing.T) {
	ctx := context.Background()
	oldMapping := testMapping("203.0.113.10", 50000, 7000)
	newMapping := testMapping("203.0.113.20", 51000, 7100)
	startErr := errors.New("start failed")
	hath := &fakeHath{running: false, errStart: startErr}
	updater := &fakeUpdater{}
	coordinator := &Coordinator{Hath: hath, Updater: updater, CurrentMapping: oldMapping}

	err := coordinator.HandleMapping(ctx, newMapping)

	assertWrappedError(t, err, startErr, "启动 hath-rust 失败")
	assertInts(t, updater.ports, []int{51000})
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

func assertWrappedError(t *testing.T, got error, want error, message string) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("expected error to wrap %v, got %v", want, got)
	}
	if !strings.Contains(got.Error(), message) {
		t.Fatalf("expected error %q to contain %q", got.Error(), message)
	}
}
