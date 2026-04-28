package service

import (
	"fmt"
	"sort"
	"strings"
	"time"
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
	return AugmentLegacyResolvedBlobs{
		Records:            records,
		Unknown:            unknown,
		CheckpointNotFound: checkpointNotFound,
		Namespace:          namespace,
		ResolutionReason:   resolutionReason,
	}
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

	for _, record := range blobs.Records {
		path := strings.TrimSpace(record.Path)
		if path == "" {
			path = record.BlobName
		}
		section := fmt.Sprintf("### %s\n%s\n\n", path, record.Content)
		if writeLimited(section) {
			break
		}
	}

	return strings.TrimSpace(b.String())
}

func (s *AugmentPluginService) Now() time.Time {
	return s.now()
}
