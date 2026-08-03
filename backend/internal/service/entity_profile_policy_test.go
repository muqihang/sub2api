package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestEntityProfilePolicyNoOpsByDefault(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	decision, err := svc.ResolveEntityProfilePolicy(EntityProfilePolicyArtifact{
		Enabled:    true,
		ArtifactID: " entity-prof-001 ",
		Mode:       EntityProfilePolicyModeApprovedOnly,
	})

	require.NoError(t, err)
	require.False(t, decision.Enabled)
	require.Empty(t, decision.ArtifactID)
	require.Empty(t, decision.Mode)
	require.Empty(t, decision.Audit.EntityProfileArtifactID)
	require.Empty(t, decision.Audit.EntityProfilePolicyMode)
}

func TestEntityProfilePolicyParsesArtifactWhenFeatureFlagEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.EntityProfileOverride.Enabled = true
	svc := &OpenAIGatewayService{cfg: cfg}

	decision, err := svc.ResolveEntityProfilePolicy(EntityProfilePolicyArtifact{
		Enabled:    true,
		ArtifactID: " entity-prof-001 ",
		Mode:       " APPROVED_ONLY ",
	})

	require.NoError(t, err)
	require.True(t, decision.Enabled)
	require.Equal(t, "entity-prof-001", decision.ArtifactID)
	require.Equal(t, EntityProfilePolicyModeApprovedOnly, decision.Mode)
	require.False(t, decision.RuntimeApplied)
	require.Equal(t, "entity-prof-001", decision.Audit.EntityProfileArtifactID)
	require.Equal(t, EntityProfilePolicyModeApprovedOnly, decision.Audit.EntityProfilePolicyMode)
	require.False(t, decision.Audit.EntityProfileRuntimeApplied)
}

func TestEntityProfilePolicyDisabledArtifactStaysInertWhenFeatureFlagEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.EntityProfileOverride.Enabled = true
	svc := &OpenAIGatewayService{cfg: cfg}

	decision, err := svc.ResolveEntityProfilePolicy(EntityProfilePolicyArtifact{
		Enabled:    false,
		ArtifactID: "entity-prof-001",
		Mode:       EntityProfilePolicyModeApprovedOnly,
	})

	require.NoError(t, err)
	require.False(t, decision.Enabled)
	require.Empty(t, decision.ArtifactID)
	require.Empty(t, decision.Mode)
	require.Empty(t, decision.Audit.EntityProfileArtifactID)
	require.Empty(t, decision.Audit.EntityProfilePolicyMode)
}

func TestEntityProfilePolicyRejectsInvalidArtifactID(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.EntityProfileOverride.Enabled = true
	svc := &OpenAIGatewayService{cfg: cfg}

	_, err := svc.ResolveEntityProfilePolicy(EntityProfilePolicyArtifact{
		Enabled:    true,
		ArtifactID: "../entity-prof-001",
		Mode:       EntityProfilePolicyModeApprovedOnly,
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "invalid entity profile artifact_id")
}

func TestEntityProfilePolicyRejectsInvalidMode(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.EntityProfileOverride.Enabled = true
	svc := &OpenAIGatewayService{cfg: cfg}

	_, err := svc.ResolveEntityProfilePolicy(EntityProfilePolicyArtifact{
		Enabled:    true,
		ArtifactID: "entity-prof-001",
		Mode:       "force",
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "invalid entity profile policy mode")
}
