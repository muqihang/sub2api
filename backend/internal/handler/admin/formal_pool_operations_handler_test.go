package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestFormalPoolOperationsHandlerWriteAccountResult_ReturnsDiagnosticsForGenericErrorWithResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &FormalPoolOperationsHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	account := &service.Account{ID: 42, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Extra: map[string]any{service.FormalPoolExtraOnboardingStage: service.FormalPoolStageHealthcheckPassed}}
	result := &service.FormalPoolOperationsAccountResult{Account: account, Diagnostics: &service.FormalPoolOperationsDiagnostics{AccountID: 42, FailureCode: "runtime_registration_failed"}}

	h.writeAccountResult(c, result, errors.New("plain service error"))

	if w.Code != http.StatusInternalServerError && w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "diagnostics") || !strings.Contains(body, "runtime_registration_failed") || !strings.Contains(body, "account") || !strings.Contains(body, "healthcheck_passed") {
		t.Fatalf("expected safe account and diagnostics in error response, got %s", body)
	}
}

func TestFormalPoolOperationsHandlerWriteAccountResult_PreservesApplicationErrorWithResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &FormalPoolOperationsHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	account := &service.Account{ID: 43, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Extra: map[string]any{service.FormalPoolExtraOnboardingStage: service.FormalPoolStageRuntimeRegistered}}
	result := &service.FormalPoolOperationsAccountResult{Account: account, Diagnostics: &service.FormalPoolOperationsDiagnostics{AccountID: 43, FailureCode: "runtime_evidence_incomplete"}}

	h.writeAccountResult(c, result, infraerrors.BadRequest("RUNTIME_EVIDENCE_INCOMPLETE", "runtime evidence is incomplete"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "RUNTIME_EVIDENCE_INCOMPLETE") || !strings.Contains(body, "runtime evidence is incomplete") || !strings.Contains(body, "diagnostics") || !strings.Contains(body, "runtime_registered") {
		t.Fatalf("expected preserved infra error and diagnostics, got %s", body)
	}
}
