package service

import (
	"os"
	"path/filepath"
	"strings"
)

type AugmentGatewayWorkspaceMetadata struct {
	WorkspaceRoot             string   `json:"workspace_root,omitempty"`
	Branch                    string   `json:"branch,omitempty"`
	Worktree                  string   `json:"worktree,omitempty"`
	WorkspaceFolders          []string `json:"workspace_folders,omitempty"`
	CurrentTerminalCWD        string   `json:"current_terminal_cwd,omitempty"`
	HasWorkspaceRoot          bool     `json:"has_workspace_root"`
	HasGitMetadata            bool     `json:"has_git_metadata"`
	HasWorkspaceFolders       bool     `json:"has_workspace_folders"`
	HasCurrentTerminalCWD     bool     `json:"has_current_terminal_cwd"`
	FileToolWorkspaceMismatch bool     `json:"file_tool_workspace_mismatch"`
}

func ResolveAugmentGatewayWorkspaceMetadata() AugmentGatewayWorkspaceMetadata {
	root := strings.TrimSpace(os.Getenv(augmentLegacyWorkspaceRootEnv))
	if root == "" {
		root = inferAugmentLegacyWorkspaceRoot()
	}
	return resolveAugmentGatewayWorkspaceMetadataForRoot(root)
}

func ResolveAugmentGatewayWorkspaceMetadataForRoot(root string) AugmentGatewayWorkspaceMetadata {
	return resolveAugmentGatewayWorkspaceMetadataForRoot(root)
}

func ResolveAugmentGatewayWorkspaceMetadataForIDEState(workspaceFolders []string, currentTerminalCWD string) AugmentGatewayWorkspaceMetadata {
	folders := augmentGatewayCanonicalWorkspaceFolders(workspaceFolders)
	cwd := augmentGatewayCanonicalWorkspaceDir(currentTerminalCWD)

	for _, candidate := range augmentGatewayWorkspaceCandidates(cwd, folders) {
		metadata := resolveAugmentGatewayWorkspaceMetadataForRoot(candidate)
		if metadata.HasWorkspaceRoot {
			return augmentGatewayWorkspaceMetadataWithIDEState(metadata, folders, cwd)
		}
	}

	return augmentGatewayWorkspaceMetadataWithIDEState(ResolveAugmentGatewayWorkspaceMetadata(), folders, cwd)
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

func augmentGatewayWorkspaceCandidates(cwd string, folders []string) []string {
	out := make([]string, 0, len(folders)+1)
	seen := make(map[string]struct{}, len(folders)+1)
	appendCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	appendCandidate(cwd)
	for _, folder := range folders {
		appendCandidate(folder)
	}
	return out
}

func augmentGatewayWorkspaceMetadataWithIDEState(metadata AugmentGatewayWorkspaceMetadata, folders []string, cwd string) AugmentGatewayWorkspaceMetadata {
	metadata.WorkspaceFolders = append([]string(nil), folders...)
	metadata.CurrentTerminalCWD = cwd
	metadata.HasWorkspaceFolders = len(folders) > 0
	metadata.HasCurrentTerminalCWD = cwd != ""
	metadata.FileToolWorkspaceMismatch = cwd != "" && len(folders) > 0 && !augmentGatewayPathWithinAnyRoot(cwd, folders)
	return metadata
}

func augmentGatewayCanonicalWorkspaceFolders(folders []string) []string {
	out := make([]string, 0, len(folders))
	seen := make(map[string]struct{}, len(folders))
	for _, folder := range folders {
		folder = augmentGatewayCanonicalWorkspaceDir(folder)
		if folder == "" {
			continue
		}
		if _, ok := seen[folder]; ok {
			continue
		}
		seen[folder] = struct{}{}
		out = append(out, folder)
	}
	return out
}

func augmentGatewayCanonicalWorkspaceDir(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return ""
	}
	return filepath.Clean(abs)
}

func augmentGatewayPathWithinAnyRoot(target string, roots []string) bool {
	target = augmentGatewayCanonicalWorkspaceDir(target)
	if target == "" {
		return false
	}
	for _, root := range roots {
		root = augmentGatewayCanonicalWorkspaceDir(root)
		if root == "" {
			continue
		}
		if target == root || strings.HasPrefix(target, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
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
