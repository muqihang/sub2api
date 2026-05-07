package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

const (
	openAIEgressSourceBucket          = "bucket"
	openAIEgressSourceAccountFallback = "account_fallback"
	openAIEgressSourceDirectFallback  = "direct_fallback"
)

type OpenAIEgressResolution struct {
	BucketName    string
	ProxyURL      string
	ProxySelected bool
	ProxyLabel    string
	ProxyHash     string
	Source        string
}

type OpenAIEgressPolicyError struct {
	Code       string
	BucketName string
}

func (e *OpenAIEgressPolicyError) Error() string {
	if e == nil {
		return "openai egress policy rejected request"
	}
	if strings.TrimSpace(e.BucketName) == "" {
		return fmt.Sprintf("openai egress policy rejected request: %s", e.Code)
	}
	return fmt.Sprintf("openai egress policy rejected request: %s bucket=%s", e.Code, e.BucketName)
}

func (s *OpenAIGatewayCoreService) ResolveEgress(ctx context.Context, account *Account, fallbackProxyURL string) (*OpenAIEgressResolution, error) {
	_ = ctx
	fallbackProxyURL = strings.TrimSpace(fallbackProxyURL)
	if s == nil || s.cfg == nil {
		return buildOpenAIEgressResolution("default", fallbackProxyURL, openAIEgressSourceAccountFallback), nil
	}

	bucketName := s.ResolveEgressBucket(account)
	bucket, ok := s.findEgressBucket(bucketName)
	if !ok {
		return s.resolveOpenAIEgressFallback(bucketName, "missing_bucket", fallbackProxyURL)
	}
	if !bucket.Enabled {
		return s.resolveOpenAIEgressFallback(bucketName, "disabled_bucket", fallbackProxyURL)
	}
	proxyURL := strings.TrimSpace(bucket.ProxyURL)
	if proxyURL != "" {
		return buildOpenAIEgressResolution(bucketName, proxyURL, openAIEgressSourceBucket), nil
	}
	if s.allowDirectEgressFallback() {
		return buildOpenAIEgressResolution(bucketName, "", openAIEgressSourceDirectFallback), nil
	}
	return nil, &OpenAIEgressPolicyError{Code: "direct_fallback_disabled", BucketName: bucketName}
}

func (s *OpenAIGatewayCoreService) findEgressBucket(name string) (bucket openAIEgressBucketView, ok bool) {
	name = strings.TrimSpace(name)
	if s == nil || s.cfg == nil {
		return openAIEgressBucketView{}, false
	}
	for _, item := range s.cfg.Gateway.OpenAICore.EgressBuckets {
		if strings.TrimSpace(item.Name) == name {
			return openAIEgressBucketView{
				Name:     strings.TrimSpace(item.Name),
				Enabled:  item.Enabled,
				ProxyURL: strings.TrimSpace(item.ProxyURL),
			}, true
		}
	}
	return openAIEgressBucketView{}, false
}

type openAIEgressBucketView struct {
	Name     string
	Enabled  bool
	ProxyURL string
}

func (s *OpenAIGatewayCoreService) resolveOpenAIEgressFallback(bucketName, code, fallbackProxyURL string) (*OpenAIEgressResolution, error) {
	if s == nil || s.cfg == nil {
		return buildOpenAIEgressResolution(bucketName, fallbackProxyURL, openAIEgressSourceAccountFallback), nil
	}
	if s.cfg.Gateway.OpenAICore.EgressFailClosed {
		return nil, &OpenAIEgressPolicyError{Code: code, BucketName: bucketName}
	}
	if fallbackProxyURL != "" && s.allowAccountProxyFallback() {
		return buildOpenAIEgressResolution(bucketName, fallbackProxyURL, openAIEgressSourceAccountFallback), nil
	}
	if s.allowDirectEgressFallback() {
		return buildOpenAIEgressResolution(bucketName, "", openAIEgressSourceDirectFallback), nil
	}
	if fallbackProxyURL != "" {
		return nil, &OpenAIEgressPolicyError{Code: "account_proxy_fallback_disabled", BucketName: bucketName}
	}
	return nil, &OpenAIEgressPolicyError{Code: "direct_fallback_disabled", BucketName: bucketName}
}

func buildOpenAIEgressResolution(bucketName, proxyURL, source string) *OpenAIEgressResolution {
	proxyURL = strings.TrimSpace(proxyURL)
	resolution := &OpenAIEgressResolution{
		BucketName:    strings.TrimSpace(bucketName),
		ProxyURL:      proxyURL,
		ProxySelected: proxyURL != "",
		Source:        source,
	}
	if proxyURL != "" {
		resolution.ProxyLabel = MaskOpenAIProxyURL(proxyURL)
		resolution.ProxyHash = HashOpenAIProxyURL(proxyURL)
	}
	return resolution
}

func (s *OpenAIGatewayCoreService) allowAccountProxyFallback() bool {
	if s == nil || s.cfg == nil {
		return true
	}
	return s.cfg.Gateway.OpenAICore.AllowAccountProxyFallback ||
		(!s.cfg.Gateway.OpenAICore.ProductionMode && !s.cfg.Gateway.OpenAICore.EgressFailClosed)
}

func (s *OpenAIGatewayCoreService) allowDirectEgressFallback() bool {
	if s == nil || s.cfg == nil {
		return true
	}
	return s.cfg.Gateway.OpenAICore.AllowDirectFallback ||
		(!s.cfg.Gateway.OpenAICore.ProductionMode && !s.cfg.Gateway.OpenAICore.EgressFailClosed)
}

func MaskOpenAIProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	_, parsed, err := proxyurl.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" {
		return "<invalid_proxy>"
	}
	parsed.User = nil
	return parsed.String()
}

func HashOpenAIProxyURL(raw string) string {
	label := MaskOpenAIProxyURL(raw)
	if label == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}
