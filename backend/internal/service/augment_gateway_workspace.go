package service

import (
	"os"
	"path/filepath"
	"strings"
)

type AugmentGatewayWorkspaceMetadata struct {
	WorkspaceRoot    string `json:"workspace_root,omitempty"`
	Branch           string `json:"branch,omitempty"`
	Worktree         string `json:"worktree,omitempty"`
	HasWorkspaceRoot bool   `json:"has_workspace_root"`
	HasGitMetadata   bool   `json:"has_git_metadata"`
}

func ResolveAugmentGatewayWorkspaceMetadata() AugmentGatewayWorkspaceMetadata {
	root := strings.TrimSpace(os.Getenv(augmentLegacyWorkspaceRootEnv))
	if root == "" {
		root = inferAugmentLegacyWorkspaceRoot()
	}
	return resolveAugmentGatewayWorkspaceMetadataForRoot(root)
}

func resolveAugmentGatewayWorkspaceMetadataForRoot(root string) AugmentGatewayWorkspaceMetadata {
	root = strings.TrimSpace(root)
	if root == "" {
		return AugmentGatewayWorkspaceMetadata{}
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return AugmentGatewayWorkspaceMetadata{}
	}
	info, err := os.Stat(absRoot)
	if err != nil || !info.IsDir() {
		return AugmentGatewayWorkspaceMetadata{}
	}

	metadata := AugmentGatewayWorkspaceMetadata{
		WorkspaceRoot:    absRoot,
		HasWorkspaceRoot: true,
	}
	if gitDir, worktree := augmentGatewayFindGitDir(absRoot); gitDir != "" {
		metadata.Worktree = worktree
		metadata.Branch = augmentGatewayReadGitBranch(gitDir)
		metadata.HasGitMetadata = strings.TrimSpace(metadata.Worktree) != "" || strings.TrimSpace(metadata.Branch) != ""
	}
	return metadata
}

func augmentGatewayFindGitDir(root string) (gitDir string, worktree string) {
	for candidate := filepath.Clean(root); candidate != ""; candidate = filepath.Dir(candidate) {
		dotGit := filepath.Join(candidate, ".git")
		info, err := os.Stat(dotGit)
		switch {
		case err == nil && info.IsDir():
			return dotGit, candidate
		case err == nil && !info.IsDir():
			raw, readErr := os.ReadFile(dotGit)
			if readErr == nil {
				line := strings.TrimSpace(string(raw))
				if strings.HasPrefix(line, "gitdir:") {
					path := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
					if path != "" && !filepath.IsAbs(path) {
						path = filepath.Join(candidate, path)
					}
					if abs, absErr := filepath.Abs(path); absErr == nil {
						return abs, candidate
					}
					return path, candidate
				}
			}
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
	}
	return "", ""
}

func augmentGatewayReadGitBranch(gitDir string) string {
	raw, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(raw))
	if strings.HasPrefix(head, "ref:") {
		ref := strings.TrimSpace(strings.TrimPrefix(head, "ref:"))
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	return ""
}
