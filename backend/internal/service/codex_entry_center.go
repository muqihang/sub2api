package service

import (
	"context"
	"time"
)

// Page-level state determines whether the frontend renders the wizard or the console.
type CodexPageState string

const (
	CodexPageStateOnboardingCredential CodexPageState = "onboarding_credential"
	CodexPageStateOnboardingAttach     CodexPageState = "onboarding_attach"
	CodexPageStateOnboardingVerify     CodexPageState = "onboarding_verify"
	CodexPageStateConsole              CodexPageState = "console"
)

// Device-level health state.
type CodexDeviceState string

const (
	CodexDeviceStateHealthy           CodexDeviceState = "healthy"
	CodexDeviceStateCatalogStale      CodexDeviceState = "catalog_stale"
	CodexDeviceStateDeviceOffline     CodexDeviceState = "device_offline"
	CodexDeviceStateCredentialRevoked CodexDeviceState = "credential_revoked"
	CodexDeviceStateClientOutdated    CodexDeviceState = "client_outdated"
)

// Attachment mode: how the credential was bound.
type CodexAttachmentMode string

const (
	CodexAttachmentModeIndependent CodexAttachmentMode = "independent_credential"
	CodexAttachmentModeReusedKey   CodexAttachmentMode = "reused_key"
)

// Setup session presentation: where the session UI should appear.
type CodexSetupSessionPresentation string

const (
	CodexSetupSessionPresentationWizard        CodexSetupSessionPresentation = "wizard"
	CodexSetupSessionPresentationConsoleBanner CodexSetupSessionPresentation = "console_banner"
)

// CatalogErrorKind describes the last catalog sync error.
type CodexCatalogErrorKind string

const (
	CodexCatalogErrorNone    CodexCatalogErrorKind = "none"
	CodexCatalogErrorTimeout CodexCatalogErrorKind = "timeout"
	CodexCatalogErrorAuth    CodexCatalogErrorKind = "auth"
	CodexCatalogErrorServer  CodexCatalogErrorKind = "server"
	CodexCatalogErrorUnknown CodexCatalogErrorKind = "unknown"
)

// Summary response returned by GET /codex/summary.
type CodexEntrySummary struct {
	PageState                CodexPageState                 `json:"page_state"`
	WizardStep               *int                           `json:"wizard_step"`
	AttachmentMode           *CodexAttachmentMode           `json:"attachment_mode"`
	SetupSessionPresentation *CodexSetupSessionPresentation `json:"setup_session_presentation"`
	SetupSession             *CodexSetupSessionDTO          `json:"setup_session"`
	FocusDeviceID            *int64                         `json:"focus_device_id"`
	Devices                  []CodexDeviceDTO               `json:"devices"`
}

type CodexSetupSessionDTO struct {
	ID                   string              `json:"id"`
	CredentialLabel      string              `json:"credential_label"`
	AttachmentMode       CodexAttachmentMode `json:"attachment_mode"`
	ReuseAPIKeyID        *int64              `json:"reuse_api_key_id"`
	LaunchURL            *string             `json:"launch_url"`
	CLICommand           *string             `json:"cli_command"`
	ExpiresAt            time.Time           `json:"expires_at"`
	FirstSeenAt          *time.Time          `json:"first_seen_at"`
	FirstCatalogSyncedAt *time.Time          `json:"first_catalog_synced_at"`
}

type CodexDeviceDTO struct {
	DeviceID                  int64                 `json:"device_id"`
	DeviceName                string                `json:"device_name"`
	AttachmentMode            CodexAttachmentMode   `json:"attachment_mode"`
	DeviceState               CodexDeviceState      `json:"device_state"`
	LastSeenAt                *time.Time            `json:"last_seen_at"`
	ClientVersion             *string               `json:"client_version"`
	MinSupportedClientVersion *string               `json:"min_supported_client_version"`
	CatalogSyncedAt           *time.Time            `json:"catalog_synced_at"`
	CatalogLastErrorKind      CodexCatalogErrorKind `json:"catalog_last_error_kind"`
	RevokedAt                 *time.Time            `json:"revoked_at"`
}

// Setup session create request/response for POST /codex/setup-sessions.
type CodexCreateSetupSessionRequest struct {
	UserID          int64
	AttachmentMode  CodexAttachmentMode
	CredentialLabel string
	ReuseAPIKeyID   *int64
	ServerOrigin    string
	GatewayOrigin   string
}

type CodexCreateSetupSessionResponse struct {
	SetupSession             CodexSetupSessionDTO          `json:"setup_session"`
	PageState                CodexPageState                `json:"page_state"`
	SetupSessionPresentation CodexSetupSessionPresentation `json:"setup_session_presentation"`
}

// Setup session regenerate response for POST /codex/setup-sessions/:id/regenerate.
type CodexRegenerateSetupSessionResponse struct {
	SetupSession CodexSetupSessionRegenerateDTO `json:"setup_session"`
}

type CodexSetupSessionRegenerateDTO struct {
	ID         string    `json:"id"`
	LaunchURL  *string   `json:"launch_url"`
	CLICommand *string   `json:"cli_command"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Diagnose request/response for POST /codex/diagnose.
type CodexDiagnoseRequest struct {
	UserID         int64
	SetupSessionID *string
	DeviceID       *int64
}

type CodexDiagnoseReport struct {
	OK         bool                 `json:"ok"`
	TargetKind string               `json:"target_kind"`
	Checks     []CodexDiagnoseCheck `json:"checks"`
}

type CodexDiagnoseCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Hint   string `json:"hint"`
}

// Device action response (202 Accepted pattern).
type CodexDeviceActionResponse struct {
	DeviceID int64 `json:"device_id"`
	Accepted bool  `json:"accepted"`
}

// CodexEntryCenterService defines the contract for the entry center summary and actions.
// This is separate from the existing CodexAgentService which handles the device-agent protocol.
type CodexEntryCenterService interface {
	GetSummary(ctx context.Context, userID int64) (*CodexEntrySummary, error)
	CreateSetupSession(ctx context.Context, req CodexCreateSetupSessionRequest) (*CodexCreateSetupSessionResponse, error)
	RegenerateSetupSession(ctx context.Context, userID int64, sessionID string) (*CodexRegenerateSetupSessionResponse, error)
	Diagnose(ctx context.Context, req CodexDiagnoseRequest) (*CodexDiagnoseReport, error)
	ResyncDevice(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error)
	RepairDevice(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error)
	ReattachDevice(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error)
	RevokeAttachment(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error)
	RemoveDevice(ctx context.Context, userID int64, deviceID int64) (*CodexDeviceActionResponse, error)
}
