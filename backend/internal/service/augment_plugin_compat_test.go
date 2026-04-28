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
