package app_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/app"
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

	cleanup, err := app.RunUPnPModeOnce(context.Background(), cfg, hathController, updater, mapper)
	if err != nil {
		t.Fatalf("app.RunUPnPModeOnce returned error: %v", err)
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

	cleanup, err := app.RunUPnPModeOnce(ctx, cfg, hathController, updater, mapper)
	if err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("app.RunUPnPModeOnce returned nil error, want update error")
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

	cleanup, err := app.RunUPnPModeOnce(context.Background(), cfg, hathController, updater, mapper)
	if !errors.Is(err, wantErr) {
		t.Fatalf("app.RunUPnPModeOnce error = %v, want %v", err, wantErr)
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

	cleanup, err := app.RunUPnPModeOnce(context.Background(), cfg, hathController, updater, mapper)
	if !errors.Is(err, wantErr) {
		t.Fatalf("app.RunUPnPModeOnce error = %v, want %v", err, wantErr)
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
		errCh <- app.RunUPnPMode(ctx, cfg, hathController, updater, mapper)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("app.RunUPnPMode returned error: %v", err)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("app.RunUPnPMode did not stop after renewal")
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
}

func TestRunUPnPModeDeletesMappingWhenRenewSeesShutdown(t *testing.T) {
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
		errCh <- app.RunUPnPMode(ctx, cfg, hathController, updater, mapper)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("app.RunUPnPMode returned error: %v", err)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("app.RunUPnPMode did not return after shutdown during renew")
	}
	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
}

func TestRunUPnPModeDoesNotDeleteMappingWhenRenewOperationTimesOut(t *testing.T) {
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
	}
	mapper.onAdd = func(call int, cfg upnp.Config) {
		if call == 2 {
			mapper.addErr = context.DeadlineExceeded
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.RunUPnPMode(context.Background(), cfg, hathController, updater, mapper)
	}()

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "续租 UPnP 端口映射失败") {
			t.Fatalf("app.RunUPnPMode error = %v, want renew timeout error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("app.RunUPnPMode did not return after renew timeout")
	}
	if mapper.deleteCalls != 0 {
		t.Fatalf("DeleteMapping calls = %d, want 0", mapper.deleteCalls)
	}
}

func TestRunUPnPModeDoesNotDeleteMappingWhenRenewFails(t *testing.T) {
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
	}
	mapper.onAdd = func(call int, cfg upnp.Config) {
		if call == 2 {
			mapper.addErr = errors.New("renew failed")
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.RunUPnPMode(context.Background(), cfg, hathController, updater, mapper)
	}()

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "续租 UPnP 端口映射失败") {
			t.Fatalf("app.RunUPnPMode error = %v, want renew error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("app.RunUPnPMode did not return after renew failure")
	}
	if mapper.deleteCalls != 0 {
		t.Fatalf("DeleteMapping calls = %d, want 0", mapper.deleteCalls)
	}
	if hathController.stopCalls != 1 {
		t.Fatalf("hath stop calls = %d, want 1", hathController.stopCalls)
	}
}

func TestRunUPnPModeWithRetryRetriesInitialAddFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 0,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
			RestartDelay:    config.Duration{Duration: time.Millisecond},
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
	if err := app.RunUPnPModeWithRetry(ctx, cfg, hathController, updater, mapper); err != nil {
		t.Fatalf("app.RunUPnPModeWithRetry returned error: %v", err)
	}
	if mapper.addCalls != 2 {
		t.Fatalf("AddMapping calls = %d, want 2", mapper.addCalls)
	}
	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
}

func TestRunUPnPModeWithRetryRetriesUpdateFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 0,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
			RestartDelay:    config.Duration{Duration: time.Millisecond},
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
			updater.errUpdate = nil
			cancel()
		}
	}

	if err := app.RunUPnPModeWithRetry(ctx, cfg, hathController, updater, mapper); err != nil {
		t.Fatalf("app.RunUPnPModeWithRetry returned error: %v", err)
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
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 1,
			Description:   "upnp-test",
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 5 * time.Second},
			RestartDelay:    config.Duration{Duration: time.Millisecond},
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

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.RunUPnPModeWithRetry(ctx, cfg, hathController, updater, mapper)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("app.RunUPnPModeWithRetry returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("app.RunUPnPModeWithRetry did not retry after renewal failure")
	}
	if mapper.addCalls != 3 {
		t.Fatalf("AddMapping calls = %d, want 3", mapper.addCalls)
	}
	if mapper.deleteCalls != 1 {
		t.Fatalf("DeleteMapping calls = %d, want 1", mapper.deleteCalls)
	}
}
