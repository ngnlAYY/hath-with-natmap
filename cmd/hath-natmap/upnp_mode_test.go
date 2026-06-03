package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/config"
	"github.com/ngnlAYY/hath-with-natter/internal/upnp"
)

func TestRunUPnPModeOnceAddsMappingUpdatesAndStartsHath(t *testing.T) {
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 30,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
		},
	}
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
	}

	cleanup, err := runUPnPModeOnce(context.Background(), cfg, hathController, updater, mapper)
	if err != nil {
		t.Fatalf("runUPnPModeOnce returned error: %v", err)
	}
	if cleanup == nil {
		t.Fatal("cleanup = nil, want non-nil")
	}

	wantUPnPCfg := upnp.Config{
		Port:          4567,
		LeaseDuration: 30,
		Description:   "upnp-test",
	}
	if !reflect.DeepEqual(mapper.addCfg, wantUPnPCfg) {
		t.Fatalf("AddMapping cfg = %#v, want %#v", mapper.addCfg, wantUPnPCfg)
	}
	if !mapper.addHadDeadline {
		t.Fatal("AddMapping context missing deadline")
	}
	if !reflect.DeepEqual(updater.ports, []int{54000}) {
		t.Fatalf("updated ports = %#v, want %#v", updater.ports, []int{54000})
	}
	if !reflect.DeepEqual(hathController.starts, []int{4567}) {
		t.Fatalf("hath start ports = %#v, want %#v", hathController.starts, []int{4567})
	}

	cleanup()

	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
	if !reflect.DeepEqual(mapper.deleteCfg, wantUPnPCfg) {
		t.Fatalf("DeleteMapping cfg = %#v, want %#v", mapper.deleteCfg, wantUPnPCfg)
	}
	if !mapper.deleteHadDeadline {
		t.Fatal("DeleteMapping context missing deadline")
	}
}

func TestRunUPnPModeOnceUsesIndependentAddContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 45,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
		},
	}
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{errUpdate: errors.New("update failed")}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
	}

	cleanup, err := runUPnPModeOnce(ctx, cfg, hathController, updater, mapper)
	if err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("runUPnPModeOnce returned nil error, want update error")
	}
	if mapper.addCtxErr != nil {
		t.Fatalf("AddMapping ctx.Err = %v, want nil", mapper.addCtxErr)
	}
	if !mapper.addHadDeadline {
		t.Fatal("AddMapping context missing deadline")
	}
	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
}

func TestRunUPnPModeOnceDeletesMappingWhenUpdateFails(t *testing.T) {
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 45,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
		},
	}
	wantErr := errors.New("update failed")
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{errUpdate: wantErr}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
	}

	cleanup, err := runUPnPModeOnce(context.Background(), cfg, hathController, updater, mapper)
	if !errors.Is(err, wantErr) {
		t.Fatalf("runUPnPModeOnce error = %v, want %v", err, wantErr)
	}
	if cleanup != nil {
		t.Fatal("cleanup != nil, want nil")
	}
	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
	if len(hathController.starts) != 0 {
		t.Fatalf("hath start ports = %#v, want none", hathController.starts)
	}
}

func TestRunUPnPModeOnceDoesNotDeleteMappingWhenAddFails(t *testing.T) {
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 45,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
		},
	}
	wantErr := errors.New("add failed")
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{}
	mapper := &fakeMainUPnPMapper{addErr: wantErr}

	cleanup, err := runUPnPModeOnce(context.Background(), cfg, hathController, updater, mapper)
	if !errors.Is(err, wantErr) {
		t.Fatalf("runUPnPModeOnce error = %v, want %v", err, wantErr)
	}
	if cleanup != nil {
		t.Fatal("cleanup != nil, want nil")
	}
	if mapper.deleteCalls != 0 {
		t.Fatalf("DeleteMapping calls = %d, want 0", mapper.deleteCalls)
	}
	if len(updater.ports) != 0 {
		t.Fatalf("updated ports = %#v, want none", updater.ports)
	}
	if len(hathController.starts) != 0 {
		t.Fatalf("hath start ports = %#v, want none", hathController.starts)
	}
}

func TestRunUPnPModeRenewsLease(t *testing.T) {
	oldTicker := newUPnPRenewTicker
	tickCh := make(chan time.Time)
	intervalCh := make(chan time.Duration, 1)
	stopped := false
	newUPnPRenewTicker = func(interval time.Duration) (<-chan time.Time, func()) {
		intervalCh <- interval
		return tickCh, func() { stopped = true }
	}
	defer func() { newUPnPRenewTicker = oldTicker }()

	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 1,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
		},
	}
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
		onAdd: func(call int, cfg upnp.Config) {
			if call == 2 {
				cancel()
			}
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runUPnPMode(ctx, cfg, hathController, updater, mapper)
	}()

	select {
	case interval := <-intervalCh:
		if interval != 500*time.Millisecond {
			cancel()
			t.Fatalf("renew interval = %s, want %s", interval, 500*time.Millisecond)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("renew ticker was not created")
	}

	tickCh <- time.Now()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runUPnPMode returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runUPnPMode did not stop after renewal")
	}
	if mapper.addCalls != 2 {
		t.Fatalf("AddMapping calls = %d, want 2", mapper.addCalls)
	}
	if hathController.stopCalls != 1 {
		t.Fatalf("hath stop calls = %d, want 1", hathController.stopCalls)
	}
	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
	if !stopped {
		t.Fatal("renew ticker stop was not called")
	}
}

func TestRunUPnPModeDeletesMappingWhenRenewSeesShutdown(t *testing.T) {
	oldTicker := newUPnPRenewTicker
	tickCh := make(chan time.Time)
	newUPnPRenewTicker = func(interval time.Duration) (<-chan time.Time, func()) {
		return tickCh, func() {}
	}
	defer func() { newUPnPRenewTicker = oldTicker }()

	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 2,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
		},
	}
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
		onAdd: func(call int, cfg upnp.Config) {
			if call == 2 {
				cancel()
			}
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runUPnPMode(ctx, cfg, hathController, updater, mapper)
	}()

	tickCh <- time.Now()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runUPnPMode returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runUPnPMode did not return after shutdown during renew")
	}
	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
}

func TestRunUPnPModeDoesNotDeleteMappingWhenRenewOperationTimesOut(t *testing.T) {
	oldTicker := newUPnPRenewTicker
	tickCh := make(chan time.Time)
	newUPnPRenewTicker = func(interval time.Duration) (<-chan time.Time, func()) {
		return tickCh, func() {}
	}
	defer func() { newUPnPRenewTicker = oldTicker }()

	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 2,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
		},
	}
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
	}
	mapper.onAdd = func(call int, cfg upnp.Config) {
		if call == 2 {
			mapper.addErr = context.DeadlineExceeded
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runUPnPMode(context.Background(), cfg, hathController, updater, mapper)
	}()

	tickCh <- time.Now()

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "续租 UPnP 端口映射失败") {
			t.Fatalf("runUPnPMode error = %v, want renew timeout error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runUPnPMode did not return after renew timeout")
	}
	if mapper.deleteCalls != 0 {
		t.Fatalf("DeleteMapping calls = %d, want 0", mapper.deleteCalls)
	}
}

func TestRunUPnPModeDoesNotDeleteMappingWhenRenewFails(t *testing.T) {
	oldTicker := newUPnPRenewTicker
	tickCh := make(chan time.Time)
	newUPnPRenewTicker = func(interval time.Duration) (<-chan time.Time, func()) {
		return tickCh, func() {}
	}
	defer func() { newUPnPRenewTicker = oldTicker }()

	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 2,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
		},
	}
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
	}
	mapper.onAdd = func(call int, cfg upnp.Config) {
		if call == 2 {
			mapper.addErr = errors.New("renew failed")
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runUPnPMode(context.Background(), cfg, hathController, updater, mapper)
	}()

	tickCh <- time.Now()

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "续租 UPnP 端口映射失败") {
			t.Fatalf("runUPnPMode error = %v, want renew error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runUPnPMode did not return after renew failure")
	}
	if mapper.deleteCalls != 0 {
		t.Fatalf("DeleteMapping calls = %d, want 0", mapper.deleteCalls)
	}
	if hathController.stopCalls != 1 {
		t.Fatalf("hath stop calls = %d, want 1", hathController.stopCalls)
	}
}

func TestRunUPnPModeWithRetryRetriesInitialAddFailure(t *testing.T) {
	oldSleep := sleepUPnPRestart
	defer func() { sleepUPnPRestart = oldSleep }()

	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 0,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
			RestartDelay:    config.Duration{Duration: 20 * time.Second},
			Retry:           config.RetryConfig{InitialDelay: config.Duration{Duration: time.Second}},
		},
	}
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
	}
	mapper.onAdd = func(call int, cfg upnp.Config) {
		if call == 1 {
			mapper.addErr = errors.New("add failed")
			return
		}
		mapper.addErr = nil
		cancel()
	}
	sleepCalls := 0
	sleepUPnPRestart = func(ctx context.Context, delay time.Duration) bool {
		sleepCalls++
		if delay != 20*time.Second {
			t.Fatalf("restart delay = %s, want %s", delay, 20*time.Second)
		}
		return true
	}

	if err := runUPnPModeWithRetry(ctx, cfg, hathController, updater, mapper); err != nil {
		t.Fatalf("runUPnPModeWithRetry returned error: %v", err)
	}
	if mapper.addCalls != 2 {
		t.Fatalf("AddMapping calls = %d, want 2", mapper.addCalls)
	}
	if sleepCalls != 1 {
		t.Fatalf("restart sleeps = %d, want 1", sleepCalls)
	}
	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
}

func TestRunUPnPModeWithRetryRetriesUpdateFailure(t *testing.T) {
	oldSleep := sleepUPnPRestart
	defer func() { sleepUPnPRestart = oldSleep }()

	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 0,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
			RestartDelay:    config.Duration{Duration: 20 * time.Second},
		},
	}
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{errUpdate: errors.New("update failed")}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
	}
	mapper.onAdd = func(call int, cfg upnp.Config) {
		if call == 2 {
			cancel()
		}
	}
	sleepUPnPRestart = func(ctx context.Context, delay time.Duration) bool {
		updater.errUpdate = nil
		return true
	}

	if err := runUPnPModeWithRetry(ctx, cfg, hathController, updater, mapper); err != nil {
		t.Fatalf("runUPnPModeWithRetry returned error: %v", err)
	}
	if mapper.addCalls != 2 {
		t.Fatalf("AddMapping calls = %d, want 2", mapper.addCalls)
	}
	if mapper.deleteCalls != 2 {
		t.Fatalf("DeleteMapping calls = %d, want 2", mapper.deleteCalls)
	}
	if !reflect.DeepEqual(hathController.starts, []int{4567}) {
		t.Fatalf("hath start ports = %#v, want %#v", hathController.starts, []int{4567})
	}
}

func TestRunUPnPModeWithRetryRetriesRenewFailure(t *testing.T) {
	oldTicker := newUPnPRenewTicker
	oldSleep := sleepUPnPRestart
	defer func() {
		newUPnPRenewTicker = oldTicker
		sleepUPnPRestart = oldSleep
	}()

	tickChs := make(chan chan time.Time, 2)
	newUPnPRenewTicker = func(interval time.Duration) (<-chan time.Time, func()) {
		tickCh := make(chan time.Time)
		tickChs <- tickCh
		return tickCh, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 2,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
			RestartDelay:    config.Duration{Duration: 20 * time.Second},
		},
	}
	hathController := &fakeMainHath{}
	updater := &fakeMainUpdater{}
	mapper := &fakeMainUPnPMapper{
		addResult: upnp.Mapping{
			PublicAddress:  "203.0.113.10",
			PublicPort:     54000,
			PrivatePort:    4567,
			PrivateAddress: "192.168.1.10",
		},
	}
	mapper.onAdd = func(call int, cfg upnp.Config) {
		if call == 2 {
			mapper.addErr = errors.New("renew failed")
			return
		}
		mapper.addErr = nil
		if call == 3 {
			cancel()
		}
	}
	sleepCalls := 0
	sleepUPnPRestart = func(ctx context.Context, delay time.Duration) bool {
		sleepCalls++
		return true
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runUPnPModeWithRetry(ctx, cfg, hathController, updater, mapper)
	}()

	firstTick := <-tickChs
	firstTick <- time.Now()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runUPnPModeWithRetry returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runUPnPModeWithRetry did not retry after renewal failure")
	}
	if mapper.addCalls != 3 {
		t.Fatalf("AddMapping calls = %d, want 3", mapper.addCalls)
	}
	if sleepCalls != 1 {
		t.Fatalf("restart sleeps = %d, want 1", sleepCalls)
	}
	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
}
