package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type OracleCompatibilityGate struct {
	Status            string `json:"status"`
	EvidenceRef       string `json:"evidence_ref"`
	AuthoritySignalID string `json:"authority_signal_id"`
}

type OracleFourCompatibilityGates struct {
	Wire             OracleCompatibilityGate `json:"wire"`
	Semantic         OracleCompatibilityGate `json:"semantic"`
	StateSequence    OracleCompatibilityGate `json:"state_sequence"`
	FailureSemantics OracleCompatibilityGate `json:"failure_semantics"`
}

type OracleBehaviorCoherenceCertificate struct {
	SchemaID                  string                       `json:"schema_id"`
	SchemaMajor               int                          `json:"schema_major"`
	SchemaRevision            int                          `json:"schema_revision"`
	Kind                      string                       `json:"kind"`
	CertificateID             string                       `json:"certificate_id"`
	PackageName               string                       `json:"package_name"`
	PackageVersion            string                       `json:"package_version"`
	PackageArtifactSHA256     string                       `json:"package_artifact_sha256"`
	BuildIdentityRef          string                       `json:"build_identity_ref"`
	Platform                  string                       `json:"platform"`
	Architecture              string                       `json:"architecture"`
	Entrypoint                string                       `json:"entrypoint"`
	AuthMode                  string                       `json:"auth_mode"`
	EnvironmentProfileRef     string                       `json:"environment_profile_ref"`
	PersonaRef                string                       `json:"persona_ref"`
	RequestASTProfileRef      string                       `json:"request_ast_profile_ref"`
	ResponseProfileRef        string                       `json:"response_profile_ref"`
	CCHPolicyRef              string                       `json:"cch_policy_ref"`
	TLSHTTPProfileRef         string                       `json:"tls_http_profile_ref"`
	ProxyGeneration           int64                        `json:"proxy_generation"`
	CredentialGeneration      int64                        `json:"credential_generation"`
	RetryPolicyRef            string                       `json:"retry_policy_ref"`
	StateSequenceRef          string                       `json:"state_sequence_ref"`
	FailureSemanticsRef       string                       `json:"failure_semantics_ref"`
	ModelCapabilitySetRef     string                       `json:"model_capability_set_ref"`
	ContractDigest            string                       `json:"contract_digest"`
	ManifestDigest            string                       `json:"manifest_digest"`
	ProfileGeneration         int64                        `json:"profile_generation"`
	SidecarProtocolGeneration int64                        `json:"sidecar_protocol_generation"`
	ReplayLedgerGeneration    int64                        `json:"replay_ledger_generation"`
	Gates                     OracleFourCompatibilityGates `json:"gates"`
	DependencyDigests         []string                     `json:"dependency_digests"`
}

type OracleAuthoritySignal struct {
	SignalID                      string   `json:"signal_id"`
	AuthorityState                string   `json:"authority_state"`
	ObservationScope              string   `json:"observation_scope"`
	ServerDependency              bool     `json:"server_dependency"`
	StabilityClass                string   `json:"stability_class"`
	Confidence                    string   `json:"confidence"`
	IssuedAtMS                    int64    `json:"issued_at_ms"`
	ExpiresAtMS                   int64    `json:"expires_at_ms"`
	Owner                         string   `json:"owner"`
	RevalidationCommandID         string   `json:"revalidation_command_id"`
	InvalidatingDependencyDigests []string `json:"invalidating_dependency_digests"`
	NegativeEvidence              []string `json:"negative_evidence"`
	ContradictoryEvidence         []string `json:"contradictory_evidence"`
	ContradictionStatus           string   `json:"contradiction_status"`
	MinimumAuthorityAfterExpiry   string   `json:"minimum_authority_after_expiry"`
	AffectedCapabilities          []string `json:"affected_capabilities"`
	FailureAction                 string   `json:"failure_action"`
}

type OracleNegativeCapabilities struct {
	Models              []string `json:"models"`
	BetaTokens          []string `json:"beta_tokens"`
	Transports          []string `json:"transports"`
	Entrypoints         []string `json:"entrypoints"`
	Fallbacks           []string `json:"fallbacks"`
	FeatureCombinations []string `json:"feature_combinations"`
	AuthorityStates     []string `json:"authority_states"`
}

type OracleAdmissionExpectedTuple struct {
	ContractDigest            string `json:"contract_digest"`
	ManifestDigest            string `json:"manifest_digest"`
	ManifestPayloadDigest     string `json:"manifest_payload_digest"`
	PackageArtifactSHA256     string `json:"package_artifact_sha256"`
	PackageVersion            string `json:"package_version"`
	ProxyGeneration           int64  `json:"proxy_generation"`
	CredentialGeneration      int64  `json:"credential_generation"`
	ProfileGeneration         int64  `json:"profile_generation"`
	SidecarProtocolGeneration int64  `json:"sidecar_protocol_generation"`
	ReplayLedgerGeneration    int64  `json:"replay_ledger_generation"`
}

type OracleAdmissionContext struct {
	NowMS                        int64                        `json:"now_ms"`
	MinimumAuthorityState        string                       `json:"minimum_authority_state"`
	Expected                     OracleAdmissionExpectedTuple `json:"expected"`
	RequestedCapabilities        []string                     `json:"requested_capabilities"`
	InvalidatedDependencyDigests []string                     `json:"invalidated_dependency_digests"`
	Signals                      []OracleAuthoritySignal      `json:"signals"`
	NegativeCapabilities         OracleNegativeCapabilities   `json:"-"`
}

type OracleAdmissionDecision struct {
	Allowed  bool   `json:"allowed"`
	Code     string `json:"code"`
	Action   string `json:"action,omitempty"`
	Gate     string `json:"gate,omitempty"`
	SignalID string `json:"signal_id,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

func decodeOracleStrictJSON(raw []byte, target any) error {
	if _, err := CanonicalizeOracleJSON(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode oracle contract: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("decode oracle contract: trailing data")
	}
	return nil
}

func validateOracleBehaviorCertificate(value OracleBehaviorCoherenceCertificate) error {
	if value.SchemaID != "oracle.compatibility" || value.SchemaMajor != 1 || value.SchemaRevision != 0 || value.Kind != "behavior_coherence_certificate" {
		return fmt.Errorf("invalid schema or kind")
	}
	required := []string{
		value.CertificateID, value.PackageName, value.PackageVersion, value.PackageArtifactSHA256,
		value.BuildIdentityRef, value.Platform, value.Architecture, value.Entrypoint, value.AuthMode,
		value.EnvironmentProfileRef, value.PersonaRef, value.RequestASTProfileRef, value.ResponseProfileRef,
		value.CCHPolicyRef, value.TLSHTTPProfileRef, value.RetryPolicyRef, value.StateSequenceRef,
		value.FailureSemanticsRef, value.ModelCapabilitySetRef, value.ContractDigest, value.ManifestDigest,
	}
	for _, item := range required {
		if item == "" {
			return fmt.Errorf("missing required field")
		}
	}
	if value.PackageName != "@anthropic-ai/claude-code" || len(value.DependencyDigests) == 0 {
		return fmt.Errorf("invalid package or dependencies")
	}
	for _, gate := range []OracleCompatibilityGate{value.Gates.Wire, value.Gates.Semantic, value.Gates.StateSequence, value.Gates.FailureSemantics} {
		if gate.Status == "" || gate.EvidenceRef == "" || gate.AuthoritySignalID == "" {
			return fmt.Errorf("invalid compatibility gate")
		}
		switch gate.Status {
		case "pass", "fail", "unsupported", "unobserved":
		default:
			return fmt.Errorf("unknown compatibility gate status")
		}
	}
	return nil
}
