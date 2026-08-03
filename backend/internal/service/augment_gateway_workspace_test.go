package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveAugmentGatewayWorkspaceMetadataDetectsGitBranchAndWorktree(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git", "refs", "heads", "feature"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".git", "HEAD"), []byte("ref: refs/heads/feature/context-bundle\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".git", "refs", "heads", "feature", "context-bundle"), []byte("0123456789012345678901234567890123456789\n"), 0o644))
	t.Setenv(augmentLegacyWorkspaceRootEnv, workspaceRoot)

	meta := ResolveAugmentGatewayWorkspaceMetadata()

	require.Equal(t, workspaceRoot, meta.WorkspaceRoot)
	require.Equal(t, "feature/context-bundle", meta.Branch)
	require.Equal(t, workspaceRoot, meta.Worktree)
	require.True(t, meta.HasWorkspaceRoot)
	require.True(t, meta.HasGitMetadata)
}

func TestResolveAugmentGatewayWorkspaceMetadataFallsBackForNonGitWorkspace(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(augmentLegacyWorkspaceRootEnv, workspaceRoot)

	meta := ResolveAugmentGatewayWorkspaceMetadata()

	require.Equal(t, workspaceRoot, meta.WorkspaceRoot)
	require.Empty(t, meta.Branch)
	require.Empty(t, meta.Worktree)
	require.True(t, meta.HasWorkspaceRoot)
	require.False(t, meta.HasGitMetadata)
}
