package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	augmentLegacyWorkspaceRootEnv      = "AUGMENT_LEGACY_WORKSPACE_ROOT"
	augmentLegacyWorkspaceMaxFiles     = 8000
	augmentLegacyWorkspaceMaxBytes     = 32 << 20
	augmentLegacyWorkspaceMaxFileBytes = 512 << 10
)

type AugmentLegacyUploadedBlob struct {
	BlobName string
	Path     string
	Content  string
}

type AugmentLegacySavedChat struct {
	ConversationID string
	Title          string
	Chat           []AugmentLegacySavedChatItem
}

type AugmentLegacySavedChatItem struct {
	RequestMessage string
	ResponseText   string
	RequestID      string
}

type AugmentLegacyResolvedBlobs struct {
	Records            []augmentLegacyBlobRecord
	Unknown            []string
	CheckpointNotFound bool
	Namespace          string
	ResolutionReason   string
}

func (s *AugmentPluginService) StoreLegacyBlobs(blobs []AugmentLegacyUploadedBlob) []string {
	return s.StoreLegacyBlobsForNamespace(augmentLegacyDefaultNamespace, blobs)
}

func (s *AugmentPluginService) StoreLegacyBlobsForNamespace(namespace string, blobs []AugmentLegacyUploadedBlob) []string {
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.augmentLegacyNamespaceLocked(namespace)
	stored := make([]string, 0, len(blobs))
	for _, blob := range blobs {
		name := strings.TrimSpace(blob.BlobName)
		if name == "" {
			continue
		}
		record := augmentLegacyBlobRecord{
			BlobName:   name,
			Path:       strings.TrimSpace(blob.Path),
			Content:    blob.Content,
			UploadedAt: now,
		}
		state.Blobs[name] = record
		stored = append(stored, name)
	}
	if err := s.persistLegacyCompatStateLocked(); err != nil {
		return dedupeStrings(stored)
	}
	return dedupeStrings(stored)
}

func (s *AugmentPluginService) FindLegacyMissing(blobNames []string) (unknown []string, nonindexed []string) {
	return s.FindLegacyMissingForNamespace(augmentLegacyDefaultNamespace, blobNames)
}

func (s *AugmentPluginService) FindLegacyMissingForNamespace(namespace string, blobNames []string) (unknown []string, nonindexed []string) {
	names := dedupeStrings(blobNames)
	if len(names) == 0 {
		return []string{}, []string{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.augmentLegacyNamespaceLocked(namespace)
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := state.Blobs[name]; ok {
			continue
		}
		out = append(out, name)
	}
	return out, []string{}
}

func (s *AugmentPluginService) ResolveLegacyBlobs(checkpointID string, added, deleted []string) AugmentLegacyResolvedBlobs {
	return s.ResolveLegacyBlobsForNamespace(augmentLegacyDefaultNamespace, checkpointID, added, deleted)
}

func (s *AugmentPluginService) ResolveLegacyBlobsForNamespace(namespace, checkpointID string, added, deleted []string) AugmentLegacyResolvedBlobs {
	checkpointID = strings.TrimSpace(checkpointID)
	added = dedupeStrings(added)
	deleted = dedupeStrings(deleted)

	s.mu.Lock()
	defer s.mu.Unlock()

	namespace = normalizeAugmentLegacyNamespace(namespace)
	state := s.augmentLegacyNamespaceLocked(namespace)
	active := make(map[string]struct{})
	baseNames := make(map[string]struct{})
	checkpointFound := false
	if checkpointID != "" {
		if checkpointState, ok := state.Checkpoints[checkpointID]; ok {
			checkpointFound = true
			for name := range checkpointState.BlobNames {
				baseNames[name] = struct{}{}
				active[name] = struct{}{}
			}
		} else {
			for name := range state.Blobs {
				active[name] = struct{}{}
			}
		}
	}
	for _, name := range added {
		active[name] = struct{}{}
	}
	for _, name := range deleted {
		delete(active, name)
	}

	names := make([]string, 0, len(active))
	for name := range active {
		names = append(names, name)
	}
	sort.Strings(names)

	records := make([]augmentLegacyBlobRecord, 0, len(names))
	unknown := make([]string, 0)
	checkpointNotFound := checkpointID != "" && !checkpointFound
	resolutionReason := "namespace_state_loaded"
	if checkpointID != "" && !checkpointFound {
		resolutionReason = "checkpoint_missing"
		if len(records) > 0 {
			resolutionReason = "checkpoint_missing_namespace_fallback"
		}
	} else if checkpointID == "" {
		resolutionReason = "no_checkpoint"
	}
	for _, name := range names {
		record, ok := state.Blobs[name]
		if !ok {
			unknown = append(unknown, name)
			if checkpointID != "" {
				if _, fromBaseCheckpoint := baseNames[name]; fromBaseCheckpoint || !checkpointFound {
					checkpointNotFound = true
				}
			}
			continue
		}
		records = append(records, record)
	}
	if len(records) == 0 && len(unknown) == 0 && checkpointID == "" && len(added) == 0 && len(deleted) == 0 {
		if workspaceRecords := s.loadLegacyWorkspaceFallbackRecordsLocked(); len(workspaceRecords) > 0 {
			records = workspaceRecords
			resolutionReason = "workspace_fallback_no_checkpoint"
		}
	}
	return AugmentLegacyResolvedBlobs{
		Records:            records,
		Unknown:            unknown,
		CheckpointNotFound: checkpointNotFound,
		Namespace:          namespace,
		ResolutionReason:   resolutionReason,
	}
}

func (s *AugmentPluginService) loadLegacyWorkspaceFallbackRecordsLocked() []augmentLegacyBlobRecord {
	root := strings.TrimSpace(os.Getenv(augmentLegacyWorkspaceRootEnv))
	if root == "" {
		root = inferAugmentLegacyWorkspaceRoot()
	}
	if root == "" {
		return nil
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}

	now := s.now()
	records := make([]augmentLegacyBlobRecord, 0)
	totalBytes := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == root {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if shouldSkipAugmentLegacyWorkspaceDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(records) >= augmentLegacyWorkspaceMaxFiles || totalBytes >= augmentLegacyWorkspaceMaxBytes {
			return fs.SkipAll
		}
		if !shouldIndexAugmentLegacyWorkspaceFile(name) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() <= 0 || info.Size() > augmentLegacyWorkspaceMaxFileBytes {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		contentBytes, err := os.ReadFile(path)
		if err != nil || len(contentBytes) == 0 {
			return nil
		}
		if len(contentBytes)+totalBytes > augmentLegacyWorkspaceMaxBytes {
			return fs.SkipAll
		}
		totalBytes += len(contentBytes)
		records = append(records, augmentLegacyBlobRecord{
			BlobName:   augmentLegacyWorkspaceBlobName(rel, contentBytes),
			Path:       rel,
			Content:    string(contentBytes),
			UploadedAt: now,
		})
		return nil
	})
	sort.Slice(records, func(i, j int) bool {
		return records[i].Path < records[j].Path
	})
	return records
}

func inferAugmentLegacyWorkspaceRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if filepath.Base(wd) == "backend" {
		return filepath.Dir(wd)
	}
	return ""
}

func shouldSkipAugmentLegacyWorkspaceDir(name string) bool {
	switch strings.TrimSpace(name) {
	case ".git", ".hg", ".svn", ".idea", ".vscode", ".DS_Store",
		"node_modules", "vendor", "dist", "build", "out", "target", ".next",
		"coverage", ".cache", ".turbo", ".worktrees", "tmp", "temp", "augment-compat":
		return true
	default:
		return false
	}
}

func shouldIndexAugmentLegacyWorkspaceFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".json",
		".md", ".yaml", ".yml", ".toml", ".sql", ".proto", ".sh",
		".py", ".rs", ".java", ".kt", ".swift", ".c", ".cc", ".cpp",
		".h", ".hpp", ".vue", ".svelte", ".css", ".scss", ".html":
		return true
	default:
		return false
	}
}

func augmentLegacyWorkspaceBlobName(path string, content []byte) string {
	sum := sha256.Sum256(append([]byte(path+"\x00"), content...))
	return "workspace-" + hex.EncodeToString(sum[:])
}

func (s *AugmentPluginService) SaveLegacyChat(session AugmentLegacySavedChat) {
	s.SaveLegacyChatForNamespace(augmentLegacyDefaultNamespace, session)
}

func (s *AugmentPluginService) SaveLegacyChatForNamespace(namespace string, session AugmentLegacySavedChat) {
	id := strings.TrimSpace(session.ConversationID)
	if id == "" {
		return
	}

	chatCopy := make([]augmentLegacyChatExchange, 0, len(session.Chat))
	for _, item := range session.Chat {
		chatCopy = append(chatCopy, augmentLegacyChatExchange{
			RequestMessage: item.RequestMessage,
			ResponseText:   item.ResponseText,
			RequestID:      item.RequestID,
		})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.augmentLegacyNamespaceLocked(namespace)
	state.Chats[id] = augmentLegacyChatConversation{
		ConversationID: id,
		Title:          strings.TrimSpace(session.Title),
		Chat:           chatCopy,
		UpdatedAt:      s.now(),
	}
	_ = s.persistLegacyCompatStateLocked()
}

func (s *AugmentPluginService) BuildLegacyFormattedRetrieval(informationRequest string, blobs AugmentLegacyResolvedBlobs, maxOutputLength int) string {
	limit := int(maxOutputLength)
	if limit <= 0 {
		limit = 20000
	}
	if limit > 20000 {
		limit = 20000
	}

	var b strings.Builder
	writeLimited := func(text string) bool {
		if text == "" {
			return false
		}
		remaining := limit - b.Len()
		if remaining <= 0 {
			return true
		}
		if len(text) > remaining {
			b.WriteString(text[:remaining])
			return true
		}
		b.WriteString(text)
		return false
	}

	req := strings.TrimSpace(informationRequest)
	if req != "" {
		if writeLimited("[CODEBASE_RETRIEVAL]\n") {
			return b.String()
		}
		if writeLimited("request: " + req + "\n\n") {
			return b.String()
		}
	}

	for _, record := range rankAugmentLegacyBlobRecords(req, blobs.Records) {
		path := strings.TrimSpace(record.Path)
		if path == "" {
			path = record.BlobName
		}
		section := fmt.Sprintf("### %s\n%s\n\n", path, augmentLegacyRetrievalSnippet(req, record.Content))
		if writeLimited(section) {
			break
		}
	}

	return strings.TrimSpace(b.String())
}

type augmentLegacyScoredBlobRecord struct {
	record augmentLegacyBlobRecord
	score  int
}

func rankAugmentLegacyBlobRecords(query string, records []augmentLegacyBlobRecord) []augmentLegacyBlobRecord {
	if len(records) <= 1 {
		return records
	}

	tokens := augmentLegacyRetrievalQueryTokens(query)
	scored := make([]augmentLegacyScoredBlobRecord, 0, len(records))
	for _, record := range records {
		scored = append(scored, augmentLegacyScoredBlobRecord{
			record: record,
			score:  augmentLegacyBlobRecordScore(query, tokens, record),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		leftPath := strings.TrimSpace(scored[i].record.Path)
		if leftPath == "" {
			leftPath = scored[i].record.BlobName
		}
		rightPath := strings.TrimSpace(scored[j].record.Path)
		if rightPath == "" {
			rightPath = scored[j].record.BlobName
		}
		return leftPath < rightPath
	})

	ranked := make([]augmentLegacyBlobRecord, 0, len(scored))
	for _, item := range scored {
		ranked = append(ranked, item.record)
	}
	return ranked
}

func augmentLegacyBlobRecordScore(query string, tokens []string, record augmentLegacyBlobRecord) int {
	queryLower := strings.ToLower(strings.TrimSpace(query))
	pathLower := strings.ToLower(strings.TrimSpace(record.Path))
	if pathLower == "" {
		pathLower = strings.ToLower(strings.TrimSpace(record.BlobName))
	}
	contentLower := strings.ToLower(record.Content)

	score := 0
	if queryLower != "" {
		if strings.Contains(pathLower, queryLower) {
			score += 180
		}
		if strings.Contains(contentLower, queryLower) {
			score += 120
		}
		for _, exact := range augmentLegacyRetrievalExactPhrases(queryLower) {
			if strings.Contains(pathLower, exact) {
				score += 240
			}
			if strings.Contains(contentLower, exact) {
				score += 300
			}
		}
	}

	for _, token := range tokens {
		if len(token) < 3 {
			continue
		}
		if strings.Contains(pathLower, token) {
			score += 35
		}
		if strings.Contains(contentLower, token) {
			score += 4
		}
		if len(token) >= 12 && strings.Contains(contentLower, token) {
			score += 360
		}
		if augmentLegacyLooksLikeIdentifier(token) && !augmentLegacyRetrievalStopToken(token) && augmentLegacyContentLooksLikeDefinition(contentLower, token) {
			score += 260
		}
	}

	if augmentLegacyPathLooksLikeBusinessSource(pathLower) {
		score += 180
	}
	if augmentLegacyPathLooksLikeRootDocumentation(pathLower) {
		score -= 260
	}

	switch {
	case strings.Contains(pathLower, "/ent/"),
		strings.Contains(pathLower, "ent/"),
		strings.Contains(pathLower, "generated"),
		strings.Contains(contentLower, "code generated"),
		strings.Contains(contentLower, "generated code"):
		score -= 160
	case strings.Contains(pathLower, "/docs/") || strings.HasPrefix(pathLower, "docs/"):
		score -= 25
	}

	if strings.Contains(pathLower, "_test.go") && !augmentLegacyQueryMentionsTests(queryLower) {
		score -= 400
	}

	return score
}

func augmentLegacyPathLooksLikeBusinessSource(pathLower string) bool {
	if strings.Contains(pathLower, "backend/internal/") ||
		strings.Contains(pathLower, "frontend/src/") ||
		strings.Contains(pathLower, "cmd/server/") {
		return true
	}
	switch filepath.Ext(pathLower) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".vue", ".py", ".rs", ".java", ".kt", ".swift":
		return true
	default:
		return false
	}
}

func augmentLegacyPathLooksLikeRootDocumentation(pathLower string) bool {
	base := filepath.Base(pathLower)
	switch base {
	case "readme.md", "readme_cn.md", "readme_ja.md", "changelog.md", "dev_guide.md":
		return true
	default:
		return strings.HasPrefix(pathLower, "docs/")
	}
}

func augmentLegacyRetrievalExactPhrases(queryLower string) []string {
	phrases := make([]string, 0)
	for _, part := range strings.Fields(queryLower) {
		part = strings.Trim(part, " \t\r\n.,;:()[]{}'\"`")
		if strings.Contains(part, "/") || strings.Contains(part, "-") || strings.Contains(part, "_") || strings.Contains(part, ".") {
			if part != "" {
				phrases = append(phrases, part)
			}
		}
	}
	return phrases
}

func augmentLegacyRetrievalQueryTokens(query string) []string {
	seen := make(map[string]struct{})
	tokens := make([]string, 0)
	for _, token := range strings.FieldsFunc(query, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') &&
			!(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') &&
			r != '_'
	}) {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	return tokens
}

func augmentLegacyLooksLikeIdentifier(token string) bool {
	if len(token) < 6 {
		return false
	}
	for _, r := range token {
		if r == '_' || (r >= '0' && r <= '9') {
			return true
		}
	}
	return len(token) >= 12
}

func augmentLegacyRetrievalStopToken(token string) bool {
	switch token {
	case "service", "handler", "route", "routes", "runtime", "request", "response",
		"retrieval", "codebase", "context", "engine", "gateway", "backend",
		"frontend", "internal", "package", "function", "method", "struct":
		return true
	default:
		return false
	}
}

func augmentLegacyContentLooksLikeDefinition(contentLower string, token string) bool {
	if !strings.Contains(contentLower, token) {
		return false
	}
	return strings.Contains(contentLower, "func ") ||
		strings.Contains(contentLower, "func (") ||
		strings.Contains(contentLower, "type ") ||
		strings.Contains(contentLower, "const ") ||
		strings.Contains(contentLower, "var ")
}

func augmentLegacyQueryMentionsTests(queryLower string) bool {
	return strings.Contains(queryLower, "test") || strings.Contains(queryLower, "测试")
}

func augmentLegacyRetrievalSnippet(query string, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= 30 {
		return content
	}

	matchIndex := augmentLegacyBestSnippetLine(query, lines)
	if matchIndex < 0 {
		return content
	}

	start := matchIndex - 1
	if start < 0 {
		start = 0
	}
	end := matchIndex + 2
	if end > len(lines) {
		end = len(lines)
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

func augmentLegacyBestSnippetLine(query string, lines []string) int {
	tokens := augmentLegacyRetrievalQueryTokens(query)
	phrases := augmentLegacyRetrievalExactPhrases(strings.ToLower(query))
	bestIndex := -1
	bestScore := 0
	for index, line := range lines {
		lineLower := strings.ToLower(line)
		score := 0
		for _, phrase := range phrases {
			if strings.Contains(lineLower, phrase) {
				score += 10
			}
		}
		for _, token := range tokens {
			if len(token) >= 3 && strings.Contains(lineLower, token) {
				score += 2
			}
		}
		if score > bestScore {
			bestScore = score
			bestIndex = index
		}
	}
	return bestIndex
}

func (s *AugmentPluginService) Now() time.Time {
	return s.now()
}
