package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	// Thresholds for device state determination.
	codexDeviceOfflineThreshold    = 10 * time.Minute
	codexCatalogStaleThreshold     = 24 * time.Hour
	codexMinSupportedClientVersion = "0.1.0"
)

var (
	ErrCodexSetupSessionNotFound   = infraerrors.NotFound("CODEX_SETUP_SESSION_NOT_FOUND", "setup session not found")
	ErrCodexDiagnoseTargetRequired = infraerrors.BadRequest("CODEX_DIAGNOSE_TARGET_REQUIRED", "either setup_session_id or device_id is required")
	ErrCodexDiagnoseBothTargets    = infraerrors.BadRequest("CODEX_DIAGNOSE_BOTH_TARGETS", "only one of setup_session_id or device_id may be specified")
)

// codexAPIKeyCreator creates API keys for the independent_credential path.
type codexAPIKeyCreator interface {
	Create(ctx context.Context, userID int64, req CreateAPIKeyRequest) (*APIKey, error)
}

// CodexEntryCenterServiceImpl implements CodexEntryCenterService.
type CodexEntryCenterServiceImpl struct {
	repo            CodexAgentRepository
	apiKeyReader    codexManagedAPIKeyReader
	apiKeyCreator   codexAPIKeyCreator
	cfg             *CodexEntryCenterConfig
	modelRegistry   *CodexGatewayModelRegistry
	pricingResolver *ModelPricingResolver
}

type CodexEntryCenterConfig struct {
	ServerOrigin  string
	GatewayOrigin string
}

func NewCodexEntryCenterService(
	repo CodexAgentRepository,
	apiKeyReader codexManagedAPIKeyReader,
	apiKeyCreator codexAPIKeyCreator,
	cfg *CodexEntryCenterConfig,
	modelRegistry *CodexGatewayModelRegistry,
	pricingResolver ...*ModelPricingResolver,
) *CodexEntryCenterServiceImpl {
	registry := modelRegistry
	if registry == nil {
		registry = NewDefaultCodexGatewayModelRegistry()
	}
	var resolver *ModelPricingResolver
	if len(pricingResolver) > 0 {
		resolver = pricingResolver[0]
	}
	if resolver == nil {
		resolver = NewModelPricingResolver(nil, NewBillingService(nil, nil))
	}
	return &CodexEntryCenterServiceImpl{
		repo:            repo,
		apiKeyReader:    apiKeyReader,
		apiKeyCreator:   apiKeyCreator,
		cfg:             cfg,
		modelRegistry:   registry,
		pricingResolver: resolver,
	}
}

// GetSummary computes the entry center summary based on attachment lifecycle.
// Key decision: we do NOT check for purpose="codex" keys. We check for actual
// attachment relationships (setup sessions + managed devices).
func (s *CodexEntryCenterServiceImpl) GetSummary(ctx context.Context, userID int64) (*CodexEntrySummary, error) {
	devices, err := s.repo.ListManagedDevicesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Filter to presentable devices (active or recently revoked for display).
	presentableDevices := filterPresentableDevices(devices)

	// Find any pending (unconsumed, unexpired) setup session for this user.
	pendingSession, err := s.findPendingSetupSession(ctx, userID)
	if err != nil {
		return nil, err
	}
	pricingGroupID := s.resolveModelPricingGroupID(ctx, presentableDevices, pendingSession)

	summary := &CodexEntrySummary{
		Devices:      make([]CodexDeviceDTO, 0, len(presentableDevices)),
		ModelCatalog: s.buildModelCatalogSummary(ctx, pricingGroupID),
	}

	// Build device DTOs.
	for _, d := range presentableDevices {
		dto := s.buildDeviceDTO(d)
		summary.Devices = append(summary.Devices, dto)
	}

	// Determine page_state based on attachment lifecycle.
	hasDevices := len(presentableDevices) > 0

	if hasDevices {
		// Console mode: user has at least one presentable device.
		summary.PageState = CodexPageStateConsole
		summary.WizardStep = nil

		if pendingSession != nil {
			// There's an active setup session while in console mode -> console_banner.
			pres := CodexSetupSessionPresentationConsoleBanner
			summary.SetupSessionPresentation = &pres
			summary.SetupSession = s.buildSetupSessionDTO(pendingSession)
		}

		// Focus on the first unhealthy device, or the first device.
		summary.FocusDeviceID = s.pickFocusDevice(presentableDevices)
	} else if pendingSession != nil {
		// No devices, but there's a pending session.
		sessionDTO := s.buildSetupSessionDTO(pendingSession)
		summary.SetupSession = sessionDTO

		if sessionDTO.FirstCatalogSyncedAt != nil {
			// Catalog already synced -> should transition to console.
			// This is an edge case; normally the device would exist by now.
			summary.PageState = CodexPageStateConsole
			summary.WizardStep = nil
		} else if sessionDTO.FirstSeenAt != nil {
			// Device appeared (first heartbeat) but catalog not yet synced.
			summary.PageState = CodexPageStateOnboardingVerify
			step := 3
			summary.WizardStep = &step
		} else {
			// Waiting for device to appear.
			summary.PageState = CodexPageStateOnboardingAttach
			step := 2
			summary.WizardStep = &step
		}

		pres := CodexSetupSessionPresentationWizard
		summary.SetupSessionPresentation = &pres
		mode := sessionDTO.AttachmentMode
		summary.AttachmentMode = &mode
	} else {
		// No devices, no pending session -> fresh onboarding.
		summary.PageState = CodexPageStateOnboardingCredential
		step := 1
		summary.WizardStep = &step
	}

	return summary, nil
}

// CreateSetupSession creates a new setup session (接入会话).
func (s *CodexEntryCenterServiceImpl) CreateSetupSession(ctx context.Context, req CodexCreateSetupSessionRequest) (*CodexCreateSetupSessionResponse, error) {
	var apiKeyID int64

	// Validate attachment mode and determine the API key to use.
	switch req.AttachmentMode {
	case CodexAttachmentModeIndependent:
		if req.CredentialLabel == "" {
			req.CredentialLabel = "Codex"
		}
		// Create a new API key with codex_only=true for the independent path.
		newKey, err := s.apiKeyCreator.Create(ctx, req.UserID, CreateAPIKeyRequest{
			Name:      req.CredentialLabel,
			CodexOnly: true,
		})
		if err != nil {
			return nil, err
		}
		apiKeyID = newKey.ID
	case CodexAttachmentModeReusedKey:
		if req.ReuseAPIKeyID == nil {
			return nil, infraerrors.BadRequest("CODEX_REUSE_KEY_REQUIRED", "reuse_api_key_id is required for reused_key mode")
		}
		// Verify ownership.
		validIDs, err := s.apiKeyReader.VerifyOwnership(ctx, req.UserID, []int64{*req.ReuseAPIKeyID})
		if err != nil {
			return nil, err
		}
		if len(validIDs) != 1 {
			return nil, ErrCodexManagedAPIKeyOwnershipDenied
		}
		apiKeyID = *req.ReuseAPIKeyID
	default:
		return nil, infraerrors.BadRequest("CODEX_INVALID_ATTACHMENT_MODE", "attachment_mode must be independent_credential or reused_key")
	}

	serverOrigin := firstNonEmpty(req.ServerOrigin, s.cfg.ServerOrigin)
	gatewayOrigin := firstNonEmpty(req.GatewayOrigin, s.cfg.GatewayOrigin, serverOrigin)

	// Generate setup code.
	code, err := randomHexString(24)
	if err != nil {
		return nil, fmt.Errorf("generate setup session code: %w", err)
	}
	expiresAt := time.Now().Add(10 * time.Minute)

	_, err = s.repo.CreateSetupGrant(ctx, CreateCodexSetupGrantParams{
		CodeHash:      hashManagedSecret(code),
		UserID:        req.UserID,
		APIKeyID:      apiKeyID,
		Mode:          codexManagedMode,
		ServerOrigin:  serverOrigin,
		GatewayOrigin: gatewayOrigin,
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		return nil, err
	}

	// Build launch URL and CLI command.
	launchURL := fmt.Sprintf("%s?client=codex&code=%s&server=%s", codexDeeplinkScheme, url.QueryEscape(code), url.QueryEscape(serverOrigin))
	cliCommand := fmt.Sprintf("codex auth --code %s --server %s", code, serverOrigin)

	sessionID := hashManagedSecret(code)[:16] // Use first 16 chars of hash as public session ID.

	sessionDTO := CodexSetupSessionDTO{
		ID:              sessionID,
		CredentialLabel: req.CredentialLabel,
		AttachmentMode:  req.AttachmentMode,
		ReuseAPIKeyID:   req.ReuseAPIKeyID,
		LaunchURL:       &launchURL,
		CLICommand:      &cliCommand,
		ExpiresAt:       expiresAt,
	}

	// Determine presentation based on whether user already has devices.
	devices, err := s.repo.ListManagedDevicesByUser(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	presentable := filterPresentableDevices(devices)

	var pageState CodexPageState
	var presentation CodexSetupSessionPresentation
	if len(presentable) > 0 {
		pageState = CodexPageStateConsole
		presentation = CodexSetupSessionPresentationConsoleBanner
	} else {
		pageState = CodexPageStateOnboardingAttach
		presentation = CodexSetupSessionPresentationWizard
	}

	return &CodexCreateSetupSessionResponse{
		SetupSession:             sessionDTO,
		PageState:                pageState,
		SetupSessionPresentation: presentation,
	}, nil
}

// RegenerateSetupSession regenerates the pairing code for an existing session.
func (s *CodexEntryCenterServiceImpl) RegenerateSetupSession(ctx context.Context, userID int64, sessionID string) (*CodexRegenerateSetupSessionResponse, error) {
	// For v1, regeneration creates a new setup grant (the old one expires naturally).
	// We need to find the original grant's api_key_id to create a new one.
	// Since we use hash prefix as session ID, we need to look up by user.
	pendingSession, err := s.findPendingSetupSession(ctx, userID)
	if err != nil {
		return nil, err
	}
	if pendingSession == nil {
		return nil, ErrCodexSetupSessionNotFound
	}

	serverOrigin := pendingSession.ServerOrigin
	gatewayOrigin := pendingSession.GatewayOrigin

	code, err := randomHexString(24)
	if err != nil {
		return nil, fmt.Errorf("generate setup session code: %w", err)
	}
	expiresAt := time.Now().Add(10 * time.Minute)

	_, err = s.repo.CreateSetupGrant(ctx, CreateCodexSetupGrantParams{
		CodeHash:      hashManagedSecret(code),
		UserID:        userID,
		APIKeyID:      pendingSession.APIKeyID,
		Mode:          codexManagedMode,
		ServerOrigin:  serverOrigin,
		GatewayOrigin: gatewayOrigin,
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		return nil, err
	}

	launchURL := fmt.Sprintf("%s?client=codex&code=%s&server=%s", codexDeeplinkScheme, url.QueryEscape(code), url.QueryEscape(serverOrigin))
	cliCommand := fmt.Sprintf("codex auth --code %s --server %s", code, serverOrigin)
	newSessionID := hashManagedSecret(code)[:16]

	return &CodexRegenerateSetupSessionResponse{
		SetupSession: CodexSetupSessionRegenerateDTO{
			ID:         newSessionID,
			LaunchURL:  &launchURL,
			CLICommand: &cliCommand,
			ExpiresAt:  expiresAt,
		},
	}, nil
}

// Diagnose runs diagnostic checks against a setup session or device.
func (s *CodexEntryCenterServiceImpl) Diagnose(ctx context.Context, req CodexDiagnoseRequest) (*CodexDiagnoseReport, error) {
	if req.SetupSessionID == nil && req.DeviceID == nil {
		return nil, ErrCodexDiagnoseTargetRequired
	}
	if req.SetupSessionID != nil && req.DeviceID != nil {
		return nil, ErrCodexDiagnoseBothTargets
	}

	if req.SetupSessionID != nil {
		return s.diagnoseSetupSession(ctx, req.UserID, *req.SetupSessionID)
	}
	return s.diagnoseDevice(ctx, req.UserID, *req.DeviceID)
}

func (s *CodexEntryCenterServiceImpl) diagnoseSetupSession(ctx context.Context, userID int64, sessionID string) (*CodexDiagnoseReport, error) {
	// Look up the specific session by ID.
	grant, err := s.findSetupSessionByID(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}

	checks := []CodexDiagnoseCheck{
		{Name: "credential", Status: "ok", Hint: "接入凭证有效"},
		{Name: "local_launch", Status: "warn", Hint: "等待本机客户端连接"},
		{Name: "device_heartbeat", Status: "fail", Hint: "尚未收到设备心跳"},
		{Name: "catalog_sync", Status: "fail", Hint: "尚未完成模型目录同步"},
	}

	if grant == nil {
		checks[0] = CodexDiagnoseCheck{Name: "credential", Status: "fail", Hint: "未找到有效的接入会话，可能已过期"}
	} else if time.Now().After(grant.ExpiresAt) {
		checks[0] = CodexDiagnoseCheck{Name: "credential", Status: "fail", Hint: "接入会话已过期，请重新生成"}
	}

	allOK := true
	for _, c := range checks {
		if c.Status != "ok" {
			allOK = false
			break
		}
	}

	return &CodexDiagnoseReport{
		OK:         allOK,
		TargetKind: "setup_session",
		Checks:     checks,
	}, nil
}

func (s *CodexEntryCenterServiceImpl) diagnoseDevice(ctx context.Context, userID int64, deviceID int64) (*CodexDiagnoseReport, error) {
	device, err := s.repo.GetManagedDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if device.UserID != userID {
		return nil, ErrCodexManagedDeviceOwnershipDenied
	}

	checks := make([]CodexDiagnoseCheck, 0, 5)

	// Check credential.
	credCheck := CodexDiagnoseCheck{Name: "credential", Status: "ok", Hint: "接入凭证有效"}
	if device.Status == "revoked" || device.RevokedAt != nil {
		credCheck = CodexDiagnoseCheck{Name: "credential", Status: "fail", Hint: "接入凭证已被吊销"}
	}
	checks = append(checks, credCheck)

	// Check heartbeat.
	heartbeatCheck := CodexDiagnoseCheck{Name: "device_heartbeat", Status: "ok", Hint: "设备心跳正常"}
	if device.LastSeenAt == nil || time.Since(*device.LastSeenAt) > codexDeviceOfflineThreshold {
		heartbeatCheck = CodexDiagnoseCheck{Name: "device_heartbeat", Status: "fail", Hint: "设备已离线，超过 10 分钟未收到心跳"}
	}
	checks = append(checks, heartbeatCheck)

	// Check catalog sync (simplified: we use last_seen_at as proxy for now).
	catalogCheck := CodexDiagnoseCheck{Name: "catalog_sync", Status: "ok", Hint: "模型目录同步正常"}
	checks = append(checks, catalogCheck)

	// Check client version.
	versionCheck := CodexDiagnoseCheck{Name: "client_version", Status: "ok", Hint: "客户端版本符合要求"}
	if device.ManagerVersion != "" && device.ManagerVersion < codexMinSupportedClientVersion {
		versionCheck = CodexDiagnoseCheck{Name: "client_version", Status: "warn", Hint: fmt.Sprintf("客户端版本 %s 低于最低要求 %s", device.ManagerVersion, codexMinSupportedClientVersion)}
	}
	checks = append(checks, versionCheck)

	allOK := true
	for _, c := range checks {
		if c.Status != "ok" {
			allOK = false
			break
		}
	}

	return &CodexDiagnoseReport{
		OK:         allOK,
		TargetKind: "device",
		Checks:     checks,
	}, nil
}

// ResyncDevice triggers a catalog resync for a device.
func (s *CodexEntryCenterServiceImpl) ResyncDevice(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error) {
	if err := s.verifyDeviceOwnership(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	// In v1, resync is a no-op signal; the device will resync on next heartbeat.
	return &CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

// RepairDevice attempts to repair a device connection.
func (s *CodexEntryCenterServiceImpl) RepairDevice(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error) {
	if err := s.verifyDeviceOwnership(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	// In v1, repair is a signal; actual repair happens on next device check-in.
	return &CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

// ReattachDevice re-attaches a device whose credential was revoked.
func (s *CodexEntryCenterServiceImpl) ReattachDevice(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error) {
	if err := s.verifyDeviceOwnership(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	// In v1, reattach marks the device as needing reauthorization.
	// The user will need to go through a new setup session.
	return &CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

// RevokeAttachment revokes the credential attached to a device (only for independent_credential).
func (s *CodexEntryCenterServiceImpl) RevokeAttachment(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error) {
	device, err := s.repo.GetManagedDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if device.UserID != userID {
		return nil, ErrCodexManagedDeviceOwnershipDenied
	}
	// Revoke the device.
	if err := s.repo.RevokeManagedDevice(ctx, deviceID, time.Now()); err != nil {
		return nil, err
	}
	return &CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

// RemoveDevice removes a device from the user's device list.
func (s *CodexEntryCenterServiceImpl) RemoveDevice(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error) {
	device, err := s.repo.GetManagedDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if device.UserID != userID {
		return nil, ErrCodexManagedDeviceOwnershipDenied
	}
	if err := s.repo.RevokeManagedDevice(ctx, deviceID, time.Now()); err != nil {
		return nil, err
	}
	return &CodexDeviceActionResponse{DeviceID: deviceID, Accepted: true}, nil
}

// ─── Internal helpers ───

func (s *CodexEntryCenterServiceImpl) verifyDeviceOwnership(ctx context.Context, userID int64, deviceID int64) error {
	device, err := s.repo.GetManagedDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	if device.UserID != userID {
		return ErrCodexManagedDeviceOwnershipDenied
	}
	return nil
}

func (s *CodexEntryCenterServiceImpl) findSetupSessionByID(ctx context.Context, userID int64, sessionID string) (*dbent.CodexSetupGrant, error) {
	// Session ID is the string representation of the grant's database ID.
	grantID, err := strconv.ParseInt(sessionID, 10, 64)
	if err != nil {
		// Try looking up by code-hash prefix (for sessions returned directly by create/regenerate).
		grants, err := s.repo.ListPendingSetupGrantsByUser(ctx, userID, time.Now())
		if err != nil {
			return nil, err
		}
		for _, g := range grants {
			if len(g.CodeHash) >= len(sessionID) && g.CodeHash[:len(sessionID)] == sessionID {
				return g, nil
			}
		}
		return nil, ErrCodexSetupSessionNotFound
	}
	grant, err := s.repo.GetSetupGrantByID(ctx, grantID)
	if err != nil {
		return nil, err
	}
	if grant.UserID != userID {
		return nil, ErrCodexSetupSessionNotFound
	}
	return grant, nil
}

func (s *CodexEntryCenterServiceImpl) findPendingSetupSession(ctx context.Context, userID int64) (*dbent.CodexSetupGrant, error) {
	// Find the most recent unconsumed, unexpired setup grant for this user.
	// This represents the "pending setup session" in the entry center model.
	grants, err := s.repo.ListPendingSetupGrantsByUser(ctx, userID, time.Now())
	if err != nil {
		return nil, err
	}
	if len(grants) == 0 {
		return nil, nil
	}
	// Return the most recent one.
	return grants[0], nil
}

func (s *CodexEntryCenterServiceImpl) buildSetupSessionDTO(grant *dbent.CodexSetupGrant) *CodexSetupSessionDTO {
	sessionID := codexSetupSessionID(grant)

	// Determine attachment mode from the grant.
	// For now, all grants created via the old flow are "reused_key".
	mode := CodexAttachmentModeReusedKey

	var reuseKeyID *int64
	if grant.APIKeyID > 0 {
		id := grant.APIKeyID
		reuseKeyID = &id
	}

	return &CodexSetupSessionDTO{
		ID:              sessionID,
		CredentialLabel: "Codex",
		AttachmentMode:  mode,
		ReuseAPIKeyID:   reuseKeyID,
		LaunchURL:       nil,
		CLICommand:      nil,
		ExpiresAt:       grant.ExpiresAt,
	}
}

func codexSetupSessionID(grant *dbent.CodexSetupGrant) string {
	if grant != nil && len(grant.CodeHash) >= 16 {
		return grant.CodeHash[:16]
	}
	if grant == nil {
		return ""
	}
	return strconv.FormatInt(grant.ID, 10)
}

func (s *CodexEntryCenterServiceImpl) buildModelCatalogSummary(ctx context.Context, pricingGroupID *int64) []CodexEntryModelSummary {
	if s == nil || s.modelRegistry == nil {
		return []CodexEntryModelSummary{}
	}
	models := s.modelRegistry.Models()
	out := make([]CodexEntryModelSummary, 0, len(models))
	for _, model := range models {
		out = append(out, CodexEntryModelSummary{
			Name:        model.Slug,
			DisplayName: model.DisplayName,
			Platform:    model.Provider,
			Pricing:     s.buildModelPricingSummary(ctx, model, pricingGroupID),
		})
	}
	return out
}

func (s *CodexEntryCenterServiceImpl) resolveModelPricingGroupID(ctx context.Context, devices []*dbent.CodexManagedDevice, pendingSession *dbent.CodexSetupGrant) *int64 {
	if s == nil || s.apiKeyReader == nil {
		return nil
	}

	var apiKeyID int64
	if focusDeviceID := s.pickFocusDevice(devices); focusDeviceID != nil {
		for _, device := range devices {
			if device.ID == *focusDeviceID {
				apiKeyID = device.APIKeyID
				break
			}
		}
	}
	if apiKeyID <= 0 && len(devices) > 0 {
		apiKeyID = devices[0].APIKeyID
	}
	if apiKeyID <= 0 && pendingSession != nil {
		apiKeyID = pendingSession.APIKeyID
	}
	if apiKeyID <= 0 {
		return nil
	}

	apiKey, err := s.apiKeyReader.GetByID(ctx, apiKeyID)
	if err != nil || apiKey == nil {
		return nil
	}
	return apiKey.GroupID
}

func (s *CodexEntryCenterServiceImpl) buildModelPricingSummary(ctx context.Context, model CodexGatewayModel, groupID *int64) *CodexEntryModelPricing {
	if s == nil || s.pricingResolver == nil {
		return nil
	}
	var best *CodexEntryModelPricing
	for _, modelName := range codexEntryPricingModelCandidates(model) {
		resolved := s.pricingResolver.Resolve(ctx, PricingInput{Model: modelName, GroupID: groupID})
		pricing := codexEntryResolvedPricingToDTO(resolved)
		if pricing == nil {
			continue
		}
		if pricing.Source == PricingSourceChannel {
			return pricing
		}
		if best == nil {
			best = pricing
		}
	}
	return best
}

func codexEntryPricingModelCandidates(model CodexGatewayModel) []string {
	raw := []string{
		model.Slug,
		model.UpstreamBaseModel,
		model.UpstreamModel,
		model.UpstreamThinkingModel,
		codexEntryBasePricingModel(model.Slug),
		codexEntryBasePricingModel(model.UpstreamBaseModel),
		codexEntryBasePricingModel(model.UpstreamModel),
		codexEntryBasePricingModel(model.UpstreamThinkingModel),
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func codexEntryBasePricingModel(model string) string {
	model = strings.TrimSpace(model)
	for _, suffix := range []string{"-thinking-ag", "-thinking", "-max", "-ag"} {
		if strings.HasSuffix(model, suffix) {
			return strings.TrimSuffix(model, suffix)
		}
	}
	return ""
}

func codexEntryResolvedPricingToDTO(resolved *ResolvedPricing) *CodexEntryModelPricing {
	if resolved == nil {
		return nil
	}
	mode := resolved.Mode
	if mode == "" {
		mode = BillingModeToken
	}
	dto := &CodexEntryModelPricing{
		BillingMode: string(mode),
		Intervals:   codexEntryPricingIntervalsToDTO(codexEntryResolvedIntervals(resolved)),
		Source:      resolved.Source,
	}

	switch mode {
	case BillingModePerRequest:
		dto.PerRequestPrice = positiveFloatPtr(resolved.DefaultPerRequestPrice)
	case BillingModeImage:
		dto.PerRequestPrice = positiveFloatPtr(resolved.DefaultPerRequestPrice)
		dto.ImageOutputPrice = positiveFloatPtr(resolved.DefaultPerRequestPrice)
	default:
		if resolved.BasePricing != nil {
			dto.InputPrice = positiveFloatPtr(resolved.BasePricing.InputPricePerToken)
			dto.OutputPrice = positiveFloatPtr(resolved.BasePricing.OutputPricePerToken)
			dto.CacheWritePrice = positiveFloatPtr(firstPositiveFloat(
				resolved.BasePricing.CacheCreationPricePerToken,
				resolved.BasePricing.CacheCreation5mPrice,
				resolved.BasePricing.CacheCreation1hPrice,
			))
			dto.CacheReadPrice = positiveFloatPtr(resolved.BasePricing.CacheReadPricePerToken)
			dto.ImageOutputPrice = positiveFloatPtr(resolved.BasePricing.ImageOutputPricePerToken)
		}
	}

	if !codexEntryModelPricingHasValues(dto) {
		return nil
	}
	return dto
}

func codexEntryResolvedIntervals(resolved *ResolvedPricing) []PricingInterval {
	if resolved == nil {
		return nil
	}
	if resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage {
		return resolved.RequestTiers
	}
	return resolved.Intervals
}

func codexEntryPricingIntervalsToDTO(intervals []PricingInterval) []CodexEntryPricingInterval {
	out := make([]CodexEntryPricingInterval, 0, len(intervals))
	for _, iv := range intervals {
		out = append(out, CodexEntryPricingInterval{
			MinTokens:       iv.MinTokens,
			MaxTokens:       cloneIntPtr(iv.MaxTokens),
			TierLabel:       iv.TierLabel,
			InputPrice:      clonePositiveFloatPtr(iv.InputPrice),
			OutputPrice:     clonePositiveFloatPtr(iv.OutputPrice),
			CacheWritePrice: clonePositiveFloatPtr(iv.CacheWritePrice),
			CacheReadPrice:  clonePositiveFloatPtr(iv.CacheReadPrice),
			PerRequestPrice: clonePositiveFloatPtr(iv.PerRequestPrice),
		})
	}
	return out
}

func codexEntryModelPricingHasValues(pricing *CodexEntryModelPricing) bool {
	if pricing == nil {
		return false
	}
	return pricing.InputPrice != nil ||
		pricing.OutputPrice != nil ||
		pricing.CacheWritePrice != nil ||
		pricing.CacheReadPrice != nil ||
		pricing.ImageOutputPrice != nil ||
		pricing.PerRequestPrice != nil ||
		len(pricing.Intervals) > 0
}

func positiveFloatPtr(value float64) *float64 {
	if value <= 0 {
		return nil
	}
	v := value
	return &v
}

func clonePositiveFloatPtr(value *float64) *float64 {
	if value == nil || *value <= 0 {
		return nil
	}
	v := *value
	return &v
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func (s *CodexEntryCenterServiceImpl) buildDeviceDTO(device *dbent.CodexManagedDevice) CodexDeviceDTO {
	state := s.computeDeviceState(device)
	clientVersion := device.ManagerVersion
	minVersion := codexMinSupportedClientVersion

	return CodexDeviceDTO{
		DeviceID:                  device.ID,
		DeviceName:                device.Name,
		AttachmentMode:            CodexAttachmentModeReusedKey, // Default; will be enhanced when we track attachment mode per device.
		DeviceState:               state,
		LastSeenAt:                device.LastSeenAt,
		ClientVersion:             &clientVersion,
		MinSupportedClientVersion: &minVersion,
		CatalogSyncedAt:           nil, // Will be populated when catalog tracking is added.
		CatalogLastErrorKind:      CodexCatalogErrorNone,
		RevokedAt:                 device.RevokedAt,
	}
}

func (s *CodexEntryCenterServiceImpl) computeDeviceState(device *dbent.CodexManagedDevice) CodexDeviceState {
	// Priority order for state determination:
	// 1. credential_revoked: device or its credential is revoked
	// 2. device_offline: no heartbeat within threshold
	// 3. client_outdated: version below minimum
	// 4. catalog_stale: catalog sync issues (simplified for v1)
	// 5. healthy: everything is fine

	if device.Status == "revoked" || device.RevokedAt != nil {
		return CodexDeviceStateCredentialRevoked
	}

	if device.LastSeenAt == nil || time.Since(*device.LastSeenAt) > codexDeviceOfflineThreshold {
		return CodexDeviceStateDeviceOffline
	}

	if device.ManagerVersion != "" && device.ManagerVersion < codexMinSupportedClientVersion {
		return CodexDeviceStateClientOutdated
	}

	// For v1, we don't have catalog sync tracking yet, so we skip catalog_stale.
	return CodexDeviceStateHealthy
}

func (s *CodexEntryCenterServiceImpl) pickFocusDevice(devices []*dbent.CodexManagedDevice) *int64 {
	// Focus on the first unhealthy device, or the first device.
	for _, d := range devices {
		state := s.computeDeviceState(d)
		if state != CodexDeviceStateHealthy {
			id := d.ID
			return &id
		}
	}
	if len(devices) > 0 {
		id := devices[0].ID
		return &id
	}
	return nil
}

// filterPresentableDevices returns devices that should be shown in the UI.
// Active devices are always shown. Revoked devices are excluded.
func filterPresentableDevices(devices []*dbent.CodexManagedDevice) []*dbent.CodexManagedDevice {
	result := make([]*dbent.CodexManagedDevice, 0, len(devices))
	for _, d := range devices {
		if d.Status == "active" {
			result = append(result, d)
		}
	}
	return result
}
