package service

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
)

var (
	OracleManifestAuthorityDomain     = []byte("oracle-manifest-v1\x00")
	OracleCheckpointAuthorityDomain   = []byte("oracle-checkpoint-v1\x00")
	OracleRootRotationAuthorityDomain = []byte("oracle-root-rotation-v1\x00")
	OracleRevocationAuthorityDomain   = []byte("oracle-revocation-v1\x00")
)

type OracleTrustKey struct {
	KeyID     string
	Role      string
	Epoch     int64
	Revoked   bool
	PublicKey ed25519.PublicKey
}

type OracleAuthorityThresholds struct {
	Root       int `json:"root"`
	Manifest   int `json:"manifest"`
	Checkpoint int `json:"checkpoint"`
	Revocation int `json:"revocation"`
}

type OracleTrustState struct {
	RootEpoch             int64
	PolicyVersion         int64
	RollbackFloor         int64
	RevocationVersion     int64
	ManifestDigest        string
	ManifestPayloadDigest string
	CheckpointVersion     int64
	CheckpointDigest      string
	ReplicaGeneration     int64
	LastWallClockMS       int64
	Keys                  map[string]OracleTrustKey
	Thresholds            OracleAuthorityThresholds
	RollbackTargets       map[string]OracleRollbackTarget
}

type OracleRollbackTarget struct {
	PolicyVersion int64 `json:"policyVersion"`
	Revoked       bool  `json:"revoked"`
}

type OracleAuthorityManifest struct {
	SchemaID                      string   `json:"schemaId"`
	SchemaMajor                   int      `json:"schemaMajor"`
	SchemaRevision                int      `json:"schemaRevision"`
	Kind                          string   `json:"kind"`
	ManifestID                    string   `json:"manifestId"`
	PolicyVersion                 int64    `json:"policyVersion"`
	ParentDigest                  string   `json:"parentDigest"`
	RollbackDigest                string   `json:"rollbackDigest"`
	ContractDigest                string   `json:"contractDigest"`
	ManifestPayloadDigest         string   `json:"manifestPayloadDigest"`
	IssuedAtMS                    int64    `json:"issuedAtMs"`
	ExpiresAtMS                   int64    `json:"expiresAtMs"`
	SourcePackageDigests          []string `json:"sourcePackageDigests"`
	PromotionRefs                 []string `json:"promotionRefs"`
	WitnessCheckpointDigest       string   `json:"witnessCheckpointDigest"`
	InvalidatingDependencyDigests []string `json:"invalidatingDependencyDigests"`
}

type OracleAuthorityCheckpoint struct {
	SchemaID                 string `json:"schemaId"`
	SchemaMajor              int    `json:"schemaMajor"`
	SchemaRevision           int    `json:"schemaRevision"`
	Kind                     string `json:"kind"`
	Version                  int64  `json:"version"`
	ManifestDigest           string `json:"manifestDigest"`
	PreviousCheckpointDigest string `json:"previousCheckpointDigest"`
	WitnessCheckpointDigest  string `json:"witnessCheckpointDigest"`
	IssuedAtMS               int64  `json:"issuedAtMs"`
	ExpiresAtMS              int64  `json:"expiresAtMs"`
}

type OracleAuthoritySignature struct {
	Algorithm          string `json:"algorithm"`
	KeyID              string `json:"keyId"`
	KeyEpoch           int64  `json:"keyEpoch"`
	Role               string `json:"role"`
	SignatureBase64URL string `json:"signatureBase64url"`
}

type OracleManifestAuthorityUpdate struct {
	Manifest             OracleAuthorityManifest
	ManifestSignatures   []OracleAuthoritySignature
	Checkpoint           OracleAuthorityCheckpoint
	CheckpointSignatures []OracleAuthoritySignature
}

type OracleManifestAuthorityContext struct {
	NowWallClockMS               int64
	MonotonicElapsedMS           int64
	MaximumClockRollbackMS       int64
	MaximumCheckpointAgeMS       int64
	ExpectedReplicaGeneration    int64
	InvalidatedDependencyDigests []string
	WitnessedCheckpoints         map[int64]string
}

type OracleRootRotationKey struct {
	KeyID                  string `json:"keyId"`
	Role                   string `json:"role"`
	Epoch                  int64  `json:"epoch"`
	PublicKeySPKIBase64URL string `json:"publicKeySpkiBase64url"`
}

type OracleRootRotation struct {
	SchemaID         string                  `json:"schemaId"`
	SchemaMajor      int                     `json:"schemaMajor"`
	SchemaRevision   int                     `json:"schemaRevision"`
	Kind             string                  `json:"kind"`
	OldEpoch         int64                   `json:"oldEpoch"`
	NewEpoch         int64                   `json:"newEpoch"`
	NewRootThreshold int                     `json:"newRootThreshold"`
	NewKeys          []OracleRootRotationKey `json:"newKeys"`
}

type OracleAuthorityRevocation struct {
	SchemaID       string   `json:"schemaId"`
	SchemaMajor    int      `json:"schemaMajor"`
	SchemaRevision int      `json:"schemaRevision"`
	Kind           string   `json:"kind"`
	Version        int64    `json:"version"`
	KeyEpoch       int64    `json:"keyEpoch"`
	IssuedAtMS     int64    `json:"issuedAtMs"`
	ExpiresAtMS    int64    `json:"expiresAtMs"`
	RevokedKeyIDs  []string `json:"revokedKeyIds"`
	ReasonRef      string   `json:"reasonRef"`
}

type OracleAuthorityDecision struct {
	Allowed         bool
	Code            string
	NextState       *OracleTrustState
	NextStateDigest string
}

func OracleDomainSeparatedJCS(domain []byte, value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	canonical, err := CanonicalizeOracleJSON(raw)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 0, len(domain)+len(canonical.Canonical))
	result = append(result, domain...)
	result = append(result, canonical.Canonical...)
	return result, nil
}

func OracleAuthorityObjectDigest(domain []byte, value any) (string, error) {
	bytes, err := OracleDomainSeparatedJCS(domain, value)
	if err != nil {
		return "", err
	}
	return oracleSHA256Hex(bytes), nil
}

func oracleSHA256Hex(value []byte) string {
	return sha256HexBytes(value)
}

func sha256HexBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

type oracleTrustKeyMetadata struct {
	KeyID                  string `json:"keyId"`
	Role                   string `json:"role"`
	Epoch                  int64  `json:"epoch"`
	Revoked                bool   `json:"revoked"`
	PublicKeySPKIBase64URL string `json:"publicKeySpkiBase64url"`
}

func OracleTrustStateDigest(state OracleTrustState) (string, error) {
	metadata := make([]oracleTrustKeyMetadata, 0, len(state.Keys))
	for _, key := range state.Keys {
		der, err := x509.MarshalPKIXPublicKey(key.PublicKey)
		if err != nil {
			return "", err
		}
		metadata = append(metadata, oracleTrustKeyMetadata{KeyID: key.KeyID, Role: key.Role, Epoch: key.Epoch, Revoked: key.Revoked, PublicKeySPKIBase64URL: base64.RawURLEncoding.EncodeToString(der)})
	}
	sort.Slice(metadata, func(i, j int) bool { return metadata[i].KeyID < metadata[j].KeyID })
	snapshot := struct {
		CheckpointDigest      string                          `json:"checkpointDigest"`
		CheckpointVersion     int64                           `json:"checkpointVersion"`
		KeyMetadata           []oracleTrustKeyMetadata        `json:"keyMetadata"`
		LastWallClockMS       int64                           `json:"lastWallClockMs"`
		ManifestDigest        string                          `json:"manifestDigest"`
		ManifestPayloadDigest string                          `json:"manifestPayloadDigest"`
		PolicyVersion         int64                           `json:"policyVersion"`
		ReplicaGeneration     int64                           `json:"replicaGeneration"`
		RevocationVersion     int64                           `json:"revocationVersion"`
		RollbackFloor         int64                           `json:"rollbackFloor"`
		RootEpoch             int64                           `json:"rootEpoch"`
		RollbackTargets       map[string]OracleRollbackTarget `json:"rollbackTargets"`
		Thresholds            OracleAuthorityThresholds       `json:"thresholds"`
	}{state.CheckpointDigest, state.CheckpointVersion, metadata, state.LastWallClockMS, state.ManifestDigest, state.ManifestPayloadDigest, state.PolicyVersion, state.ReplicaGeneration, state.RevocationVersion, state.RollbackFloor, state.RootEpoch, state.RollbackTargets, state.Thresholds}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	canonical, err := CanonicalizeOracleJSON(raw)
	if err != nil {
		return "", err
	}
	return canonical.SHA256, nil
}

func oracleAuthorityDeny(code string) OracleAuthorityDecision {
	return OracleAuthorityDecision{Code: code}
}

func verifyOracleAuthorityThreshold(signed any, signatures []OracleAuthoritySignature, role string, epoch int64, keys map[string]OracleTrustKey, threshold int, domain []byte) string {
	if len(signatures) > 64 || len(keys) > 64 {
		return "authority_resource_limit"
	}
	if threshold < 1 || threshold > 64 {
		return "authority_threshold_insufficient"
	}
	bytes, err := OracleDomainSeparatedJCS(domain, signed)
	if err != nil {
		return "authority_signature_invalid"
	}
	seen := make(map[string]struct{})
	valid := 0
	var observedEpoch int64
	observedEpochInitialized := false
	for _, signature := range signatures {
		if _, exists := seen[signature.KeyID]; exists {
			return "authority_duplicate_signer"
		}
		seen[signature.KeyID] = struct{}{}
		key, exists := keys[signature.KeyID]
		if !exists || key.Role != role || signature.Role != role || key.Epoch != signature.KeyEpoch || epoch != 0 && signature.KeyEpoch != epoch || observedEpochInitialized && signature.KeyEpoch != observedEpoch {
			return "authority_wrong_role"
		}
		observedEpoch = signature.KeyEpoch
		observedEpochInitialized = true
		if key.Revoked {
			return "authority_key_revoked"
		}
		raw, err := base64.RawURLEncoding.DecodeString(signature.SignatureBase64URL)
		if err != nil || signature.Algorithm != "Ed25519" || len(raw) != ed25519.SignatureSize || !ed25519.Verify(key.PublicKey, bytes, raw) {
			return "authority_signature_invalid"
		}
		valid++
	}
	if valid < threshold {
		return "authority_threshold_insufficient"
	}
	return ""
}

func VerifyOracleManifestAuthorityUpdate(state OracleTrustState, update OracleManifestAuthorityUpdate, context OracleManifestAuthorityContext) OracleAuthorityDecision {
	if context.NowWallClockMS+context.MaximumClockRollbackMS < state.LastWallClockMS || context.MonotonicElapsedMS < 0 {
		return oracleAuthorityDeny("authority_clock_rollback")
	}
	if context.ExpectedReplicaGeneration != state.ReplicaGeneration {
		return oracleAuthorityDeny("authority_replica_conflict")
	}
	manifestRaw, err := json.Marshal(update.Manifest)
	if err != nil || len(manifestRaw) > 1048576 {
		return oracleAuthorityDeny("authority_resource_limit")
	}
	if code := verifyOracleAuthorityThreshold(update.Manifest, update.ManifestSignatures, "manifest", 0, state.Keys, state.Thresholds.Manifest, OracleManifestAuthorityDomain); code != "" {
		return oracleAuthorityDeny(code)
	}
	if update.Manifest.ExpiresAtMS < context.NowWallClockMS {
		return oracleAuthorityDeny("authority_expired")
	}
	if update.Manifest.ParentDigest != state.ManifestDigest {
		return oracleAuthorityDeny("authority_parent_mismatch")
	}
	if update.Manifest.PolicyVersion > state.PolicyVersion {
		if update.Manifest.RollbackDigest != state.ManifestDigest {
			return oracleAuthorityDeny("authority_parent_mismatch")
		}
	} else {
		target, exists := state.RollbackTargets[update.Manifest.RollbackDigest]
		if update.Manifest.PolicyVersion == state.PolicyVersion || update.Manifest.PolicyVersion < state.RollbackFloor || !exists || target.Revoked || target.PolicyVersion != update.Manifest.PolicyVersion {
			return oracleAuthorityDeny("authority_policy_rollback")
		}
	}
	for _, digest := range update.Manifest.InvalidatingDependencyDigests {
		if oracleContains(context.InvalidatedDependencyDigests, digest) {
			return oracleAuthorityDeny("authority_dependency_invalidated")
		}
	}
	manifestDigest, err := OracleAuthorityObjectDigest(OracleManifestAuthorityDomain, update.Manifest)
	if err != nil {
		return oracleAuthorityDeny("authority_signature_invalid")
	}
	if code := verifyOracleAuthorityThreshold(update.Checkpoint, update.CheckpointSignatures, "checkpoint", 0, state.Keys, state.Thresholds.Checkpoint, OracleCheckpointAuthorityDomain); code != "" {
		return oracleAuthorityDeny(code)
	}
	if update.Checkpoint.Version <= state.CheckpointVersion || update.Checkpoint.PreviousCheckpointDigest != state.CheckpointDigest {
		return oracleAuthorityDeny("authority_checkpoint_stale")
	}
	if context.NowWallClockMS-update.Checkpoint.IssuedAtMS > context.MaximumCheckpointAgeMS || update.Checkpoint.ExpiresAtMS < context.NowWallClockMS {
		return oracleAuthorityDeny("authority_freeze")
	}
	if update.Checkpoint.ManifestDigest != manifestDigest {
		return oracleAuthorityDeny("authority_mix_and_match")
	}
	if update.Checkpoint.WitnessCheckpointDigest != update.Manifest.WitnessCheckpointDigest {
		return oracleAuthorityDeny("authority_witness_mismatch")
	}
	checkpointDigest, err := OracleAuthorityObjectDigest(OracleCheckpointAuthorityDomain, update.Checkpoint)
	if err != nil {
		return oracleAuthorityDeny("authority_signature_invalid")
	}
	if witnessed := context.WitnessedCheckpoints[update.Checkpoint.Version]; witnessed != "" && witnessed != checkpointDigest {
		return oracleAuthorityDeny("authority_split_view")
	}
	next := state
	next.PolicyVersion = update.Manifest.PolicyVersion
	next.ManifestDigest = manifestDigest
	next.ManifestPayloadDigest = update.Manifest.ManifestPayloadDigest
	next.CheckpointVersion = update.Checkpoint.Version
	next.CheckpointDigest = checkpointDigest
	next.ReplicaGeneration++
	next.LastWallClockMS = context.NowWallClockMS
	next.Keys = make(map[string]OracleTrustKey, len(state.Keys))
	for id, key := range state.Keys {
		next.Keys[id] = key
	}
	next.RollbackTargets = make(map[string]OracleRollbackTarget, len(state.RollbackTargets)+1)
	for digest, target := range state.RollbackTargets {
		next.RollbackTargets[digest] = target
	}
	next.RollbackTargets[state.ManifestDigest] = OracleRollbackTarget{PolicyVersion: state.PolicyVersion}
	digest, err := OracleTrustStateDigest(next)
	if err != nil {
		return oracleAuthorityDeny("authority_state_invalid")
	}
	return OracleAuthorityDecision{Allowed: true, Code: "authority_allow", NextState: &next, NextStateDigest: digest}
}

func oracleRotationKeys(rotation OracleRootRotation) (map[string]OracleTrustKey, bool) {
	if len(rotation.NewKeys) == 0 || len(rotation.NewKeys) > 64 {
		return nil, false
	}
	keys := make(map[string]OracleTrustKey, len(rotation.NewKeys))
	for _, candidate := range rotation.NewKeys {
		if _, exists := keys[candidate.KeyID]; exists || candidate.Role != "root" || candidate.Epoch != rotation.NewEpoch {
			return nil, false
		}
		der, err := base64.RawURLEncoding.DecodeString(candidate.PublicKeySPKIBase64URL)
		if err != nil {
			return nil, false
		}
		parsed, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return nil, false
		}
		publicKey, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return nil, false
		}
		keys[candidate.KeyID] = OracleTrustKey{KeyID: candidate.KeyID, Role: "root", Epoch: candidate.Epoch, PublicKey: publicKey}
	}
	return keys, true
}

func VerifyOracleRootRotation(state OracleTrustState, rotation OracleRootRotation, oldSignatures, newSignatures []OracleAuthoritySignature) OracleAuthorityDecision {
	if rotation.OldEpoch != state.RootEpoch || rotation.NewEpoch != state.RootEpoch+1 || rotation.NewRootThreshold < 1 {
		return oracleAuthorityDeny("authority_rotation_threshold")
	}
	newKeys, ok := oracleRotationKeys(rotation)
	if !ok || rotation.NewRootThreshold > len(newKeys) {
		return oracleAuthorityDeny("authority_rotation_threshold")
	}
	if code := verifyOracleAuthorityThreshold(rotation, oldSignatures, "root", state.RootEpoch, state.Keys, state.Thresholds.Root, OracleRootRotationAuthorityDomain); code != "" {
		if code == "authority_threshold_insufficient" {
			code = "authority_rotation_threshold"
		}
		return oracleAuthorityDeny(code)
	}
	if code := verifyOracleAuthorityThreshold(rotation, newSignatures, "root", rotation.NewEpoch, newKeys, rotation.NewRootThreshold, OracleRootRotationAuthorityDomain); code != "" {
		if code == "authority_threshold_insufficient" {
			code = "authority_rotation_threshold"
		}
		return oracleAuthorityDeny(code)
	}
	next := state
	next.RootEpoch = rotation.NewEpoch
	next.ReplicaGeneration++
	next.Thresholds.Root = rotation.NewRootThreshold
	next.Keys = make(map[string]OracleTrustKey)
	for id, key := range state.Keys {
		if key.Role != "root" {
			next.Keys[id] = key
		}
	}
	for id, key := range newKeys {
		next.Keys[id] = key
	}
	digest, err := OracleTrustStateDigest(next)
	if err != nil {
		return oracleAuthorityDeny("authority_state_invalid")
	}
	return OracleAuthorityDecision{Allowed: true, Code: "authority_allow", NextState: &next, NextStateDigest: digest}
}

func VerifyOracleEmergencyRevocation(state OracleTrustState, revocation OracleAuthorityRevocation, signatures []OracleAuthoritySignature, nowWallClockMS int64) OracleAuthorityDecision {
	if code := verifyOracleAuthorityThreshold(revocation, signatures, "revocation", revocation.KeyEpoch, state.Keys, state.Thresholds.Revocation, OracleRevocationAuthorityDomain); code != "" {
		return oracleAuthorityDeny(code)
	}
	if revocation.Version <= state.RevocationVersion || revocation.ExpiresAtMS < nowWallClockMS {
		return oracleAuthorityDeny("authority_revocation_stale")
	}
	if len(revocation.RevokedKeyIDs) == 0 || len(revocation.RevokedKeyIDs) > 64 {
		return oracleAuthorityDeny("authority_revocation_invalid")
	}
	seen := make(map[string]struct{}, len(revocation.RevokedKeyIDs))
	next := state
	next.Keys = make(map[string]OracleTrustKey, len(state.Keys))
	for id, key := range state.Keys {
		next.Keys[id] = key
	}
	for _, id := range revocation.RevokedKeyIDs {
		if _, duplicate := seen[id]; duplicate {
			return oracleAuthorityDeny("authority_revocation_invalid")
		}
		seen[id] = struct{}{}
		key, exists := next.Keys[id]
		if !exists {
			return oracleAuthorityDeny("authority_revocation_invalid")
		}
		key.Revoked = true
		next.Keys[id] = key
	}
	next.RevocationVersion = revocation.Version
	next.ReplicaGeneration++
	digest, err := OracleTrustStateDigest(next)
	if err != nil {
		return oracleAuthorityDeny("authority_state_invalid")
	}
	return OracleAuthorityDecision{Allowed: true, Code: "authority_allow", NextState: &next, NextStateDigest: digest}
}
