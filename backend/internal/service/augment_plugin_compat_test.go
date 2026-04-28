package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestResolveLegacyBlobsFallsBackToWorkspaceRootWhenNoCheckpointStateExists(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "README.md"), []byte(strings.Repeat("service handler route overview\n", 80)), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, "backend/internal/server/routes"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "backend/internal/server/routes/gateway.go"), []byte(`package routes

func registerGatewayRoutes() {
	r.POST("/agents/codebase-retrieval", h.Auth.AugmentLegacyCodebaseRetrieval)
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "node_modules/noise.js"), []byte(`const noise = true`), 0o644))

	t.Setenv("AUGMENT_LEGACY_WORKSPACE_ROOT", workspaceRoot)

	svc := NewAugmentPluginService(&config.Config{}, nil, nil, nil, nil, nil)
	resolved := svc.ResolveLegacyBlobsForNamespace("workspace-session-1", "", nil, nil)

	require.False(t, resolved.CheckpointNotFound)
	require.Equal(t, "workspace_fallback_no_checkpoint", resolved.ResolutionReason)
	require.GreaterOrEqual(t, len(resolved.Records), 2)
	formatted := svc.BuildLegacyFormattedRetrieval("find AugmentLegacyCodebaseRetrieval route handler service", resolved, 2000)
	require.Contains(t, formatted, "backend/internal/server/routes/gateway.go")
	require.Contains(t, formatted, "AugmentLegacyCodebaseRetrieval")
	require.NotContains(t, formatted, "node_modules")
}

func TestRankAugmentLegacyBlobRecordsPrioritizesExactLongSymbol(t *testing.T) {
	t.Parallel()

	records := []augmentLegacyBlobRecord{
		{Path: "README.md", Content: strings.Repeat("AugmentLegacyCodebaseRetrieval service handler route overview\n", 200)},
		{Path: "backend/internal/service/noisy.go", Content: strings.Repeat("package service\nfunc helper() { serviceHandlerRoute() }\n", 80)},
		{Path: "backend/internal/server/routes/gateway.go", Content: `package routes

func registerGatewayRoutes() {
	r.POST("/agents/codebase-retrieval", h.Auth.AugmentLegacyCodebaseRetrieval)
}
`},
	}

	ranked := rankAugmentLegacyBlobRecords("find AugmentLegacyCodebaseRetrieval route handler service", records)

	require.Equal(t, "backend/internal/server/routes/gateway.go", ranked[0].Path)
}

func TestBuildLegacyFormattedRetrievalPrioritizesExactHandlerEvidence(t *testing.T) {
	t.Parallel()

	prompt := "Find `AugmentLegacyCodebaseRetrieval`. Report the exact file path, request struct fields, service methods it calls, and JSON field returned to the client. Use codebase retrieval evidence only."
	records := []augmentLegacyBlobRecord{
		{
			Path:    "backend/internal/service/gateway_service.go",
			Content: strings.Repeat("package service\nfunc GatewayService() { requestServiceHandlerRouteJSONField() }\n", 80),
		},
		{
			Path:    "frontend/src/i18n/locales/en.ts",
			Content: strings.Repeat("service request response field route handler evidence\n", 120),
		},
		{
			Path: "backend/internal/server/routes/gateway.go",
			Content: `package routes

func RegisterGatewayRoutes(r Router, h *Handlers) {
	r.POST("/agents/codebase-retrieval", h.Auth.AugmentLegacyCodebaseRetrieval)
}
`,
		},
		{
			Path: "backend/internal/handler/auth_augment_runtime.go",
			Content: `package handler

type augmentLegacyCodebaseRetrievalRequest struct {
	InformationRequest string ` + "`json:\"information_request\"`" + `
	Blobs augmentLegacyCheckpointBlobsPayload ` + "`json:\"blobs\"`" + `
	Dialog []map[string]any ` + "`json:\"dialog\"`" + `
	MaxOutputLength int ` + "`json:\"max_output_length\"`" + `
	DisableCodebaseRetrieval bool ` + "`json:\"disable_codebase_retrieval\"`" + `
	EnableCommitRetrieval bool ` + "`json:\"enable_commit_retrieval\"`" + `
	EnableConversationRetrieval bool ` + "`json:\"enable_conversation_retrieval\"`" + `
}

func (h *AuthHandler) AugmentLegacyCodebaseRetrieval(c *gin.Context) {
	var req augmentLegacyCodebaseRetrievalRequest
	namespace := h.augmentLegacyNamespace(c, principal)
	records := h.augmentPluginService.ResolveLegacyBlobsForNamespace(namespace, req.Blobs.CheckpointID, req.Blobs.AddedBlobs, req.Blobs.DeletedBlobs)
	text := h.augmentPluginService.BuildLegacyFormattedRetrieval(req.InformationRequest, records, req.MaxOutputLength)
	c.JSON(http.StatusOK, gin.H{"formatted_retrieval": text})
}
`,
		},
		{
			Path: "backend/internal/service/augment_plugin_compat.go",
			Content: `package service

func (s *AugmentPluginService) ResolveLegacyBlobsForNamespace(namespace, checkpointID string, added, deleted []string) AugmentLegacyResolvedBlobs {
	return AugmentLegacyResolvedBlobs{}
}

func (s *AugmentPluginService) BuildLegacyFormattedRetrieval(informationRequest string, blobs AugmentLegacyResolvedBlobs, maxOutputLength int) string {
	return "formatted retrieval"
}
`,
		},
		{
			Path: "backend/internal/handler/auth_augment_runtime_test.go",
			Content: `package handler

func TestRoute(t *testing.T) {
	router.POST("/agents/codebase-retrieval", authHandler.AugmentLegacyCodebaseRetrieval)
}
`,
		},
	}

	formatted := (&AugmentPluginService{}).BuildLegacyFormattedRetrieval(prompt, AugmentLegacyResolvedBlobs{Records: records}, 6000)

	handlerIndex := strings.Index(formatted, "backend/internal/handler/auth_augment_runtime.go")
	serviceIndex := strings.Index(formatted, "backend/internal/service/augment_plugin_compat.go")
	routeIndex := strings.Index(formatted, "backend/internal/server/routes/gateway.go")
	noiseIndex := strings.Index(formatted, "backend/internal/service/gateway_service.go")
	require.NotEqual(t, -1, handlerIndex)
	require.NotEqual(t, -1, serviceIndex)
	require.NotEqual(t, -1, routeIndex)
	require.NotEqual(t, -1, noiseIndex)
	require.Less(t, handlerIndex, noiseIndex)
	require.Less(t, serviceIndex, noiseIndex)
	require.Less(t, routeIndex, noiseIndex)
	require.Contains(t, formatted, "type augmentLegacyCodebaseRetrievalRequest struct")
	require.Contains(t, formatted, "InformationRequest string")
	require.Contains(t, formatted, "func (h *AuthHandler) AugmentLegacyCodebaseRetrieval")
	require.Contains(t, formatted, "ResolveLegacyBlobsForNamespace")
	require.Contains(t, formatted, "BuildLegacyFormattedRetrieval")
	require.Contains(t, formatted, `"formatted_retrieval"`)
}
