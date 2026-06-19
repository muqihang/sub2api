package service

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	openAIRuntimeGuardContentSafetyActionPass   = "pass"
	openAIRuntimeGuardContentSafetyActionBlock  = "block"
	openAIRuntimeGuardContentSafetyActionShadow = "shadow"

	openAIRuntimeGuardContentSafetyBlockedMetric = "openai_runtime_guard.content_safety.blocked"
	openAIRuntimeGuardContentSafetyShadowMetric  = "openai_runtime_guard.content_safety.shadow_blocked"
)

type openAIRuntimeGuardContentSafetyDecision struct {
	Action     string
	Category   string
	Metric     string
	Confidence string
	Blocked    bool
	Shadow     bool
}

func evaluateOpenAIRuntimeGuardContentSafety(account *Account, protocol string, body []byte) openAIRuntimeGuardContentSafetyDecision {
	if !shouldApplyOpenAIRuntimeGuardContentSafety(account) || len(body) == 0 || openAIRuntimeGuardContentSafetyDisabled(account) {
		return openAIRuntimeGuardContentSafetyDecision{}
	}
	text := openAIRuntimeGuardContentSafetyExtractText(protocol, body)
	category := classifyOpenAIRuntimeGuardContentSafety(text)
	if category == "" {
		return openAIRuntimeGuardContentSafetyDecision{}
	}
	decision := openAIRuntimeGuardContentSafetyDecision{
		Action:     openAIRuntimeGuardContentSafetyActionBlock,
		Category:   category,
		Metric:     openAIRuntimeGuardContentSafetyBlockedMetric,
		Confidence: "high",
		Blocked:    true,
	}
	if openAIRuntimeGuardContentSafetyShadowOnly(account) {
		decision.Action = openAIRuntimeGuardContentSafetyActionShadow
		decision.Metric = openAIRuntimeGuardContentSafetyShadowMetric
		decision.Blocked = false
		decision.Shadow = true
	}
	return decision
}

func shouldApplyOpenAIRuntimeGuardContentSafety(account *Account) bool {
	return account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeOAuth
}

func openAIRuntimeGuardContentSafetyMode(account *Account) string {
	if account == nil {
		return ""
	}
	for _, key := range []string{
		"openai_content_safety_guard_mode",
		"openai_runtime_guard_content_safety_mode",
		"openai_runtime_guard_content_mode",
	} {
		if mode := strings.ToLower(strings.TrimSpace(account.GetExtraString(key))); mode != "" {
			return mode
		}
	}
	return ""
}

func openAIRuntimeGuardContentSafetyDisabled(account *Account) bool {
	switch openAIRuntimeGuardContentSafetyMode(account) {
	case "off", "disabled", "disable", "false", "0":
		return true
	default:
		return false
	}
}

func openAIRuntimeGuardContentSafetyShadowOnly(account *Account) bool {
	switch openAIRuntimeGuardContentSafetyMode(account) {
	case "shadow", "shadow_only", "dry_run", "observe":
		return true
	default:
		return false
	}
}

func openAIRuntimeGuardContentSafetyExtractText(protocol string, body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	var parts []string
	switch protocol {
	case ContentModerationProtocolOpenAIImages:
		addModerationText(&parts, gjson.GetBytes(body, "prompt").String())
	case ContentModerationProtocolOpenAIChat:
		collectOpenAIRuntimeGuardChatConversationText(gjson.GetBytes(body, "messages"), &parts)
	case ContentModerationProtocolOpenAIResponses:
		collectOpenAIRuntimeGuardResponsesInputText(gjson.GetBytes(body, "input"), &parts)
	default:
		collectOpenAIRuntimeGuardResponsesInputText(gjson.GetBytes(body, "input"), &parts)
		collectOpenAIRuntimeGuardChatConversationText(gjson.GetBytes(body, "messages"), &parts)
		addModerationText(&parts, gjson.GetBytes(body, "prompt").String())
	}
	return normalizeContentModerationText(strings.Join(parts, "\n"))
}

func collectOpenAIRuntimeGuardChatConversationText(messages gjson.Result, parts *[]string) {
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, message gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(message.Get("role").String()))
		switch role {
		case "user", "assistant", "tool":
			collectOpenAIRuntimeGuardContentValueText(message.Get("content"), parts)
		}
		return true
	})
}

func collectOpenAIRuntimeGuardResponsesInputText(input gjson.Result, parts *[]string) {
	switch {
	case !input.Exists():
		return
	case input.Type == gjson.String:
		addModerationText(parts, input.String())
	case input.IsArray():
		input.ForEach(func(_, item gjson.Result) bool {
			collectOpenAIRuntimeGuardResponsesItemText(item, parts)
			return true
		})
	case input.IsObject():
		collectOpenAIRuntimeGuardResponsesItemText(input, parts)
	}
}

func collectOpenAIRuntimeGuardResponsesItemText(item gjson.Result, parts *[]string) {
	if !item.IsObject() {
		collectOpenAIRuntimeGuardContentValueText(item, parts)
		return
	}
	typ := strings.ToLower(strings.TrimSpace(item.Get("type").String()))
	role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
	switch typ {
	case "function_call_output", "tool_output", "computer_call_output", "local_shell_call_output":
		collectOpenAIRuntimeGuardContentValueText(item.Get("output"), parts)
		collectOpenAIRuntimeGuardContentValueText(item.Get("content"), parts)
		return
	case "function_call", "tool_call", "computer_call", "local_shell_call":
		return
	}
	if role == "user" || role == "assistant" || role == "tool" || role == "" {
		collectOpenAIRuntimeGuardContentValueText(item.Get("content"), parts)
		if typ == "input_text" || typ == "output_text" || item.Get("text").Exists() {
			addModerationText(parts, item.Get("text").String())
		}
	}
}

func collectOpenAIRuntimeGuardContentValueText(value gjson.Result, parts *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addModerationText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectOpenAIRuntimeGuardContentValueText(item, parts)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch typ {
		case "", "text", "input_text", "output_text", "message":
			addModerationText(parts, value.Get("text").String())
			collectOpenAIRuntimeGuardContentValueText(value.Get("content"), parts)
		case "function_call_output", "tool_output", "computer_call_output", "local_shell_call_output":
			collectOpenAIRuntimeGuardContentValueText(value.Get("output"), parts)
			collectOpenAIRuntimeGuardContentValueText(value.Get("content"), parts)
		}
	}
}

func classifyOpenAIRuntimeGuardContentSafety(text string) string {
	lower := strings.ToLower(normalizeContentModerationText(text))
	if lower == "" {
		return ""
	}
	if openAIRuntimeGuardContentSafetyMinorsSexual(lower) {
		return "content_safety.minors_sexual.high_confidence"
	}
	if openAIRuntimeGuardContentSafetyLooksDefensive(lower) {
		return ""
	}
	if openAIRuntimeGuardContentSafetyExplicitSexual(lower) {
		return "content_safety.sexual.high_confidence"
	}
	if openAIRuntimeGuardContentSafetyCredentialTheft(lower) {
		return "content_safety.credential_theft.high_confidence"
	}
	if openAIRuntimeGuardContentSafetyMalware(lower) {
		return "content_safety.malware.high_confidence"
	}
	if openAIRuntimeGuardContentSafetyDestructiveAttack(lower) {
		return "content_safety.destructive_attack.high_confidence"
	}
	if openAIRuntimeGuardContentSafetyIllicitInstructions(lower) {
		return "content_safety.illicit.high_confidence"
	}
	return ""
}

func openAIRuntimeGuardContentSafetyLooksDefensive(text string) bool {
	if hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "phishing", "fake login", "credential harvesting") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "build", "create", "deploy", "host", "clone", "make", "write") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "collect", "steal", "capture", "harvest", "exfiltrate", "send") {
		return false
	}
	if hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "malware", "ransomware", "trojan", "keylogger", "payload") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "write", "build", "create", "deploy", "install", "exfiltrate", "steal", "encrypts") {
		return false
	}
	if hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "instructions", "step-by-step", "how to", "steps") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "wipe", "destroy", "make a bomb", "build a bomb", "explosive", "stolen goods") {
		return false
	}
	return hasAnyOpenAIRuntimeGuardContentSafetyTerm(text,
		"defensive", "detect", "detection", "analyze", "analysis", "patch", "harden", "mitigate", "prevention", "protect", "remove it",
		"my own app", "own login page", "ctf", "lab", "sandbox", "malware analysis", "yara", "reverse engineer", "interoperability", "compliance",
		"education", "medical", "sti prevention", "parameterized queries",
	)
}

func openAIRuntimeGuardContentSafetyMinorsSexual(text string) bool {
	return hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "minor", "underage", "child", "teen", "12-year-old", "12 year old", "13-year-old", "13 year old", "14-year-old", "14 year old", "15 year old", "15-year-old", "16 year old", "16-year-old", "17 year old", "17-year-old") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "explicit sexual", "sex acts", "graphic sex", "porn", "nude", "naked", "sexual content")
}

func openAIRuntimeGuardContentSafetyExplicitSexual(text string) bool {
	return hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "explicit sexual", "graphic sex", "sex acts", "porn", "pornographic", "erotic roleplay") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "write", "create", "generate", "describe", "depicting", "content")
}

func openAIRuntimeGuardContentSafetyCredentialTheft(text string) bool {
	return hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "phishing", "fake login", "spoofed login", "credential harvesting") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "collect", "steal", "capture", "harvest", "send", "exfiltrate") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "password", "passwords", "credential", "credentials", "login", "token", "cookies")
}

func openAIRuntimeGuardContentSafetyMalware(text string) bool {
	return hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "malware", "ransomware", "trojan", "keylogger", "payload") &&
		(hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "write", "build", "create", "deploy", "install", "run") || hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "persists", "persistent", "encrypts", "exfiltrate", "steal", "sends")) &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "exfiltrate", "steal", "send", "sends", "encrypts", "ransom", "persists", "persistent", "cookies", "password", "passwords", "files")
}

func openAIRuntimeGuardContentSafetyDestructiveAttack(text string) bool {
	return hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "step-by-step", "instructions", "give steps", "how to") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "wipe", "destroy", "delete all", "brick", "ddos", "dos attack") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "victim", "production server", "server", "database", "disk", "disks")
}

func openAIRuntimeGuardContentSafetyIllicitInstructions(text string) bool {
	if hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "instructions", "step-by-step", "how to", "steps") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "make a bomb", "build a bomb", "explosive", "explosives", "improvised weapon") {
		return true
	}
	return hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "instructions", "step-by-step", "how to", "scheme") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "illegal", "stolen", "trafficking", "evading police", "evade law enforcement", "law enforcement") &&
		hasAnyOpenAIRuntimeGuardContentSafetyTerm(text, "profit", "credit card", "credit cards", "stolen goods", "evading", "evade")
}

func hasAnyOpenAIRuntimeGuardContentSafetyTerm(text string, terms ...string) bool {
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func openAIRuntimeGuardContentSafetyDecisionToReasoningDecision(decision openAIRuntimeGuardContentSafetyDecision) openAIReasoningEffortGuardDecision {
	if decision.Category == "" || decision.Metric == "" {
		return openAIReasoningEffortGuardDecision{}
	}
	action := decision.Action
	if action == "" {
		action = openAIRuntimeGuardContentSafetyActionPass
	}
	return openAIReasoningEffortGuardDecision{
		Action:     action,
		Blocked:    decision.Blocked,
		Present:    true,
		Status:     http.StatusBadRequest,
		Path:       "input",
		Category:   decision.Category,
		Metric:     decision.Metric,
		Confidence: decision.Confidence,
	}
}

func setOpenAIRuntimeGuardContentSafetyMetadata(c *gin.Context, decision openAIRuntimeGuardContentSafetyDecision) {
	if c == nil || decision.Category == "" || decision.Metric == "" {
		return
	}
	c.Set(OpenAIRuntimeGuardMetadataKey, OpenAIRuntimeGuardMetadata{
		Action:     decision.Action,
		Category:   decision.Category,
		Metric:     decision.Metric,
		Field:      "input",
		Path:       "input",
		Status:     http.StatusBadRequest,
		Confidence: decision.Confidence,
	})
}

func applyOpenAIRuntimeGuardContentSafetyToHTTP(c *gin.Context, account *Account, protocol string, body []byte) *OpenAIRuntimeGuardBlockedError {
	decision := evaluateOpenAIRuntimeGuardContentSafety(account, protocol, body)
	if decision.Category == "" {
		return nil
	}
	if !decision.Blocked {
		if decision.Shadow {
			setOpenAIRuntimeGuardContentSafetyMetadata(c, decision)
		}
		return nil
	}
	setOpenAIRuntimeGuardContentSafetyMetadata(c, decision)
	blocked := newOpenAIRuntimeGuardBlockedError(openAIRuntimeGuardContentSafetyDecisionToReasoningDecision(decision))
	MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
	if c != nil {
		c.Data(blocked.StatusCode, "application/json; charset=utf-8", blocked.Payload)
	}
	return blocked
}

func applyOpenAIRuntimeGuardContentSafetyToBody(account *Account, protocol string, body []byte) *OpenAIRuntimeGuardBlockedError {
	decision := evaluateOpenAIRuntimeGuardContentSafety(account, protocol, body)
	if !decision.Blocked {
		return nil
	}
	return newOpenAIRuntimeGuardBlockedError(openAIRuntimeGuardContentSafetyDecisionToReasoningDecision(decision))
}
