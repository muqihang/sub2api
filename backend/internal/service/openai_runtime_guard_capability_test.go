package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"

	"github.com/stretchr/testify/require"
)

func TestOpenAIRuntimeGuardCapabilityFixturesCaptureReasoningPolicy(t *testing.T) {
	repaired := openAIRuntimeGuardFixtureByID(t, "reasoning_effort_max_repaired_to_xhigh")
	requireOpenAIRuntimeGuardDecision(t, repaired, "repair", 1)
	require.NotNil(t, repaired.Expect.Repair)
	require.Equal(t, "reasoning_effort", repaired.Expect.Repair.Path)
	require.Equal(t, "max", repaired.Expect.Repair.From)
	require.Equal(t, "xhigh", repaired.Expect.Repair.To)
	require.Equal(t, "max", openAIRuntimeGuardFixtureRequest(t, repaired)["reasoning_effort"])
	require.Equal(t, "xhigh", openAIRuntimeGuardFixtureForward(t, repaired)["reasoning_effort"])

	blocked := openAIRuntimeGuardFixtureByID(t, "reasoning_effort_unknown_local_400")
	requireOpenAIRuntimeGuardDecision(t, blocked, "block", 0)
	require.Equal(t, 400, blocked.Expect.Status)
}

func TestOpenAIRuntimeGuardCapabilityFixturesCoverImageAndSchedulerBlocks(t *testing.T) {
	cases := []struct {
		id       string
		decision string
		status   int
		category string
	}{
		{"image_generation_disabled_by_group_local_block", "block", 403, "capability.image_generation_disabled_by_group"},
		{"native_image_request_no_oauth_basic_fallback", "block", 400, "capability.native_image_no_oauth_basic_fallback"},
		{"unsupported_oauth_model_profile_scheduler_reject", "scheduler_reject", 503, "capability.unsupported_oauth_model_profile"},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			fixture := openAIRuntimeGuardFixtureByID(t, tc.id)
			requireOpenAIRuntimeGuardDecision(t, fixture, tc.decision, 0)
			require.Equal(t, tc.status, fixture.Expect.Status)
			require.Equal(t, tc.category, fixture.Expect.Category)
		})
	}
}

func TestOpenAIRuntimeGuardCapabilityFixturesCaptureTokenInvalidation(t *testing.T) {
	fixture := openAIRuntimeGuardFixtureByID(t, "token_invalidated_account_terminal_needs_relogin")
	requireOpenAIRuntimeGuardDecision(t, fixture, "account_terminal", 0)
	require.NotNil(t, fixture.UpstreamError)
	require.Equal(t, 401, fixture.UpstreamError.Status)
	require.Equal(t, "token_invalidated", fixture.UpstreamError.Code)
	require.Equal(t, "needs_relogin", fixture.Expect.AccountState)
}

func TestOpenAIRuntimeGuardCapabilityFixturesCaptureContextPolicy(t *testing.T) {
	over := openAIRuntimeGuardFixtureByID(t, "obviously_over_context_local_shadow_decision")
	requireOpenAIRuntimeGuardDecision(t, over, "shadow_block", 0)
	require.NotNil(t, over.Expect.Context)
	require.Equal(t, "high", over.Expect.Context.Confidence)
	require.Greater(t, over.Expect.Context.EstimatedTokens, over.Expect.Context.LimitTokens)

	near := openAIRuntimeGuardFixtureByID(t, "near_boundary_context_uncertain_not_blocked")
	requireOpenAIRuntimeGuardDecision(t, near, "pass", 1)
	require.NotNil(t, near.Expect.Context)
	require.Equal(t, "uncertain", near.Expect.Context.Confidence)
	require.Less(t, near.Expect.Context.EstimatedTokens, near.Expect.Context.LimitTokens)
}

func TestOpenAIRuntimeGuardCapabilitySchedulerBypassesOAuthUnsupportedModelWithoutMapping(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30301)
	accounts := []Account{
		{
			ID:          3030101,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          3030102,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    9,
			GroupIDs:    []int64{groupID},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4-nano",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3030102), selection.Account.ID)
	require.Equal(t, AccountTypeAPIKey, selection.Account.Type)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIRuntimeGuardCapabilityAdvancedSchedulerBypassesOAuthUnsupportedModelWithoutMapping(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30302)
	accounts := []Account{
		{ID: 3030201, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}},
		{ID: 3030202, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 9, GroupIDs: []int64{groupID}},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4-nano",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3030202), selection.Account.ID)
	require.Equal(t, AccountTypeAPIKey, selection.Account.Type)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIRuntimeGuardCapabilityStickyOAuthUnsupportedModelIsBypassed(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30303)
	sessionHash := "unsupported-model-sticky"
	accounts := []Account{
		{ID: 3030301, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}},
		{ID: 3030302, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 9, GroupIDs: []int64{groupID}},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:" + sessionHash: 3030301}}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		sessionHash,
		"gpt-5.4-nano",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3030302), selection.Account.ID)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, int64(3030302), cache.sessionBindings["openai:"+sessionHash])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIRuntimeGuardCapabilityStickyOAuthNativeImageIsBypassed(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30304)
	sessionHash := "native-image-sticky"
	accounts := []Account{
		{ID: 3030401, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}},
		{ID: 3030402, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 9, GroupIDs: []int64{groupID}},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:" + sessionHash: 3030401}}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithSchedulerForImages(
		ctx,
		&groupID,
		sessionHash,
		"gpt-image-2",
		nil,
		OpenAIImagesCapabilityNative,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3030402), selection.Account.ID)
	require.Equal(t, AccountTypeAPIKey, selection.Account.Type)
	require.False(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIRuntimeGuardCapabilityExplicitNativeImageDoesNotFallbackToOAuthBasic(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30305)
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          3030501,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		}}},
		cache:              &schedulerTestGatewayCache{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithSchedulerForImages(
		ctx,
		&groupID,
		"",
		"gpt-image-2",
		nil,
		OpenAIImagesCapabilityNative,
	)

	require.Error(t, err)
	require.Nil(t, selection)
	require.ErrorContains(t, err, "compatible")
}

func TestOpenAIRuntimeGuardCapabilityPreviousResponseOAuthUnsupportedModelFallsBackToAPIKey(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30306)
	accounts := []Account{
		{ID: 3030601, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}},
		{ID: 3030602, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 9, GroupIDs: []int64{groupID}},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                newSchedulerTestOpenAIWSV2Config(),
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_oauth_unsupported_model", 3030601, time.Hour))

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_oauth_unsupported_model",
		"session_prev_oauth_unsupported",
		"gpt-5.4-nano",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3030602), selection.Account.ID)
	require.Equal(t, AccountTypeAPIKey, selection.Account.Type)
	require.False(t, decision.StickyPreviousHit)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIRuntimeGuardCapabilityBasicImageSelectsOAuthBridgeFallbackPath(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30307)
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          3030701,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		}}},
		cache:              &schedulerTestGatewayCache{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithSchedulerForImages(
		ctx,
		&groupID,
		"",
		"gpt-image-2",
		nil,
		OpenAIImagesCapabilityBasic,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3030701), selection.Account.ID)
	require.Equal(t, AccountTypeOAuth, selection.Account.Type)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIRuntimeGuardCapabilityBasicImageSelectsOAuthBridgeAdvancedScheduler(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30308)
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          3030801,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		}}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithSchedulerForImages(
		ctx,
		&groupID,
		"",
		"gpt-image-2",
		nil,
		OpenAIImagesCapabilityBasic,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3030801), selection.Account.ID)
	require.Equal(t, AccountTypeOAuth, selection.Account.Type)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIRuntimeGuardCapabilityBasicImageRejectsOAuthBridgeOverrideFalse(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30309)
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          3030901,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
			Extra:       map[string]any{featureKeyCodexImageGenerationBridge: false},
		}}},
		cache:              &schedulerTestGatewayCache{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithSchedulerForImages(
		ctx,
		&groupID,
		"",
		"gpt-image-2",
		nil,
		OpenAIImagesCapabilityBasic,
	)

	require.Error(t, err)
	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
}

func TestOpenAIRuntimeGuardCapabilityOnlyOAuthUnsupportedModelReturnsUnsupportedCapability(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30310)
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          3031001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		}}},
		cache:              &schedulerTestGatewayCache{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4-nano",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.Error(t, err)
	require.Nil(t, selection)
	var selectionErr *OpenAIRuntimeGuardSelectionError
	require.ErrorAs(t, err, &selectionErr)
	require.Equal(t, OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability, selectionErr.Code)
	require.Equal(t, openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthModel, selectionErr.Category)
}

func TestOpenAIRuntimeGuardCapabilityNoOAuthCandidateReturnsNoCompatibleAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30311)
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          3031101,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.1": "gpt-5.1"}},
		}}},
		cache:              &schedulerTestGatewayCache{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4-nano",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.Error(t, err)
	require.Nil(t, selection)
	var selectionErr *OpenAIRuntimeGuardSelectionError
	require.ErrorAs(t, err, &selectionErr)
	require.Equal(t, OpenAIRuntimeGuardErrorCodeNoCompatibleAccount, selectionErr.Code)
	require.Equal(t, openAIRuntimeGuardCapabilityCategoryNoCompatibleAccount, selectionErr.Category)
}

func TestOpenAIRuntimeGuardCapabilityAPIKeyPassthroughUnsupportedOAuthSeedModelStillSelects(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30312)
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          3031201,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		}}},
		cache:              &schedulerTestGatewayCache{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4-nano",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3031201), selection.Account.ID)
	require.Equal(t, AccountTypeAPIKey, selection.Account.Type)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIRuntimeGuardCapabilitySelectionErrorsAreStructured(t *testing.T) {
	t.Run("unsupported oauth capability", func(t *testing.T) {
		err := noAvailableOpenAISelectionErrorForRequest("gpt-5.4-nano", "", false, true)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		var selectionErr *OpenAIRuntimeGuardSelectionError
		require.ErrorAs(t, err, &selectionErr)
		require.Equal(t, OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability, selectionErr.Code)
		require.Equal(t, openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthModel, selectionErr.Category)
		require.Equal(t, "gpt-5.4-nano", selectionErr.Metadata["model"])
	})

	t.Run("no compatible account", func(t *testing.T) {
		err := noAvailableOpenAISelectionErrorForRequest("gpt-5.1", OpenAIImagesCapabilityBasic, false, false)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		var selectionErr *OpenAIRuntimeGuardSelectionError
		require.ErrorAs(t, err, &selectionErr)
		require.Equal(t, OpenAIRuntimeGuardErrorCodeNoCompatibleAccount, selectionErr.Code)
		require.Equal(t, openAIRuntimeGuardCapabilityCategoryNoCompatibleAccount, selectionErr.Category)
		require.Equal(t, "gpt-5.1", selectionErr.Metadata["model"])
		require.Equal(t, string(OpenAIImagesCapabilityBasic), selectionErr.Metadata["image_capability"])
	})

	t.Run("local policy block", func(t *testing.T) {
		blocked := &OpenAIFastBlockedError{Message: "blocked"}
		require.Equal(t, OpenAIRuntimeGuardErrorCodeLocalPolicyBlock, blocked.RuntimeGuardCode())
		require.Equal(t, openAIRuntimeGuardCapabilityCategoryLocalPolicyBlock, blocked.RuntimeGuardCategory())
	})
}
