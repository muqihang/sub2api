package service

import (
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

const suspiciousClaudeCodeMinVersion = "2.1.150"

var billingCCVersionTokenRe = regexp.MustCompile(`\bcc_version=([0-9]+\.[0-9]+\.[0-9]+)([^;\s"]*)`)

type SuspiciousClaudeCodeProbeResult struct {
	Block   bool
	Reasons []string
}

// DetectSuspiciousClaudeCodeProbe identifies obviously synthetic Claude Code probe
// payloads. It intentionally requires a zero metadata placeholder plus suspicious
// billing material, so normal clients that use cch=00000 before signing are not blocked.
func DetectSuspiciousClaudeCodeProbe(body []byte) SuspiciousClaudeCodeProbeResult {
	var reasons []string
	if isStreamingOneTokenProbe(body) {
		reasons = append(reasons, "streaming_one_token_probe")
	}
	if hasZeroMetadataUserID(body) {
		reasons = append(reasons, "zero_metadata_user_id")
	}
	if hasPlaceholderCCH(body) {
		reasons = append(reasons, "placeholder_cch")
	}
	if hasTestCCVersion(body) {
		reasons = append(reasons, "test_cc_version")
	}
	if hasOldCCVersion(body) {
		reasons = append(reasons, "old_cc_version")
	}

	return SuspiciousClaudeCodeProbeResult{
		Block: containsReason(reasons, "streaming_one_token_probe") &&
			containsReason(reasons, "zero_metadata_user_id") &&
			(containsReason(reasons, "test_cc_version") || containsReason(reasons, "old_cc_version") || containsReason(reasons, "placeholder_cch")),
		Reasons: reasons,
	}
}

func isStreamingOneTokenProbe(body []byte) bool {
	return gjson.GetBytes(body, "stream").Bool() && gjson.GetBytes(body, "max_tokens").Int() == 1
}

func hasZeroMetadataUserID(body []byte) bool {
	userID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	if userID == "" {
		return false
	}
	var parsed struct {
		DeviceID  string `json:"device_id"`
		SessionID string `json:"session_id"`
	}
	if !gjson.Valid(userID) {
		matches := legacyUserIDRegex.FindStringSubmatch(userID)
		return matches != nil && isPlaceholderMetadataUserID(matches[1], matches[3])
	}
	parsed.DeviceID = gjson.Get(userID, "device_id").String()
	parsed.SessionID = gjson.Get(userID, "session_id").String()
	return isPlaceholderMetadataUserID(parsed.DeviceID, parsed.SessionID)
}

func hasPlaceholderCCH(body []byte) bool {
	for _, text := range billingHeaderTexts(body) {
		if strings.Contains(text, "cch=00000") {
			return true
		}
	}
	return false
}

func hasTestCCVersion(body []byte) bool {
	for _, text := range billingHeaderTexts(body) {
		for _, match := range billingCCVersionTokenRe.FindAllStringSubmatch(text, -1) {
			if len(match) >= 3 && strings.Contains(strings.ToLower(match[2]), "test") {
				return true
			}
		}
	}
	return false
}

func hasOldCCVersion(body []byte) bool {
	for _, text := range billingHeaderTexts(body) {
		for _, match := range billingCCVersionTokenRe.FindAllStringSubmatch(text, -1) {
			if len(match) >= 2 && CompareVersions(match[1], suspiciousClaudeCodeMinVersion) < 0 {
				return true
			}
		}
	}
	return false
}

func billingHeaderTexts(body []byte) []string {
	system := gjson.GetBytes(body, "system")
	if !system.Exists() {
		return nil
	}
	var out []string
	if system.IsArray() {
		system.ForEach(func(_, item gjson.Result) bool {
			text := ""
			if item.Type == gjson.String {
				text = item.String()
			} else if t := item.Get("text"); t.Type == gjson.String {
				text = t.String()
			}
			if strings.HasPrefix(strings.TrimSpace(text), "x-anthropic-billing-header:") {
				out = append(out, text)
			}
			return true
		})
		return out
	}
	if system.Type == gjson.String {
		for _, line := range strings.Split(system.String(), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "x-anthropic-billing-header:") {
				out = append(out, line)
			}
		}
	}
	return out
}

func containsReason(reasons []string, needle string) bool {
	for _, reason := range reasons {
		if reason == needle {
			return true
		}
	}
	return false
}
