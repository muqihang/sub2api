package service

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const openAIResponsesProbeBackfillPageSize = 500

type openAIResponsesProbeExecutor interface {
	ProbeOpenAIAPIKeyResponsesSupportForModel(ctx context.Context, accountID int64, requestedModel string)
}

type openAIResponsesProbeAccountLister interface {
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error)
}

type OpenAIResponsesProbeSchedulerOptions struct {
	WorkerCount      int
	QueueSize        int
	TaskTimeout      time.Duration
	TaskJitter       time.Duration
	BackfillInterval time.Duration
	BackfillJitter   time.Duration
	DisableBackfill  bool
}

func defaultOpenAIResponsesProbeSchedulerOptions() OpenAIResponsesProbeSchedulerOptions {
	return OpenAIResponsesProbeSchedulerOptions{
		WorkerCount:      2,
		QueueSize:        128,
		TaskTimeout:      70 * time.Second,
		TaskJitter:       750 * time.Millisecond,
		BackfillInterval: 15 * time.Minute,
		BackfillJitter:   2 * time.Minute,
	}
}

type openAIResponsesProbePending struct {
	model   string
	running bool
	dirty   bool
}

type OpenAIResponsesProbeScheduler struct {
	lister   openAIResponsesProbeAccountLister
	executor openAIResponsesProbeExecutor
	options  OpenAIResponsesProbeSchedulerOptions

	ctx    context.Context
	cancel context.CancelFunc
	jobs   chan int64

	mu      sync.Mutex
	pending map[int64]*openAIResponsesProbePending
	started bool
	stopped bool
	wg      sync.WaitGroup
}

func NewOpenAIResponsesProbeScheduler(accountRepo AccountRepository, accountTestService *AccountTestService) *OpenAIResponsesProbeScheduler {
	return newOpenAIResponsesProbeScheduler(accountRepo, accountTestService, defaultOpenAIResponsesProbeSchedulerOptions())
}

func newOpenAIResponsesProbeScheduler(lister openAIResponsesProbeAccountLister, executor openAIResponsesProbeExecutor, options OpenAIResponsesProbeSchedulerOptions) *OpenAIResponsesProbeScheduler {
	defaults := defaultOpenAIResponsesProbeSchedulerOptions()
	if options.WorkerCount <= 0 {
		options.WorkerCount = defaults.WorkerCount
	}
	if options.QueueSize <= 0 {
		options.QueueSize = defaults.QueueSize
	}
	if options.TaskTimeout <= 0 {
		options.TaskTimeout = defaults.TaskTimeout
	}
	if options.BackfillInterval <= 0 {
		options.BackfillInterval = defaults.BackfillInterval
	}
	if options.TaskJitter < 0 {
		options.TaskJitter = 0
	}
	if options.BackfillJitter < 0 {
		options.BackfillJitter = 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &OpenAIResponsesProbeScheduler{
		lister:   lister,
		executor: executor,
		options:  options,
		ctx:      ctx,
		cancel:   cancel,
		jobs:     make(chan int64, options.QueueSize),
		pending:  make(map[int64]*openAIResponsesProbePending),
	}
}

func (s *OpenAIResponsesProbeScheduler) Start() {
	if s == nil || s.executor == nil {
		return
	}
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	s.wg.Add(s.options.WorkerCount)
	for range s.options.WorkerCount {
		go s.worker()
	}
	if !s.options.DisableBackfill && s.lister != nil {
		s.wg.Add(1)
		go s.backfillLoop()
	}
}

// Schedule coalesces duplicate account probes. A trigger received while the
// account is running requests at most one follow-up with the latest model.
func (s *OpenAIResponsesProbeScheduler) Schedule(accountID int64, modelID string) bool {
	if s == nil || accountID <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return false
	}
	if pending := s.pending[accountID]; pending != nil {
		if modelID = strings.TrimSpace(modelID); modelID != "" {
			pending.model = modelID
		}
		if pending.running {
			pending.dirty = true
		}
		return true
	}
	s.pending[accountID] = &openAIResponsesProbePending{model: strings.TrimSpace(modelID)}
	select {
	case s.jobs <- accountID:
		return true
	default:
		delete(s.pending, accountID)
		slog.Warn("openai_responses_probe_queue_full", "account_id", accountID, "queue_size", s.options.QueueSize)
		return false
	}
}

func (s *OpenAIResponsesProbeScheduler) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case accountID := <-s.jobs:
			if s.ctx.Err() != nil {
				return
			}
			s.runAccount(accountID)
		}
	}
}

func (s *OpenAIResponsesProbeScheduler) runAccount(accountID int64) {
	for {
		s.mu.Lock()
		pending := s.pending[accountID]
		if pending == nil || s.stopped {
			s.mu.Unlock()
			return
		}
		pending.running = true
		pending.dirty = false
		modelID := pending.model
		s.mu.Unlock()

		if !waitForProbeDelay(s.ctx, randomDuration(s.options.TaskJitter)) {
			return
		}
		s.executeProbe(accountID, modelID)

		s.mu.Lock()
		pending = s.pending[accountID]
		if pending != nil && pending.dirty && !s.stopped {
			s.mu.Unlock()
			continue
		}
		delete(s.pending, accountID)
		s.mu.Unlock()
		return
	}
}

func (s *OpenAIResponsesProbeScheduler) executeProbe(accountID int64, modelID string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("openai_responses_probe_panic", "account_id", accountID, "recover", recovered)
		}
	}()
	taskCtx, cancel := context.WithTimeout(s.ctx, s.options.TaskTimeout)
	defer cancel()
	s.executor.ProbeOpenAIAPIKeyResponsesSupportForModel(taskCtx, accountID, modelID)
}

func waitForProbeDelay(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func randomDuration(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(max) + 1))
}

func (s *OpenAIResponsesProbeScheduler) backfillLoop() {
	defer s.wg.Done()
	s.RunBackfill(s.ctx)
	for {
		delay := s.options.BackfillInterval
		if jitter := s.options.BackfillJitter; jitter > 0 {
			delay = delay - jitter + randomDuration(2*jitter)
		}
		if delay <= 0 {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.RunBackfill(s.ctx)
		}
	}
}

func (s *OpenAIResponsesProbeScheduler) RunBackfill(ctx context.Context) {
	if s == nil || s.lister == nil || ctx == nil {
		return
	}
	for page := 1; ; page++ {
		accounts, result, err := s.lister.ListWithFilters(ctx, pagination.PaginationParams{
			Page:      page,
			PageSize:  openAIResponsesProbeBackfillPageSize,
			SortBy:    "id",
			SortOrder: pagination.SortOrderAsc,
		}, PlatformOpenAI, AccountTypeAPIKey, "", "", 0, "")
		if err != nil {
			slog.Warn("openai_responses_probe_backfill_list_failed", "page", page, "error", err)
			return
		}
		for i := range accounts {
			if needsOpenAIResponsesProbe(&accounts[i]) {
				if !s.Schedule(accounts[i].ID, "") {
					return
				}
			}
		}
		if result == nil || page >= result.Pages || len(accounts) == 0 {
			return
		}
	}
}

func needsOpenAIResponsesProbe(account *Account) bool {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return false
	}
	probeModel := selectResponsesProbeModel(account)
	_, responsesKnown := account.OpenAIResponsesSupportKnownForModel(probeModel)
	_, customToolsKnown := account.OpenAIResponsesCustomToolsSupportKnown()
	if !responsesKnown || !customToolsKnown {
		return true
	}
	if !strings.EqualFold(account.OpenAIResponsesCustomToolsProbeModel(), probeModel) {
		return true
	}
	probeTarget := account.OpenAIResponsesCustomToolsProbeTarget()
	return probeTarget == "" || probeTarget != account.OpenAIResponsesCustomToolsTargetFingerprint(probeModel)
}

func (s *OpenAIResponsesProbeScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}
