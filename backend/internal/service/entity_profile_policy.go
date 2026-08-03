package service

import (
	"errors"
	"fmt"
	"strings"
)

const (
	EntityProfilePolicyModeApprovedOnly = "approved_only"

	maxEntityProfileArtifactIDLength = 128
)

var (
	errEntityProfileArtifactIDRequired = errors.New("entity profile artifact_id is required")
	errEntityProfilePolicyModeRequired = errors.New("entity profile policy mode is required")
)

type EntityProfilePolicyArtifact struct {
	Enabled    bool   `json:"enabled"`
	ArtifactID string `json:"artifact_id,omitempty"`
	Mode       string `json:"mode,omitempty"`
}

type EntityProfilePolicyDecision struct {
	Enabled        bool
	ArtifactID     string
	Mode           string
	RuntimeApplied bool
	Audit          EntityProfilePolicyAuditFields
}

type EntityProfilePolicyAuditFields struct {
	EntityProfileArtifactID     string `json:"entity_profile_artifact_id,omitempty"`
	EntityProfilePolicyMode     string `json:"entity_profile_policy_mode,omitempty"`
	EntityProfileRuntimeApplied bool   `json:"entity_profile_runtime_applied"`
}

func (s *OpenAIGatewayService) ResolveEntityProfilePolicy(artifact EntityProfilePolicyArtifact) (EntityProfilePolicyDecision, error) {
	if !s.entityProfileOverrideEnabled() || !artifact.Enabled {
		return EntityProfilePolicyDecision{}, nil
	}

	artifactID, err := ParseEntityProfileArtifactID(artifact.ArtifactID)
	if err != nil {
		return EntityProfilePolicyDecision{}, err
	}
	mode, err := ParseEntityProfilePolicyMode(artifact.Mode)
	if err != nil {
		return EntityProfilePolicyDecision{}, err
	}

	return EntityProfilePolicyDecision{
		Enabled:        true,
		ArtifactID:     artifactID,
		Mode:           mode,
		RuntimeApplied: false,
		Audit: EntityProfilePolicyAuditFields{
			EntityProfileArtifactID:     artifactID,
			EntityProfilePolicyMode:     mode,
			EntityProfileRuntimeApplied: false,
		},
	}, nil
}

func (s *OpenAIGatewayService) entityProfileOverrideEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.OpenAICore.EntityProfileOverride.Enabled
}

func ParseEntityProfileArtifactID(raw string) (string, error) {
	artifactID := strings.TrimSpace(raw)
	if artifactID == "" {
		return "", errEntityProfileArtifactIDRequired
	}
	if len(artifactID) > maxEntityProfileArtifactIDLength {
		return "", fmt.Errorf("invalid entity profile artifact_id: length must be <= %d", maxEntityProfileArtifactIDLength)
	}
	for i, r := range artifactID {
		if isEntityProfileArtifactIDRune(r, i == 0) {
			continue
		}
		return "", fmt.Errorf("invalid entity profile artifact_id: %q", artifactID)
	}
	return artifactID, nil
}

func ParseEntityProfilePolicyMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return "", errEntityProfilePolicyModeRequired
	}
	switch mode {
	case EntityProfilePolicyModeApprovedOnly:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid entity profile policy mode: %q", raw)
	}
}

func isEntityProfileArtifactIDRune(r rune, first bool) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	if first {
		return false
	}
	return r == '-' || r == '_' || r == '.' || r == ':'
}
