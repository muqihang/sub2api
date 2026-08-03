package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/google/wire"
)

// ProvideAdminHandlers creates the AdminHandlers struct
func ProvideAdminHandlers(
	dashboardHandler *admin.DashboardHandler,
	userHandler *admin.UserHandler,
	groupHandler *admin.GroupHandler,
	accountHandler *admin.AccountHandler,
	announcementHandler *admin.AnnouncementHandler,
	dataManagementHandler *admin.DataManagementHandler,
	backupHandler *admin.BackupHandler,
	oauthHandler *admin.OAuthHandler,
	openaiOAuthHandler *admin.OpenAIOAuthHandler,
	geminiOAuthHandler *admin.GeminiOAuthHandler,
	geminiHealthHandler *admin.GeminiHealthHandler,
	antigravityOAuthHandler *admin.AntigravityOAuthHandler,
	grokOAuthHandler *admin.GrokOAuthHandler,
	proxyHandler *admin.ProxyHandler,
	redeemHandler *admin.RedeemHandler,
	promoHandler *admin.PromoHandler,
	settingHandler *admin.SettingHandler,
	opsHandler *admin.OpsHandler,
	systemHandler *admin.SystemHandler,
	subscriptionHandler *admin.SubscriptionHandler,
	usageHandler *admin.UsageHandler,
	userAttributeHandler *admin.UserAttributeHandler,
	errorPassthroughHandler *admin.ErrorPassthroughHandler,
	tlsFingerprintProfileHandler *admin.TLSFingerprintProfileHandler,
	apiKeyHandler *admin.AdminAPIKeyHandler,
	entityHandler *admin.EntityHandler,
	scheduledTestHandler *admin.ScheduledTestHandler,
	channelHandler *admin.ChannelHandler,
	channelMonitorHandler *admin.ChannelMonitorHandler,
	channelMonitorTemplateHandler *admin.ChannelMonitorRequestTemplateHandler,
	contentModerationHandler *admin.ContentModerationHandler,
	paymentHandler *admin.PaymentHandler,
	affiliateHandler *admin.AffiliateHandler,
	augmentGatewayHandler *admin.AugmentGatewayHandler,
	codexGatewayHandler *admin.CodexGatewayHandler,
	formalPoolOnboardingHandler *admin.FormalPoolOnboardingHandler,
	formalPoolOperationsHandler *admin.FormalPoolOperationsHandler,
	complianceHandlers ...*admin.ComplianceHandler,
) *AdminHandlers {
	var complianceHandler *admin.ComplianceHandler
	if len(complianceHandlers) > 0 {
		complianceHandler = complianceHandlers[0]
	}
	return &AdminHandlers{
		Dashboard:              dashboardHandler,
		User:                   userHandler,
		Group:                  groupHandler,
		Account:                accountHandler,
		Announcement:           announcementHandler,
		DataManagement:         dataManagementHandler,
		Backup:                 backupHandler,
		OAuth:                  oauthHandler,
		OpenAIOAuth:            openaiOAuthHandler,
		GeminiOAuth:            geminiOAuthHandler,
		GeminiHealth:           geminiHealthHandler,
		AntigravityOAuth:       antigravityOAuthHandler,
		GrokOAuth:              grokOAuthHandler,
		Proxy:                  proxyHandler,
		Redeem:                 redeemHandler,
		Promo:                  promoHandler,
		Setting:                settingHandler,
		Ops:                    opsHandler,
		System:                 systemHandler,
		Subscription:           subscriptionHandler,
		Usage:                  usageHandler,
		UserAttribute:          userAttributeHandler,
		ErrorPassthrough:       errorPassthroughHandler,
		TLSFingerprintProfile:  tlsFingerprintProfileHandler,
		APIKey:                 apiKeyHandler,
		Entity:                 entityHandler,
		ScheduledTest:          scheduledTestHandler,
		Channel:                channelHandler,
		ChannelMonitor:         channelMonitorHandler,
		ChannelMonitorTemplate: channelMonitorTemplateHandler,
		ContentModeration:      contentModerationHandler,
		Payment:                paymentHandler,
		Affiliate:              affiliateHandler,
		AugmentGateway:         augmentGatewayHandler,
		CodexGateway:           codexGatewayHandler,
		FormalPoolOnboarding:   formalPoolOnboardingHandler,
		FormalPoolOperations:   formalPoolOperationsHandler,
		Compliance:             complianceHandler,
	}
}

func ProvideAdminHandlersForWire(
	dashboardHandler *admin.DashboardHandler,
	userHandler *admin.UserHandler,
	groupHandler *admin.GroupHandler,
	accountHandler *admin.AccountHandler,
	announcementHandler *admin.AnnouncementHandler,
	dataManagementHandler *admin.DataManagementHandler,
	backupHandler *admin.BackupHandler,
	oauthHandler *admin.OAuthHandler,
	openaiOAuthHandler *admin.OpenAIOAuthHandler,
	geminiOAuthHandler *admin.GeminiOAuthHandler,
	geminiHealthHandler *admin.GeminiHealthHandler,
	antigravityOAuthHandler *admin.AntigravityOAuthHandler,
	grokOAuthHandler *admin.GrokOAuthHandler,
	proxyHandler *admin.ProxyHandler,
	redeemHandler *admin.RedeemHandler,
	promoHandler *admin.PromoHandler,
	settingHandler *admin.SettingHandler,
	opsHandler *admin.OpsHandler,
	systemHandler *admin.SystemHandler,
	subscriptionHandler *admin.SubscriptionHandler,
	usageHandler *admin.UsageHandler,
	userAttributeHandler *admin.UserAttributeHandler,
	errorPassthroughHandler *admin.ErrorPassthroughHandler,
	tlsFingerprintProfileHandler *admin.TLSFingerprintProfileHandler,
	apiKeyHandler *admin.AdminAPIKeyHandler,
	entityHandler *admin.EntityHandler,
	scheduledTestHandler *admin.ScheduledTestHandler,
	channelHandler *admin.ChannelHandler,
	channelMonitorHandler *admin.ChannelMonitorHandler,
	channelMonitorTemplateHandler *admin.ChannelMonitorRequestTemplateHandler,
	contentModerationHandler *admin.ContentModerationHandler,
	paymentHandler *admin.PaymentHandler,
	affiliateHandler *admin.AffiliateHandler,
	augmentGatewayHandler *admin.AugmentGatewayHandler,
	codexGatewayHandler *admin.CodexGatewayHandler,
	formalPoolOnboardingHandler *admin.FormalPoolOnboardingHandler,
	formalPoolOperationsHandler *admin.FormalPoolOperationsHandler,
	complianceHandler *admin.ComplianceHandler,
) *AdminHandlers {
	return ProvideAdminHandlers(
		dashboardHandler, userHandler, groupHandler, accountHandler, announcementHandler,
		dataManagementHandler, backupHandler, oauthHandler, openaiOAuthHandler, geminiOAuthHandler,
		geminiHealthHandler, antigravityOAuthHandler, grokOAuthHandler, proxyHandler, redeemHandler,
		promoHandler, settingHandler, opsHandler, systemHandler, subscriptionHandler, usageHandler,
		userAttributeHandler, errorPassthroughHandler, tlsFingerprintProfileHandler, apiKeyHandler,
		entityHandler, scheduledTestHandler, channelHandler, channelMonitorHandler,
		channelMonitorTemplateHandler, contentModerationHandler, paymentHandler, affiliateHandler,
		augmentGatewayHandler, codexGatewayHandler, formalPoolOnboardingHandler,
		formalPoolOperationsHandler, complianceHandler,
	)
}

// ProvideSystemHandler creates admin.SystemHandler with UpdateService
func ProvideSystemHandler(updateService *service.UpdateService, lockService *service.SystemOperationLockService) *admin.SystemHandler {
	return admin.NewSystemHandler(updateService, lockService)
}

func ProvideAugmentGatewayHandler(
	settingsSvc *service.AugmentGatewayAdminService,
	sessionSvc *service.AugmentOfficialPoolSessionService,
	usageSvc *service.AugmentGatewayUsageService,
) *admin.AugmentGatewayHandler {
	return admin.NewAugmentGatewayHandler(settingsSvc, sessionSvc, usageSvc)
}

func ProvideCodexGatewayAdminHandler(
	adminSvc *service.CodexGatewayAdminService,
) *admin.CodexGatewayHandler {
	return admin.NewCodexGatewayHandler(adminSvc)
}

func ProvideFormalPoolOnboardingHandler(
	svc *service.FormalPoolOnboardingService,
	limiter service.FormalPoolEgressRateLimiter,
	riskWriter service.FormalPoolRiskEventWriter,
) *admin.FormalPoolOnboardingHandler {
	return admin.NewFormalPoolOnboardingHandlerWithPublicDeps(svc, limiter, riskWriter)
}

func ProvideFormalPoolOnboardingPrincipalResolver(userService *service.UserService, cfg *config.Config) admin.FormalPoolOnboardingPrincipalResolver {
	return admin.NewFormalPoolOnboardingPrincipalResolver(userService, cfg.FormalPool.AuthorityTenantID, time.Now)
}

func ProvideFormalPoolOnboardingPrincipalRevalidator(userService *service.UserService, cfg *config.Config) service.FormalPoolOnboardingPrincipalRevalidator {
	return admin.NewFormalPoolOnboardingPrincipalRevalidator(userService, cfg.FormalPool.AuthorityTenantID, time.Now)
}

// ProvideSettingHandler creates SettingHandler with version from BuildInfo
func ProvideSettingHandler(settingService *service.SettingService, buildInfo BuildInfo, notificationEmailService *service.NotificationEmailService) *SettingHandler {
	h := NewSettingHandler(settingService, buildInfo.Version)
	h.SetNotificationEmailService(notificationEmailService)
	return h
}

// ProvideAdminSettingHandler creates admin.SettingHandler with notification template APIs.
func ProvideAdminSettingHandler(settingService *service.SettingService, emailService *service.EmailService, turnstileService *service.TurnstileService, opsService *service.OpsService, paymentConfigService *service.PaymentConfigService, paymentService *service.PaymentService, userAttributeService *service.UserAttributeService, notificationEmailService *service.NotificationEmailService) *admin.SettingHandler {
	h := admin.NewSettingHandler(settingService, emailService, turnstileService, opsService, paymentConfigService, paymentService, userAttributeService)
	h.SetNotificationEmailService(notificationEmailService)
	return h
}

// ProvideAuthHandler wires AuthHandler with explicit Augment dependencies while
// NewAuthHandler keeps direct test call sites source-compatible.
func ProvideAuthHandler(
	cfg *config.Config,
	authService *service.AuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	promoService *service.PromoService,
	redeemService *service.RedeemService,
	totpService *service.TotpService,
	userAttributeService *service.UserAttributeService,
	augmentPluginService *service.AugmentPluginService,
	augmentGatewayService *service.AugmentGatewayService,
	augmentOfficialSessionService *service.AugmentOfficialSessionService,
	augmentOfficialPoolService *service.AugmentOfficialPoolSessionService,
	augmentGatewayUsageService *service.AugmentGatewayUsageService,
) *AuthHandler {
	return NewAuthHandler(
		cfg,
		authService,
		userService,
		settingService,
		promoService,
		redeemService,
		totpService,
		userAttributeService,
		augmentPluginService,
		augmentGatewayService,
		augmentOfficialSessionService,
		augmentOfficialPoolService,
		augmentGatewayUsageService,
	)
}

func ProvideOpenAIGatewayHandler(
	gatewayService *service.OpenAIGatewayService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	cfg *config.Config,
) *OpenAIGatewayHandler {
	return NewOpenAIGatewayHandler(
		gatewayService,
		concurrencyService,
		billingCacheService,
		apiKeyService,
		usageRecordWorkerPool,
		errorPassthroughService,
		cfg,
	)
}

func ProvideCodexGatewayHandler(codexGatewayService *service.CodexGatewayService) *CodexGatewayHandler {
	return NewCodexGatewayHandler(codexGatewayService)
}

// ProvideHandlers creates the Handlers struct
func ProvideHandlers(
	authHandler *AuthHandler,
	userHandler *UserHandler,
	apiKeyHandler *APIKeyHandler,
	usageHandler *UsageHandler,
	redeemHandler *RedeemHandler,
	subscriptionHandler *SubscriptionHandler,
	announcementHandler *AnnouncementHandler,
	channelMonitorUserHandler *ChannelMonitorUserHandler,
	codexAgentHandler *CodexAgentHandler,
	codexEntryCenterHandler *CodexEntryCenterHandler,
	adminHandlers *AdminHandlers,
	gatewayHandler *GatewayHandler,
	codexGatewayHandler *CodexGatewayHandler,
	openaiGatewayHandler *OpenAIGatewayHandler,
	settingHandler *SettingHandler,
	totpHandler *TotpHandler,
	paymentHandler *PaymentHandler,
	paymentWebhookHandler *PaymentWebhookHandler,
	availableChannelHandler *AvailableChannelHandler,
	batchImageHandler *BatchImageHandler,
	_ *service.IdempotencyCoordinator,
	_ *service.IdempotencyCleanupService,
) *Handlers {
	return &Handlers{
		Auth:             authHandler,
		User:             userHandler,
		APIKey:           apiKeyHandler,
		Usage:            usageHandler,
		Redeem:           redeemHandler,
		Subscription:     subscriptionHandler,
		Announcement:     announcementHandler,
		CodexAgent:       codexAgentHandler,
		CodexEntryCenter: codexEntryCenterHandler,
		ChannelMonitor:   channelMonitorUserHandler,
		Admin:            adminHandlers,
		Gateway:          gatewayHandler,
		CodexGateway:     codexGatewayHandler,
		OpenAIGateway:    openaiGatewayHandler,
		Setting:          settingHandler,
		Totp:             totpHandler,
		Payment:          paymentHandler,
		PaymentWebhook:   paymentWebhookHandler,
		AvailableChannel: availableChannelHandler,
		BatchImage:       batchImageHandler,
	}
}

// ProviderSet is the Wire provider set for all handlers
var ProviderSet = wire.NewSet(
	// Top-level handlers
	ProvideAuthHandler,
	NewUserHandler,
	NewAPIKeyHandler,
	NewUsageHandler,
	NewRedeemHandler,
	NewSubscriptionHandler,
	NewAnnouncementHandler,
	NewChannelMonitorUserHandler,
	NewCodexAgentHandler,
	NewCodexEntryCenterHandler,
	NewGatewayHandler,
	ProvideCodexGatewayHandler,
	ProvideOpenAIGatewayHandler,
	NewTotpHandler,
	ProvideSettingHandler,
	NewPaymentHandler,
	NewPaymentWebhookHandler,
	NewAvailableChannelHandler,
	NewBatchImageHandler,

	// Admin handlers
	admin.NewDashboardHandler,
	admin.NewUserHandler,
	admin.NewGroupHandler,
	admin.NewAccountHandler,
	admin.NewAnnouncementHandler,
	admin.NewDataManagementHandler,
	admin.NewBackupHandler,
	admin.NewOAuthHandler,
	admin.NewOpenAIOAuthHandler,
	admin.NewGeminiOAuthHandler,
	admin.NewGeminiHealthHandler,
	admin.NewAntigravityOAuthHandler,
	admin.NewGrokOAuthHandler,
	admin.NewProxyHandler,
	admin.NewRedeemHandler,
	admin.NewPromoHandler,
	ProvideAdminSettingHandler,
	admin.NewOpsHandler,
	ProvideSystemHandler,
	admin.NewSubscriptionHandler,
	admin.NewUsageHandler,
	admin.NewUserAttributeHandler,
	admin.NewErrorPassthroughHandler,
	admin.NewTLSFingerprintProfileHandler,
	admin.NewAdminAPIKeyHandler,
	admin.NewEntityHandler,
	admin.NewScheduledTestHandler,
	admin.NewChannelHandler,
	admin.NewChannelMonitorHandler,
	admin.NewChannelMonitorRequestTemplateHandler,
	admin.NewContentModerationHandler,
	admin.NewPaymentHandler,
	admin.NewAffiliateHandler,
	ProvideAugmentGatewayHandler,
	ProvideCodexGatewayAdminHandler,
	ProvideFormalPoolOnboardingHandler,
	ProvideFormalPoolOnboardingPrincipalResolver,
	ProvideFormalPoolOnboardingPrincipalRevalidator,
	admin.NewFormalPoolOperationsHandler,
	admin.NewComplianceHandler,

	// AdminHandlers and Handlers constructors
	ProvideAdminHandlersForWire,
	ProvideHandlers,
)
