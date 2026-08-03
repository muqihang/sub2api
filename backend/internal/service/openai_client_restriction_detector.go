package service

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
)

// CodexOfficialClientsOnlyMessage is the generic 403 message for codex_cli_only denials.
// Version-boundary denials can use a more specific client-facing message because the
// request has already been identified as an official Codex client.
const CodexOfficialClientsOnlyMessage = "This account only allows Codex official clients"

const (
	// CodexClientRestrictionReasonDisabled 表示账号未开启 codex_cli_only。
	CodexClientRestrictionReasonDisabled = "codex_cli_only_disabled"
	// CodexClientRestrictionReasonMatchedUA 表示请求命中官方客户端 UA 白名单。
	CodexClientRestrictionReasonMatchedUA = "official_client_user_agent_matched"
	// CodexClientRestrictionReasonMatchedOriginator 表示请求命中官方客户端 originator 白名单。
	CodexClientRestrictionReasonMatchedOriginator = "official_client_originator_matched"
	// CodexClientRestrictionReasonMatchedAllowedClient 表示请求命中账号级额外放行的命名客户端预设。
	CodexClientRestrictionReasonMatchedAllowedClient = "allowed_client_matched"
	// CodexClientRestrictionReasonMatchedGlobalAllowedClient 表示请求命中全局额外放行的命名客户端预设。
	CodexClientRestrictionReasonMatchedGlobalAllowedClient = "global_allowed_client_matched"
	// CodexClientRestrictionReasonNotMatchedUA 表示请求未命中官方客户端 UA 白名单。
	CodexClientRestrictionReasonNotMatchedUA = "official_client_user_agent_not_matched"
	// CodexClientRestrictionReasonForceCodexCLI 表示通过 ForceCodexCLI 配置兜底放行。
	CodexClientRestrictionReasonForceCodexCLI = "force_codex_cli_enabled"
	// CodexClientRestrictionReasonVersionTooLow 表示官方 Codex 客户端版本低于策略要求。
	CodexClientRestrictionReasonVersionTooLow = "codex_version_too_low"
	// CodexClientRestrictionReasonVersionTooHigh 表示官方 Codex 客户端版本高于策略允许范围。
	CodexClientRestrictionReasonVersionTooHigh = "codex_version_too_high"
)

// CodexClientRestrictionDetectionResult 是 codex_cli_only 统一检测入口结果。
type CodexClientRestrictionDetectionResult struct {
	Enabled         bool
	Matched         bool
	Reason          string
	DetectedVersion string
	MinCodexVersion string
	MaxCodexVersion string
}

// CodexClientRestrictionDetector 定义 codex_cli_only 统一检测入口。
type CodexClientRestrictionDetector interface {
	Detect(c *gin.Context, account *Account, globalAllowedClients []string) CodexClientRestrictionDetectionResult
}

// OpenAICodexClientRestrictionDetector 为 OpenAI OAuth codex_cli_only 的默认实现。
type OpenAICodexClientRestrictionDetector struct {
	cfg *config.Config
}

func NewOpenAICodexClientRestrictionDetector(cfg *config.Config) *OpenAICodexClientRestrictionDetector {
	return &OpenAICodexClientRestrictionDetector{cfg: cfg}
}

func (d *OpenAICodexClientRestrictionDetector) Detect(c *gin.Context, account *Account, globalAllowedClients []string) CodexClientRestrictionDetectionResult {
	if account == nil || !account.IsCodexCLIOnlyEnabled() {
		return CodexClientRestrictionDetectionResult{
			Enabled: false,
			Matched: false,
			Reason:  CodexClientRestrictionReasonDisabled,
		}
	}

	if d != nil && d.cfg != nil && d.cfg.Gateway.ForceCodexCLI {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonForceCodexCLI,
		}
	}

	userAgent := ""
	originator := ""
	if c != nil {
		userAgent = c.GetHeader("User-Agent")
		originator = c.GetHeader("originator")
	}
	if openai.IsCodexOfficialClientRequestStrict(userAgent) {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedUA,
		}
	}
	if openai.IsCodexOfficialClientOriginator(originator) {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedOriginator,
		}
	}

	// 官方客户端白名单未命中时，先尝试账号级额外放行的命名客户端预设（如 Claude Code codex 插件）。
	if allowed := account.GetCodexCLIOnlyAllowedClients(); len(allowed) > 0 &&
		openai.MatchAllowedClients(userAgent, originator, allowed) {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedAllowedClient,
		}
	}

	// 再尝试由更高作用域（全局设置）注入的额外放行客户端列表。
	if len(globalAllowedClients) > 0 &&
		openai.MatchAllowedClients(userAgent, originator, globalAllowedClients) {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedGlobalAllowedClient,
		}
	}

	return CodexClientRestrictionDetectionResult{
		Enabled: true,
		Matched: false,
		Reason:  CodexClientRestrictionReasonNotMatchedUA,
	}
}

// CodexClientRestrictionMessage maps a codex_cli_only detection result to the
// client-facing 403 message. Only version-boundary denials reveal version details.
func CodexClientRestrictionMessage(r CodexClientRestrictionDetectionResult) string {
	switch r.Reason {
	case CodexClientRestrictionReasonVersionTooLow:
		return fmt.Sprintf(
			"Your Codex version (%s) is below the minimum required version (%s). Please update Codex.",
			r.DetectedVersion, r.MinCodexVersion,
		)
	case CodexClientRestrictionReasonVersionTooHigh:
		return fmt.Sprintf(
			"Your Codex version (%s) exceeds the maximum allowed version (%s). Please downgrade Codex to %s or lower.",
			r.DetectedVersion, r.MaxCodexVersion, r.MaxCodexVersion,
		)
	default:
		return CodexOfficialClientsOnlyMessage
	}
}
