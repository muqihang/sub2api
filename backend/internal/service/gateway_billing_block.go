package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf16"

	"github.com/tidwall/gjson"
)

// fingerprintSalt 是计算 cc_version 后缀指纹的盐值。
//
// 来源：与 Parrot src/transform/cc_mimicry.py 的 FINGERPRINT_SALT 完全一致；
// 这是真实 Claude Code CLI 抓包推导出的常量，改动会导致 fp 与 CLI 不一致，
// 进一步触发 Anthropic 的第三方检测。
const fingerprintSalt = "59cf53e54c78"

// computeClaudeCodeFingerprint 复刻真实 Claude Code CLI 的 cc_version 指纹算法：
//
//  1. 取 messages 中第一条 role=user 的纯文本（首块 text）
//  2. 按 JavaScript UTF-16 code unit 语义取第 4、7、20 个字符（不足以 '0' 补齐）
//  3. SHA256(SALT + chars + cc_version) 取 hex 前 3 字符
//
// 算法来自 Parrot src/transform/cc_mimicry.py:compute_fingerprint，与官方 CLI 字节对齐。
// 任何偏差都会导致 cc_version=X.Y.Z.{fp} 在上游侧与真实 CLI 不一致。
func computeClaudeCodeFingerprint(body []byte, version string) string {
	firstText := extractFirstUserText(body)
	indices := []int{4, 7, 20}
	codeUnits := utf16.Encode([]rune(firstText))
	selectedUnits := make([]uint16, 0, len(indices))
	for _, i := range indices {
		selectedUnits = append(selectedUnits, jsCharAt(codeUnits, i))
	}
	chars := string(utf16.Decode(selectedUnits))
	sum := sha256.Sum256([]byte(fingerprintSalt + chars + version))
	return hex.EncodeToString(sum[:])[:3]
}

// jsCharAt simulates JavaScript's str[i] lookup using UTF-16 code-unit indexing.
// The returned uint16 is a raw code unit; callers should concatenate all selected
// units first, then decode once, so surrogate pairs can recombine just like JS.
func jsCharAt(codeUnits []uint16, idx int) uint16 {
	if idx < 0 || idx >= len(codeUnits) {
		return uint16('0')
	}
	return codeUnits[idx]
}

// extractFirstUserText 提取 messages 中第一条 user 消息的首段 text 内容。
// 兼容 string 和 []block 两种 content 格式。
func extractFirstUserText(body []byte) string {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return ""
	}
	first := ""
	messages.ForEach(func(_, msg gjson.Result) bool {
		if msg.Get("role").String() != "user" {
			return true
		}
		content := msg.Get("content")
		if content.Type == gjson.String {
			first = content.String()
			return false
		}
		if content.IsArray() {
			content.ForEach(func(_, block gjson.Result) bool {
				if block.Get("type").String() == "text" {
					first = block.Get("text").String()
					return false
				}
				return true
			})
			return false
		}
		return false
	})
	return first
}

// buildBillingAttributionBlockJSON 构造 system 数组的 billing attribution block。
//
// 形态严格对齐真实 Claude Code CLI：
//
//	{"type":"text","text":"x-anthropic-billing-header: cc_version={cliVersion}.{fp}; cc_entrypoint=sdk-cli; cch=00000;"}
//
// cch=00000 是签名占位符，由 signBillingHeaderCCH 在 buildUpstreamRequest 阶段
// 替换为基于完整 body 的 xxhash64 5 位十六进制摘要。
//
// 此 block 不带 cache_control（与真实 CLI 一致；cache breakpoint 由后续的
// Claude Code prompt block 承担）。
func buildBillingAttributionBlockJSON(body []byte, cliVersion string) ([]byte, error) {
	if cliVersion == "" {
		return nil, fmt.Errorf("cliVersion required")
	}
	fp := computeClaudeCodeFingerprint(body, cliVersion)
	text := fmt.Sprintf(
		"x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=sdk-cli; cch=00000;",
		cliVersion, fp,
	)
	return json.Marshal(map[string]string{
		"type": "text",
		"text": text,
	})
}
