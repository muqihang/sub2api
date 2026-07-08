package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWeChatOAuthStartCapturesPromoCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	values := map[string]string{
		service.SettingKeyRegistrationEnabled:              "true",
		service.SettingKeyInvitationCodeEnabled:            "false",
		service.SettingKeyWeChatConnectEnabled:             "true",
		service.SettingKeyWeChatConnectAppID:               "wx-open-app",
		service.SettingKeyWeChatConnectAppSecret:           "wx-open-secret",
		service.SettingKeyWeChatConnectMode:                "open",
		service.SettingKeyWeChatConnectScopes:              "snsapi_login",
		service.SettingKeyWeChatConnectOpenEnabled:         "true",
		service.SettingKeyWeChatConnectMPEnabled:           "false",
		service.SettingKeyWeChatConnectOpenAppID:           "wx-open-app",
		service.SettingKeyWeChatConnectOpenAppSecret:       "wx-open-secret",
		service.SettingKeyWeChatConnectRedirectURL:         "https://api.example.com/api/v1/auth/oauth/wechat/callback",
		service.SettingKeyWeChatConnectFrontendRedirectURL: "/auth/wechat/callback",
	}
	handler := &AuthHandler{
		settingSvc: service.NewSettingService(&wechatPromoSettingRepoStub{values: values}, &config.Config{}),
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/wechat/start?mode=open&redirect=/billing&promo_code=WECHATPROMO", nil)
	c.Request.Host = "api.example.com"

	handler.WeChatOAuthStart(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	promoCookie := findCookie(recorder.Result().Cookies(), oauthPromoCodeCookieName)
	require.NotNil(t, promoCookie)
	require.Equal(t, "WECHATPROMO", decodeCookieValueForTest(t, promoCookie.Value))
	require.True(t, promoCookie.HttpOnly)
}

type wechatPromoSettingRepoStub struct {
	values map[string]string
}

func (r *wechatPromoSettingRepoStub) Get(_ context.Context, key string) (*service.Setting, error) {
	return &service.Setting{Key: key, Value: r.values[key], UpdatedAt: time.Now().UTC()}, nil
}

func (r *wechatPromoSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}

func (r *wechatPromoSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *wechatPromoSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *wechatPromoSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *wechatPromoSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *wechatPromoSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}
