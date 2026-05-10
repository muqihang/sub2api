package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var errOpenAIHTTPUpstreamNotConfigured = errors.New("openai http upstream not configured")

type openAIHTTPUpstreamTLSCacheIdentityContextKey struct{}

func WithOpenAIHTTPUpstreamTLSCacheIdentity(ctx context.Context, identity string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return ctx
	}
	return context.WithValue(ctx, openAIHTTPUpstreamTLSCacheIdentityContextKey{}, identity)
}

func OpenAIHTTPUpstreamTLSCacheIdentity(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(openAIHTTPUpstreamTLSCacheIdentityContextKey{}).(string)
	return strings.TrimSpace(value)
}

// sendOpenAIHTTPRequest centralizes OpenAI HTTP egress/TLS resolution so all
// HTTP endpoints make the same Do vs DoWithTLS decision.
func (s *OpenAIGatewayService) sendOpenAIHTTPRequest(ctx context.Context, c *gin.Context, req *http.Request, account *Account) (*http.Response, error) {
	if s == nil {
		return nil, errOpenAIHTTPUpstreamNotConfigured
	}
	return sendOpenAIHTTPRequestWithPolicy(ctx, c, req, account, s.httpUpstream, s.gatewayCoreService, s.resolveOpenAIEgress)
}

func sendOpenAIHTTPRequestWithPolicy(
	ctx context.Context,
	c *gin.Context,
	req *http.Request,
	account *Account,
	httpUpstream HTTPUpstream,
	gatewayCoreService *OpenAIGatewayCoreService,
	resolveEgress func(context.Context, *Account) (*OpenAIEgressResolution, error),
) (*http.Response, error) {
	if httpUpstream == nil {
		return nil, errOpenAIHTTPUpstreamNotConfigured
	}
	if account == nil {
		return nil, ErrAccountNilInput
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if resolveEgress == nil {
		resolveEgress = func(context.Context, *Account) (*OpenAIEgressResolution, error) {
			return buildOpenAIEgressResolution("", resolveOpenAIAccountProxyURL(account), openAIEgressSourceAccountFallback), nil
		}
	}
	egress, err := resolveEgress(ctx, account)
	if err != nil {
		return nil, err
	}

	transport := OpenAIClientTransportHTTP
	if c != nil {
		if detected := GetOpenAIClientTransport(c); detected != OpenAIClientTransportUnknown {
			transport = detected
		}
	}

	var effectiveTLS *OpenAIGatewayEffectiveTLS
	if gatewayCoreService != nil && gatewayCoreService.IsEnabled() {
		effectiveTLS, err = gatewayCoreService.ResolveEffectiveTLS(ctx, account, egress, transport)
		if err != nil {
			return nil, err
		}
	}
	recordOpenAIHTTPSendMetadata(c, egress, effectiveTLS)

	if effectiveTLS != nil && effectiveTLS.Enabled && effectiveTLS.HTTPApplicable && effectiveTLS.Profile != nil {
		if identity := strings.TrimSpace(effectiveTLS.CacheIdentity); identity != "" && req != nil {
			req = req.WithContext(WithOpenAIHTTPUpstreamTLSCacheIdentity(req.Context(), identity))
		}
		return httpUpstream.DoWithTLS(req, egress.ProxyURL, account.ID, account.Concurrency, effectiveTLS.Profile)
	}
	return httpUpstream.Do(req, egress.ProxyURL, account.ID, account.Concurrency)
}

func recordOpenAIHTTPSendMetadata(c *gin.Context, egress *OpenAIEgressResolution, tls *OpenAIGatewayEffectiveTLS) {
	if c == nil {
		return
	}
	if egress != nil {
		c.Set("openai_egress_bucket", strings.TrimSpace(egress.BucketName))
		c.Set("openai_egress_proxy_selected", egress.ProxySelected)
		if egress.ProxyHash != "" {
			c.Set("openai_egress_proxy_hash", egress.ProxyHash)
		}
	}
	if tls != nil {
		c.Set("openai_tls_enabled", tls.Enabled)
		c.Set("openai_tls_cache_identity", strings.TrimSpace(tls.CacheIdentity))
		c.Set("openai_tls_source", strings.TrimSpace(tls.Source))
		if tls.ProfileHash != "" {
			c.Set("openai_tls_profile_hash", tls.ProfileHash)
		}
	}
}

func isOpenAIEgressPolicyError(err error) bool {
	var policyErr *OpenAIEgressPolicyError
	return errors.As(err, &policyErr)
}
