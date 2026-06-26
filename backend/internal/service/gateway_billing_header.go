package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ccVersionInBillingRe matches the semver part of cc_version (X.Y.Z), preserving
// the trailing message-derived suffix (e.g. ".c02") if present.
var ccVersionInBillingRe = regexp.MustCompile(`cc_version=\d+\.\d+\.\d+`)

// ccEntrypointInBillingRe matches the cc_entrypoint segment in billing headers.
var ccEntrypointInBillingRe = regexp.MustCompile(`cc_entrypoint=[^;]+`)

// cchPlaceholderRe matches the cch=00000 placeholder in billing header text,
// scoped to x-anthropic-billing-header to avoid touching user content.
var cchPlaceholderRe = regexp.MustCompile(`(x-anthropic-billing-header:[^"]*?\bcch=)(00000)(;)`)

const cchSeed uint64 = 0x6E52736AC806831E

// syncBillingHeaderVersion rewrites legacy x-anthropic-billing-header fields to match
// current Claude Code billing conventions:
//   - cc_version tracks the version extracted from userAgent when available
//   - cc_entrypoint is always normalized to sdk-cli
//
// Only touches system array blocks whose text starts with "x-anthropic-billing-header".
func syncBillingHeaderVersion(body []byte, userAgent string) []byte {
	version := ExtractCLIVersion(userAgent)

	systemResult := gjson.GetBytes(body, "system")
	if !systemResult.Exists() || !systemResult.IsArray() {
		return body
	}

	idx := 0
	systemResult.ForEach(func(_, item gjson.Result) bool {
		text := item.Get("text")
		if text.Exists() && text.Type == gjson.String &&
			strings.HasPrefix(text.String(), "x-anthropic-billing-header") {
			newText := ccEntrypointInBillingRe.ReplaceAllString(text.String(), "cc_entrypoint=sdk-cli")
			if version != "" {
				newText = ccVersionInBillingRe.ReplaceAllString(newText, "cc_version="+version)
			}
			if newText != text.String() {
				if updated, err := sjson.SetBytes(body, fmt.Sprintf("system.%d.text", idx), newText); err == nil {
					body = updated
				}
			}
		}
		idx++
		return true
	})

	return body
}

// stripCCGatewayDownstreamBillingMaterial removes downstream-supplied Claude Code
// billing/CCH material before the request enters CC Gateway sign-primary mode.
// CC Gateway owns final shared-pool billing identity; user content that merely
// mentions cch=... outside the system billing block is left untouched so the
// downstream verifier can still fail closed on suspicious input.
func stripCCGatewayDownstreamBillingMaterial(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	systemResult := gjson.GetBytes(body, "system")
	if !systemResult.Exists() {
		return body
	}

	if systemResult.IsArray() {
		kept := make([]any, 0, len(systemResult.Array()))
		changed := false
		systemResult.ForEach(func(_, item gjson.Result) bool {
			text := ""
			if item.Type == gjson.String {
				text = item.String()
			} else if t := item.Get("text"); t.Type == gjson.String {
				text = t.String()
			}
			if strings.HasPrefix(strings.TrimLeft(text, " \t\r\n"), "x-anthropic-billing-header:") {
				changed = true
				return true
			}
			var decoded any
			if err := json.Unmarshal([]byte(item.Raw), &decoded); err == nil {
				kept = append(kept, decoded)
			}
			return true
		})
		if !changed {
			return body
		}
		if updated, err := sjson.SetBytes(body, "system", kept); err == nil {
			return updated
		}
		return body
	}

	if systemResult.Type == gjson.String {
		lines := strings.Split(systemResult.String(), "\n")
		kept := lines[:0]
		changed := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimLeft(line, " \t\r"), "x-anthropic-billing-header:") {
				changed = true
				continue
			}
			kept = append(kept, line)
		}
		if changed {
			if updated, err := sjson.SetBytes(body, "system", strings.Join(kept, "\n")); err == nil {
				return updated
			}
		}
	}
	return body
}

func shouldStripCCGatewayDownstreamBillingMaterial(account *Account) bool {
	if account == nil {
		return false
	}
	if requiresCCGatewayFormalPoolAttestation(account) && ccGatewayBillingShapePolicy(account) == "strip" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(account.GetExtraString("billing_cch_mode")), "sign")
}

// Deprecated: signBillingHeaderCCH is a legacy placeholder signer retained only for
// fallback/rollback paths. Strict passthrough and final OAuth mimicry paths must not
// rely on it.
//
// signBillingHeaderCCH computes the xxHash64-based CCH signature for the request
// body and replaces the cch=00000 placeholder with the computed 5-hex-char hash.
// The body must contain the placeholder when this function is called.
func signBillingHeaderCCH(body []byte) []byte {
	if !cchPlaceholderRe.Match(body) {
		return body
	}
	cch := fmt.Sprintf("%05x", xxHash64Seeded(body, cchSeed)&0xFFFFF)
	return cchPlaceholderRe.ReplaceAll(body, []byte("${1}"+cch+"${3}"))
}

// xxHash64Seeded computes xxHash64 of data with a custom seed.
func xxHash64Seeded(data []byte, seed uint64) uint64 {
	d := xxhash.NewWithSeed(seed)
	_, _ = d.Write(data)
	return d.Sum64()
}
