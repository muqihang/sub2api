//go:build unit

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type responsesProbeSchedulerExecutorStub struct {
	active         atomic.Int32
	maxActive      atomic.Int32
	calls          atomic.Int32
	webSearchCalls atomic.Int32
	started        chan struct{}
	release        chan struct{}
}

func (s *responsesProbeSchedulerExecutorStub) ProbeOpenAIAPIKeyResponsesSupportForModel(ctx context.Context, _ int64, _ string) {
	active := s.active.Add(1)
	defer s.active.Add(-1)
	s.calls.Add(1)
	for {
		current := s.maxActive.Load()
		if active <= current || s.maxActive.CompareAndSwap(current, active) {
			break
		}
	}
	if s.started != nil {
		s.started <- struct{}{}
	}
	if s.release != nil {
		select {
		case <-ctx.Done():
		case <-s.release:
		}
	}
}

func (s *responsesProbeSchedulerExecutorStub) ProbeOpenAIWebSearchSupportForModel(_ context.Context, _ int64, _ string) {
	s.webSearchCalls.Add(1)
}

type responsesProbeSchedulerListerStub struct {
	mu           sync.Mutex
	accounts     []Account
	pages        []int
	accountTypes []string
}

type responsesProbeSchedulerPanicExecutorStub struct {
	calls atomic.Int32
}

func (s *responsesProbeSchedulerPanicExecutorStub) ProbeOpenAIAPIKeyResponsesSupportForModel(_ context.Context, _ int64, _ string) {
	if s.calls.Add(1) == 1 {
		panic("probe panic")
	}
}

func (s *responsesProbeSchedulerPanicExecutorStub) ProbeOpenAIWebSearchSupportForModel(_ context.Context, _ int64, _ string) {
}

func (s *responsesProbeSchedulerListerStub) ListWithFilters(_ context.Context, params pagination.PaginationParams, _, accountType, _, _ string, _ int64, _ string) ([]Account, *pagination.PaginationResult, error) {
	s.mu.Lock()
	s.pages = append(s.pages, params.Page)
	s.accountTypes = append(s.accountTypes, accountType)
	s.mu.Unlock()
	start := (params.Page - 1) * params.PageSize
	if start >= len(s.accounts) {
		return nil, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize, Total: int64(len(s.accounts)), Pages: params.Page - 1}, nil
	}
	end := start + params.PageSize
	if end > len(s.accounts) {
		end = len(s.accounts)
	}
	pages := (len(s.accounts) + params.PageSize - 1) / params.PageSize
	return append([]Account(nil), s.accounts[start:end]...), &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize, Total: int64(len(s.accounts)), Pages: pages}, nil
}

func testResponsesProbeSchedulerOptions() OpenAIResponsesProbeSchedulerOptions {
	return OpenAIResponsesProbeSchedulerOptions{
		WorkerCount:      2,
		QueueSize:        16,
		TaskTimeout:      5 * time.Second,
		BackfillInterval: time.Hour,
		DisableBackfill:  true,
	}
}

func TestOpenAIResponsesProbeScheduler_BoundsConcurrency(t *testing.T) {
	executor := &responsesProbeSchedulerExecutorStub{
		started: make(chan struct{}, 8),
		release: make(chan struct{}, 8),
	}
	scheduler := newOpenAIResponsesProbeScheduler(nil, executor, testResponsesProbeSchedulerOptions())
	scheduler.Start()
	defer scheduler.Stop()

	for id := int64(1); id <= 6; id++ {
		require.True(t, scheduler.Schedule(id, ""))
	}
	for range 2 {
		select {
		case <-executor.started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for probe workers")
		}
	}
	require.Equal(t, int32(2), executor.maxActive.Load())
	require.Equal(t, int32(2), executor.calls.Load())

	for range 6 {
		executor.release <- struct{}{}
	}
	require.Eventually(t, func() bool { return executor.calls.Load() == 6 }, time.Second, 10*time.Millisecond)
}

func TestOpenAIResponsesProbeScheduler_DeduplicatesWithOneFollowUp(t *testing.T) {
	executor := &responsesProbeSchedulerExecutorStub{
		started: make(chan struct{}, 4),
		release: make(chan struct{}, 4),
	}
	options := testResponsesProbeSchedulerOptions()
	options.WorkerCount = 1
	scheduler := newOpenAIResponsesProbeScheduler(nil, executor, options)
	scheduler.Start()
	defer scheduler.Stop()

	require.True(t, scheduler.Schedule(42, "gpt-first"))
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial probe")
	}
	for range 20 {
		require.True(t, scheduler.Schedule(42, "gpt-latest"))
	}
	executor.release <- struct{}{}
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for coalesced follow-up")
	}
	executor.release <- struct{}{}
	require.Eventually(t, func() bool { return executor.calls.Load() == 2 }, time.Second, 10*time.Millisecond)
	time.Sleep(25 * time.Millisecond)
	require.Equal(t, int32(2), executor.calls.Load())
}

func TestNeedsOpenAIResponsesProbe_UnknownAndStaleOnly(t *testing.T) {
	complete := Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"base_url": "https://provider.example/v1",
			"model_mapping": map[string]any{
				"gpt-5.6-sol": "gpt-5.6-sol",
			},
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesSupported:                true,
			openai_compat.ExtraKeyResponsesProbeModel:               "gpt-5.6-sol",
			openai_compat.ExtraKeyResponsesProbeTarget:              "",
			openai_compat.ExtraKeyResponsesCustomToolsSupported:     false,
			openai_compat.ExtraKeyResponsesCustomToolsProbeModel:    "gpt-5.6-sol",
			openai_compat.ExtraKeyResponsesCustomToolsProbeTarget:   "",
			openai_compat.ExtraKeyResponsesCustomToolsCurrentTarget: "",
		},
	}
	complete.Extra[openai_compat.ExtraKeyResponsesProbeTarget] = complete.OpenAIResponsesTargetFingerprint("gpt-5.6-sol")
	complete.Extra[openai_compat.ExtraKeyResponsesCustomToolsProbeTarget] = complete.OpenAIResponsesCustomToolsTargetFingerprint("gpt-5.6-sol")

	require.False(t, needsOpenAIResponsesProbe(&complete))
	unknown := complete
	unknown.Extra = nil
	require.True(t, needsOpenAIResponsesProbe(&unknown))
	unschedulable := unknown
	unschedulable.Schedulable = false
	require.False(t, needsOpenAIResponsesProbe(&unschedulable))
	errorAccount := unknown
	errorAccount.Status = StatusError
	require.False(t, needsOpenAIResponsesProbe(&errorAccount))
	stale := complete
	stale.Credentials = map[string]any{
		"base_url": "https://new-provider.example/v1",
		"model_mapping": map[string]any{
			"gpt-5.6-sol": "gpt-5.6-sol",
		},
	}
	require.True(t, needsOpenAIResponsesProbe(&stale))
	nonAPIKey := complete
	nonAPIKey.Type = AccountTypeOAuth
	require.False(t, needsOpenAIResponsesProbe(&nonAPIKey))
}

func TestNeedsOpenAIWebSearchProbe_IncludesOAuthAndRejectsCurrentEvidence(t *testing.T) {
	account := Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.6-sol"},
		},
	}
	require.True(t, needsOpenAIWebSearchProbe(&account))

	account.Extra = map[string]any{
		"openai_web_search_supported":    true,
		"openai_web_search_probe_model":  "gpt-5.6-sol",
		"openai_web_search_probe_target": account.OpenAIWebSearchTargetFingerprint(),
	}
	require.False(t, needsOpenAIWebSearchProbe(&account))

	account.Credentials["model_mapping"] = map[string]any{"gpt-5.6-sol": "changed-upstream-model"}
	require.True(t, needsOpenAIWebSearchProbe(&account))
}

func TestOpenAIResponsesProbeScheduler_RunBackfillSchedulesOAuthWebSearch(t *testing.T) {
	lister := &responsesProbeSchedulerListerStub{accounts: []Account{{
		ID:          253,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}}}
	executor := &responsesProbeSchedulerExecutorStub{}
	options := testResponsesProbeSchedulerOptions()
	options.WorkerCount = 1
	scheduler := newOpenAIResponsesProbeScheduler(lister, executor, options)
	scheduler.Start()
	defer scheduler.Stop()

	scheduler.RunBackfill(context.Background())

	require.Eventually(t, func() bool { return executor.webSearchCalls.Load() == 1 }, time.Second, 10*time.Millisecond)
	require.Zero(t, executor.calls.Load(), "OAuth accounts must not run the API-key Responses probe")
	lister.mu.Lock()
	accountTypes := append([]string(nil), lister.accountTypes...)
	lister.mu.Unlock()
	require.Equal(t, []string{""}, accountTypes, "backfill must list every OpenAI account type")
}

func TestOpenAIResponsesProbeScheduler_RunBackfillPaginates(t *testing.T) {
	accounts := make([]Account, 0, 501)
	for id := int64(1); id <= 501; id++ {
		accounts = append(accounts, Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true})
	}
	lister := &responsesProbeSchedulerListerStub{accounts: accounts}
	executor := &responsesProbeSchedulerExecutorStub{}
	options := testResponsesProbeSchedulerOptions()
	options.WorkerCount = 1
	options.QueueSize = 600
	scheduler := newOpenAIResponsesProbeScheduler(lister, executor, options)
	scheduler.Start()
	defer scheduler.Stop()

	scheduler.RunBackfill(context.Background())

	lister.mu.Lock()
	pages := append([]int(nil), lister.pages...)
	lister.mu.Unlock()
	require.Equal(t, []int{1, 2}, pages)
	require.Eventually(t, func() bool { return executor.calls.Load() == 501 }, 3*time.Second, 10*time.Millisecond)
}

func TestOpenAIResponsesProbeScheduler_StopCancelsProbe(t *testing.T) {
	executor := &responsesProbeSchedulerExecutorStub{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	options := testResponsesProbeSchedulerOptions()
	options.WorkerCount = 1
	scheduler := newOpenAIResponsesProbeScheduler(nil, executor, options)
	scheduler.Start()
	require.True(t, scheduler.Schedule(7, ""))
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for probe")
	}

	done := make(chan struct{})
	go func() {
		scheduler.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler stop did not cancel the running probe")
	}
}

func TestOpenAIResponsesProbeScheduler_PanicDoesNotKillWorker(t *testing.T) {
	executor := &responsesProbeSchedulerPanicExecutorStub{}
	options := testResponsesProbeSchedulerOptions()
	options.WorkerCount = 1
	scheduler := newOpenAIResponsesProbeScheduler(nil, executor, options)
	scheduler.Start()
	defer scheduler.Stop()

	require.True(t, scheduler.Schedule(1, ""))
	require.True(t, scheduler.Schedule(2, ""))
	require.Eventually(t, func() bool { return executor.calls.Load() == 2 }, time.Second, 10*time.Millisecond)
}
