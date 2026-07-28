package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bulkProbeAdminServiceStub struct {
	*stubAdminService
	resolvedIDs []int64
}

func (s *bulkProbeAdminServiceStub) BulkUpdateAccounts(_ context.Context, input *service.BulkUpdateAccountsInput) (*service.BulkUpdateAccountsResult, error) {
	input.AccountIDs = append([]int64(nil), s.resolvedIDs...)
	return nil, errors.New("post-update propagation failed")
}

type openAIResponsesProbeSubmitterRecorder struct {
	mu  sync.Mutex
	ids []int64
}

func (r *openAIResponsesProbeSubmitterRecorder) Schedule(accountID int64, _ string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, accountID)
	return true
}

func TestAccountHandlerBulkUpdate_ReprobesResolvedFilterTargetsAfterError(t *testing.T) {
	adminService := &bulkProbeAdminServiceStub{
		stubAdminService: newStubAdminService(),
		resolvedIDs:      []int64{101, 102},
	}
	recorder := &openAIResponsesProbeSubmitterRecorder{}
	accountTestService := service.NewAccountTestService(nil, nil, nil, nil, nil, nil, nil)
	accountTestService.SetOpenAIResponsesProbeScheduler(recorder)
	handler := NewAccountHandler(adminService, nil, nil, nil, nil, nil, nil, accountTestService, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/api/v1/admin/accounts/bulk-update", handler.BulkUpdate)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/bulk-update", strings.NewReader(`{
		"filters":{"platform":"openai","type":"apikey"},
		"credentials":{"base_url":"https://new-provider.example/v1"}
	}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	require.Equal(t, []int64{101, 102}, recorder.ids)
}
