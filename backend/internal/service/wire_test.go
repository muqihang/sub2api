package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/collection"
)

func TestProvideTimingWheelService_ReturnsError(t *testing.T) {
	original := newTimingWheel
	t.Cleanup(func() { newTimingWheel = original })

	newTimingWheel = func(_ time.Duration, _ int, _ collection.Execute) (*collection.TimingWheel, error) {
		return nil, errors.New("boom")
	}

	svc, err := ProvideTimingWheelService()
	if err == nil {
		t.Fatalf("期望返回 error，但得到 nil")
	}
	if svc != nil {
		t.Fatalf("期望返回 nil svc，但得到非空")
	}
}

func TestProvideTimingWheelService_Success(t *testing.T) {
	svc, err := ProvideTimingWheelService()
	if err != nil {
		t.Fatalf("期望 err 为 nil，但得到: %v", err)
	}
	if svc == nil {
		t.Fatalf("期望 svc 非空，但得到 nil")
	}
	svc.Stop()
}

func TestProvideFormalPoolRuntimeRegistrationStartupReplayUsesCCGatewayRegistrar(t *testing.T) {
	account := formalPoolDiagnosticsAccount(map[string]any{
		FormalPoolExtraOnboardingStage:     FormalPoolStageHealthcheckPassed,
		FormalPoolExtraRuntimeRegistered:   "false",
		FormalPoolExtraRuntimeRegisteredAt: "",
		"cc_gateway_account_ref":           "hmac-sha256:" + strings.Repeat("5", 64),
		"cc_gateway_egress_bucket":         "claude-provider-bucket",
	})
	account.Status = StatusActive
	proxyID := int64(66)
	account.ProxyID = &proxyID
	store := &formalPoolRuntimeReplayStore{accounts: []*Account{account}}
	runtime := &formalPoolOperationsRuntimeFake{}
	proxy := &formalPoolOperationsProxyFake{proxy: &Proxy{ID: 66, Protocol: "socks5h", Host: "provider-proxy.local", Port: 1080, Status: StatusActive}}

	runner := ProvideFormalPoolRuntimeRegistrationStartupReplayWithDeps(store, proxy, runtime, func() time.Time { return time.Date(2026, 5, 30, 3, 4, 5, 0, time.UTC) })

	if runner == nil {
		t.Fatalf("expected startup replay runner")
	}
	if !runtime.called {
		t.Fatalf("startup replay should call runtime registrar")
	}
	if runtime.input.EgressBucket != "claude-provider-bucket" || runtime.input.ProxyURL != "socks5h://provider-proxy.local:1080" {
		t.Fatalf("bad replay registration input: %#v", runtime.input)
	}
	if account.Schedulable || account.GetExtraString(FormalPoolExtraRuntimeRegistered) != "true" || account.GetExtraString(FormalPoolExtraRuntimeRegisteredAt) != "2026-05-30T03:04:05Z" {
		t.Fatalf("startup replay should not make account schedulable and should record timestamp: %#v", account)
	}
}
