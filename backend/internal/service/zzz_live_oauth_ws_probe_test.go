package service

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/Wei-Shaw/sub2api/internal/config"
    coderws "github.com/coder/websocket"
    "github.com/gin-gonic/gin"
    "github.com/stretchr/testify/require"
    "github.com/tidwall/gjson"
)

func TestLiveOAuthWSProbe_CurrentCode_FirstTurnAndContinuation(t *testing.T) {
    if os.Getenv("LIVE_WS_PROBE") != "1" { t.Skip("set LIVE_WS_PROBE=1 to run") }
    gin.SetMode(gin.TestMode)
    accessToken := os.Getenv("LIVE_ACCESS_TOKEN")
    chatgptAccountID := os.Getenv("LIVE_CHATGPT_ACCOUNT_ID")
    require.NotEmpty(t, accessToken)
    require.NotEmpty(t, chatgptAccountID)
    cfg := &config.Config{}
    cfg.Security.URLAllowlist.Enabled = false
    cfg.Security.URLAllowlist.AllowInsecureHTTP = true
    cfg.Gateway.OpenAIWS.Enabled = true
    cfg.Gateway.OpenAIWS.OAuthEnabled = true
    cfg.Gateway.OpenAIWS.APIKeyEnabled = true
    cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
    cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
    cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
    cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
    cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
    cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
    cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 10
    cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 60
    cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 30
    svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: newOpenAIWSConnPool(cfg)}
    account := &Account{ID: 42, Name: "live-oauth-ws-probe", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": accessToken, "chatgpt_account_id": chatgptAccountID}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
    serverErrCh := make(chan error, 1)
    wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover}); if err != nil { serverErrCh <- err; return }
        defer conn.CloseNow()
        rec := httptest.NewRecorder(); ginCtx, _ := gin.CreateTestContext(rec)
        req := r.Clone(r.Context()); req.Header = req.Header.Clone(); req.Header.Set("User-Agent", "codex_cli_rs/0.104.0"); ginCtx.Request = req
        readCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second); msgType, firstMessage, readErr := conn.Read(readCtx); cancel(); if readErr != nil { serverErrCh <- readErr; return }
        if msgType != coderws.MessageText && msgType != coderws.MessageBinary { serverErrCh <- fmt.Errorf("unsupported websocket client message type: %s", msgType.String()); return }
        serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, accessToken, firstMessage, nil)
    })); defer wsServer.Close()
    dialCtx, cancelDial := context.WithTimeout(context.Background(), 10*time.Second); clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil); cancelDial(); require.NoError(t, err); defer clientConn.CloseNow()
    send := func(payload string) { writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel(); require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(payload))) }
    recvTerminal := func(timeout time.Duration) ([]byte, map[string]any) { deadline := time.Now().Add(timeout); for time.Now().Before(deadline) { readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second); msgType, message, err := clientConn.Read(readCtx); cancel(); require.NoError(t, err); require.Equal(t, coderws.MessageText, msgType); var obj map[string]any; require.NoError(t, json.Unmarshal(message, &obj)); typ, _ := obj["type"].(string); if typ == "response.completed" || typ == "response.done" || typ == "error" { return message, obj } }; t.Fatal("timeout waiting terminal event"); return nil, nil }
    sid := fmt.Sprintf("live-probe-%d", time.Now().UnixNano())
    send(fmt.Sprintf(`{"type":"response.create","model":"gpt-5.4","stream":false,"store":true,"service_tier":"priority","prompt_cache_key":"%s","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with ONLY: PROBE1"}]}]}`, sid))
    firstRaw, firstObj := recvTerminal(120*time.Second); require.Equal(t, "response.completed", firstObj["type"]); firstID := gjson.GetBytes(firstRaw, "response.id").String(); require.NotEmpty(t, firstID)
    send(fmt.Sprintf(`{"type":"response.create","model":"gpt-5.4","stream":false,"store":true,"service_tier":"priority","prompt_cache_key":"%s","previous_response_id":"%s","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with ONLY: PROBE2"}]}]}`, sid, firstID))
    secondRaw, secondObj := recvTerminal(120*time.Second); require.Equal(t, "response.completed", secondObj["type"]); secondID := gjson.GetBytes(secondRaw, "response.id").String(); require.NotEmpty(t, secondID); require.NotEqual(t, firstID, secondID)
    require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done")); select { case err := <-serverErrCh: require.NoError(t, err); case <-time.After(20 * time.Second): t.Fatal("timeout waiting server close") }
}

func TestLiveOAuthWSProbe_CurrentCode_WarmupThenActual(t *testing.T) {
    if os.Getenv("LIVE_WS_PROBE") != "1" { t.Skip("set LIVE_WS_PROBE=1 to run") }
    gin.SetMode(gin.TestMode)
    accessToken := os.Getenv("LIVE_ACCESS_TOKEN")
    chatgptAccountID := os.Getenv("LIVE_CHATGPT_ACCOUNT_ID")
    require.NotEmpty(t, accessToken)
    require.NotEmpty(t, chatgptAccountID)

    cfg := &config.Config{}
    cfg.Security.URLAllowlist.Enabled = false
    cfg.Security.URLAllowlist.AllowInsecureHTTP = true
    cfg.Gateway.OpenAIWS.Enabled = true
    cfg.Gateway.OpenAIWS.OAuthEnabled = true
    cfg.Gateway.OpenAIWS.APIKeyEnabled = true
    cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
    cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
    cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
    cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
    cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
    cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
    cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 10
    cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 60
    cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 30

    svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: newOpenAIWSConnPool(cfg)}
    account := &Account{ID: 42, Name: "live-oauth-ws-probe", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": accessToken, "chatgpt_account_id": chatgptAccountID}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}

    serverErrCh := make(chan error, 1)
    wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover}); if err != nil { serverErrCh <- err; return }
        defer conn.CloseNow()
        rec := httptest.NewRecorder(); ginCtx, _ := gin.CreateTestContext(rec)
        req := r.Clone(r.Context()); req.Header = req.Header.Clone(); req.Header.Set("User-Agent", "codex_cli_rs/0.104.0"); ginCtx.Request = req
        readCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second); msgType, firstMessage, readErr := conn.Read(readCtx); cancel(); if readErr != nil { serverErrCh <- readErr; return }
        if msgType != coderws.MessageText && msgType != coderws.MessageBinary { serverErrCh <- fmt.Errorf("unsupported websocket client message type: %s", msgType.String()); return }
        serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, accessToken, firstMessage, nil)
    })); defer wsServer.Close()

    dialCtx, cancelDial := context.WithTimeout(context.Background(), 10*time.Second)
    clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
    cancelDial(); require.NoError(t, err); defer clientConn.CloseNow()
    send := func(payload string) { writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel(); require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(payload))) }
    recvTerminal := func(timeout time.Duration) ([]byte, map[string]any) { deadline := time.Now().Add(timeout); for time.Now().Before(deadline) { readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second); msgType, message, err := clientConn.Read(readCtx); cancel(); require.NoError(t, err); require.Equal(t, coderws.MessageText, msgType); var obj map[string]any; require.NoError(t, json.Unmarshal(message, &obj)); typ, _ := obj["type"].(string); if typ == "response.completed" || typ == "response.done" || typ == "error" { return message, obj } }; t.Fatal("timeout waiting terminal event"); return nil, nil }

    sid := fmt.Sprintf("live-warmup-%d", time.Now().UnixNano())
    send(fmt.Sprintf(`{"type":"response.create","generate":false,"model":"gpt-5.4","stream":false,"store":true,"service_tier":"priority","prompt_cache_key":"%s","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"warmup"}]}]}`, sid))
    warmRaw, warmObj := recvTerminal(120*time.Second)
    require.Equal(t, "response.completed", warmObj["type"])
    warmID := gjson.GetBytes(warmRaw, "response.id").String()
    require.NotEmpty(t, warmID)

    send(fmt.Sprintf(`{"type":"response.create","model":"gpt-5.4","stream":false,"store":true,"service_tier":"priority","prompt_cache_key":"%s","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with ONLY: AFTER_WARMUP_OK"}]}]}`, sid))
    actualRaw, actualObj := recvTerminal(120*time.Second)
    require.Equal(t, "response.completed", actualObj["type"])
    actualID := gjson.GetBytes(actualRaw, "response.id").String()
    require.NotEmpty(t, actualID)

    require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
    select { case err := <-serverErrCh: require.NoError(t, err); case <-time.After(20 * time.Second): t.Fatal("timeout waiting server close") }
}
