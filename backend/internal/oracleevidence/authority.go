package oracleevidence

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"sort"
)

var (
	manifestDomain     = []byte("oracle-manifest-v1\x00")
	checkpointDomain   = []byte("oracle-checkpoint-v1\x00")
	rootRotationDomain = []byte("oracle-root-rotation-v1\x00")
	revocationDomain   = []byte("oracle-revocation-v1\x00")
)

type authorityKey struct {
	id       string
	role     string
	epoch    int64
	revoked  bool
	public   ed25519.PublicKey
	spkiText string
}

func verifyManifestAuthorityUpdateImpl(input AuthorityInput) Decision {
	state, stateOK := strictObject(input.State)
	update, updateOK := strictObject(input.Candidate)
	context, contextOK := strictObject(input.Context)
	if !stateOK || !updateOK || !contextOK || !trustStateShape(state) || !manifestUpdateShape(update) || !manifestContextShape(context) {
		return Decision{Code: "authority_signature_invalid"}
	}
	now, _ := int64Value(context["nowWallClockMs"])
	maximumRollback, _ := int64Value(context["maximumClockRollbackMs"])
	lastWallClock, _ := int64Value(state["lastWallClockMs"])
	monotonic, _ := int64Value(context["monotonicElapsedMs"])
	if now+maximumRollback < lastWallClock || monotonic < 0 {
		return Decision{Code: "authority_clock_rollback"}
	}
	expectedReplica, _ := int64Value(context["expectedReplicaGeneration"])
	replica, _ := int64Value(state["replicaGeneration"])
	if expectedReplica != replica {
		return Decision{Code: "authority_replica_conflict"}
	}
	keys, keyErr := authorityKeys(state)
	if keyErr != nil {
		return decisionFromError(keyErr, "authority_signature_invalid")
	}
	manifest, _ := objectValue(update["manifest"])
	manifestBytes, canonicalErr := canonicalizeValueImpl(manifest)
	if canonicalErr != nil {
		return Decision{Code: "authority_signature_invalid"}
	}
	if len(manifestBytes) > maxJSONBytes {
		return Decision{Code: "authority_resource_limit"}
	}
	thresholds, _ := objectValue(state["thresholds"])
	manifestThreshold, _ := int64Value(thresholds["manifest"])
	manifestSignatures, _ := arrayValue(update["manifestSignatures"])
	if code := verifyAuthorityThreshold(manifest, manifestSignatures, "manifest", nil, keys, manifestThreshold, manifestDomain); code != "" {
		return Decision{Code: code}
	}
	manifestExpiry, _ := int64Value(manifest["expiresAtMs"])
	if manifestExpiry < now {
		return Decision{Code: "authority_expired"}
	}
	parentDigest, _ := stringValue(manifest["parentDigest"])
	stateManifestDigest, _ := stringValue(state["manifestDigest"])
	if parentDigest != stateManifestDigest {
		return Decision{Code: "authority_parent_mismatch"}
	}
	policyVersion, _ := int64Value(manifest["policyVersion"])
	currentPolicy, _ := int64Value(state["policyVersion"])
	rollbackDigest, _ := stringValue(manifest["rollbackDigest"])
	if policyVersion > currentPolicy {
		if rollbackDigest != stateManifestDigest {
			return Decision{Code: "authority_parent_mismatch"}
		}
	} else {
		rollbackFloor, _ := int64Value(state["rollbackFloor"])
		targets, _ := objectValue(state["rollbackTargets"])
		target, targetOK := objectValue(targets[rollbackDigest])
		targetPolicy, _ := int64Value(target["policyVersion"])
		targetRevoked, _ := boolValue(target["revoked"])
		if policyVersion == currentPolicy || policyVersion < rollbackFloor || !targetOK || targetRevoked || targetPolicy != policyVersion {
			return Decision{Code: "authority_policy_rollback"}
		}
	}
	invalidating, _ := stringArray(manifest["invalidatingDependencyDigests"], 4_096)
	invalidated, _ := stringArray(context["invalidatedDependencyDigests"], 4_096)
	for _, digest := range invalidating {
		if containsString(invalidated, digest) {
			return Decision{Code: "authority_dependency_invalidated"}
		}
	}
	manifestDigest, digestErr := authorityObjectDigest(manifestDomain, manifest)
	if digestErr != nil {
		return Decision{Code: "authority_signature_invalid"}
	}
	checkpoint, _ := objectValue(update["checkpoint"])
	checkpointThreshold, _ := int64Value(thresholds["checkpoint"])
	checkpointSignatures, _ := arrayValue(update["checkpointSignatures"])
	if code := verifyAuthorityThreshold(checkpoint, checkpointSignatures, "checkpoint", nil, keys, checkpointThreshold, checkpointDomain); code != "" {
		return Decision{Code: code}
	}
	checkpointVersion, _ := int64Value(checkpoint["version"])
	currentCheckpointVersion, _ := int64Value(state["checkpointVersion"])
	previousCheckpoint, _ := stringValue(checkpoint["previousCheckpointDigest"])
	stateCheckpoint, _ := stringValue(state["checkpointDigest"])
	if checkpointVersion <= currentCheckpointVersion || previousCheckpoint != stateCheckpoint {
		return Decision{Code: "authority_checkpoint_stale"}
	}
	issuedAt, _ := int64Value(checkpoint["issuedAtMs"])
	checkpointExpiry, _ := int64Value(checkpoint["expiresAtMs"])
	maximumAge, _ := int64Value(context["maximumCheckpointAgeMs"])
	if now-issuedAt > maximumAge || checkpointExpiry < now {
		return Decision{Code: "authority_freeze"}
	}
	checkpointManifest, _ := stringValue(checkpoint["manifestDigest"])
	if checkpointManifest != manifestDigest {
		return Decision{Code: "authority_mix_and_match"}
	}
	checkpointWitness, _ := stringValue(checkpoint["witnessCheckpointDigest"])
	manifestWitness, _ := stringValue(manifest["witnessCheckpointDigest"])
	if checkpointWitness != manifestWitness {
		return Decision{Code: "authority_witness_mismatch"}
	}
	nextCheckpointDigest, digestErr := authorityObjectDigest(checkpointDomain, checkpoint)
	if digestErr != nil {
		return Decision{Code: "authority_signature_invalid"}
	}
	witnessed, _ := objectValue(context["witnessedCheckpoints"])
	if observed, present := stringValue(witnessed[decimalKey(checkpointVersion)]); present && observed != nextCheckpointDigest {
		return Decision{Code: "authority_split_view"}
	}
	nextState := cloneObject(state)
	nextState["policyVersion"] = manifest["policyVersion"]
	nextState["manifestDigest"] = manifestDigest
	nextState["manifestPayloadDigest"] = manifest["manifestPayloadDigest"]
	nextState["checkpointVersion"] = checkpoint["version"]
	nextState["checkpointDigest"] = nextCheckpointDigest
	nextState["replicaGeneration"] = replica + 1
	nextState["lastWallClockMs"] = context["nowWallClockMs"]
	rollbackTargets := cloneObject(mustObject(nextState["rollbackTargets"]))
	rollbackTargets[stateManifestDigest] = map[string]any{"policyVersion": state["policyVersion"], "revoked": false}
	nextState["rollbackTargets"] = rollbackTargets
	return authorityAllowed(nextState)
}

func verifyRootRotationImpl(input AuthorityInput) Decision {
	state, stateOK := strictObject(input.State)
	candidate, candidateOK := strictObject(input.Candidate)
	if !stateOK || !candidateOK || !trustStateShape(state) || !exactKeys(candidate, "rotation", "oldSignatures", "newSignatures") {
		return Decision{Code: "authority_rotation_threshold"}
	}
	rotation, rotationOK := objectValue(candidate["rotation"])
	oldSignatures, oldOK := arrayValue(candidate["oldSignatures"])
	newSignatures, newOK := arrayValue(candidate["newSignatures"])
	if !rotationOK || !oldOK || !newOK || !rootRotationShape(rotation) {
		return Decision{Code: "authority_rotation_threshold"}
	}
	rootEpoch, _ := int64Value(state["rootEpoch"])
	oldEpoch, _ := int64Value(rotation["oldEpoch"])
	newEpoch, _ := int64Value(rotation["newEpoch"])
	newThreshold, _ := int64Value(rotation["newRootThreshold"])
	if oldEpoch != rootEpoch || newEpoch != rootEpoch+1 || newThreshold < 1 {
		return Decision{Code: "authority_rotation_threshold"}
	}
	stateKeys, keyErr := authorityKeys(state)
	if keyErr != nil {
		return Decision{Code: "authority_rotation_threshold"}
	}
	newKeys, newKeysWire, keyErr := rotationAuthorityKeys(rotation)
	if keyErr != nil || newThreshold > int64(len(newKeys)) {
		return Decision{Code: "authority_rotation_threshold"}
	}
	thresholds := mustObject(state["thresholds"])
	oldThreshold, _ := int64Value(thresholds["root"])
	if code := verifyAuthorityThreshold(rotation, oldSignatures, "root", &rootEpoch, stateKeys, oldThreshold, rootRotationDomain); code != "" {
		if code == "authority_threshold_insufficient" {
			code = "authority_rotation_threshold"
		}
		return Decision{Code: code}
	}
	if code := verifyAuthorityThreshold(rotation, newSignatures, "root", &newEpoch, newKeys, newThreshold, rootRotationDomain); code != "" {
		if code == "authority_threshold_insufficient" {
			code = "authority_rotation_threshold"
		}
		return Decision{Code: code}
	}
	nextState := cloneObject(state)
	nextState["rootEpoch"] = rotation["newEpoch"]
	retained := make(map[string]any)
	for keyID, rawKey := range mustObject(state["keys"]) {
		key := mustObject(rawKey)
		role, _ := stringValue(key["role"])
		if role != "root" {
			retained[keyID] = key
		}
	}
	for keyID, key := range newKeysWire {
		retained[keyID] = key
	}
	nextState["keys"] = retained
	nextThresholds := cloneObject(thresholds)
	nextThresholds["root"] = rotation["newRootThreshold"]
	nextState["thresholds"] = nextThresholds
	replica, _ := int64Value(state["replicaGeneration"])
	nextState["replicaGeneration"] = replica + 1
	return authorityAllowed(nextState)
}

func verifyEmergencyRevocationImpl(input AuthorityInput) Decision {
	state, stateOK := strictObject(input.State)
	candidate, candidateOK := strictObject(input.Candidate)
	if !stateOK || !candidateOK || !trustStateShape(state) || !exactKeys(candidate, "revocation", "signatures", "nowWallClockMs") {
		return Decision{Code: "authority_revocation_invalid"}
	}
	revocation, revocationOK := objectValue(candidate["revocation"])
	signatures, signaturesOK := arrayValue(candidate["signatures"])
	now, nowOK := int64Value(candidate["nowWallClockMs"])
	if !revocationOK || !signaturesOK || !nowOK || !emergencyRevocationShape(revocation) {
		return Decision{Code: "authority_revocation_invalid"}
	}
	keys, keyErr := authorityKeys(state)
	if keyErr != nil {
		return Decision{Code: "authority_signature_invalid"}
	}
	thresholds := mustObject(state["thresholds"])
	threshold, _ := int64Value(thresholds["revocation"])
	epoch, _ := int64Value(revocation["keyEpoch"])
	if code := verifyAuthorityThreshold(revocation, signatures, "revocation", &epoch, keys, threshold, revocationDomain); code != "" {
		return Decision{Code: code}
	}
	version, _ := int64Value(revocation["version"])
	currentVersion, _ := int64Value(state["revocationVersion"])
	expires, _ := int64Value(revocation["expiresAtMs"])
	if version <= currentVersion || expires < now {
		return Decision{Code: "authority_revocation_stale"}
	}
	revokedIDs, ok := stringArray(revocation["revokedKeyIds"], 64)
	if !ok || len(revokedIDs) == 0 {
		return Decision{Code: "authority_revocation_invalid"}
	}
	nextKeys := cloneObject(mustObject(state["keys"]))
	for _, keyID := range revokedIDs {
		key, exists := objectValue(nextKeys[keyID])
		if !exists {
			return Decision{Code: "authority_revocation_invalid"}
		}
		nextKey := cloneObject(key)
		nextKey["revoked"] = true
		nextKeys[keyID] = nextKey
	}
	nextState := cloneObject(state)
	nextState["revocationVersion"] = revocation["version"]
	nextState["keys"] = nextKeys
	replica, _ := int64Value(state["replicaGeneration"])
	nextState["replicaGeneration"] = replica + 1
	return authorityAllowed(nextState)
}

func trustStateDigestImpl(stateBytes []byte) (string, error) {
	state, ok := strictObject(stateBytes)
	if !ok || !trustStateShape(state) {
		return "", contractErr("authority_signature_invalid")
	}
	return trustStateDigestValue(state)
}

func trustStateDigestValue(state map[string]any) (string, error) {
	keys, err := authorityKeys(state)
	if err != nil {
		return "", err
	}
	keyIDs := make([]string, 0, len(keys))
	for keyID := range keys {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Strings(keyIDs)
	metadata := make([]any, 0, len(keyIDs))
	for _, keyID := range keyIDs {
		key := keys[keyID]
		metadata = append(metadata, map[string]any{
			"keyId": key.id, "role": key.role, "epoch": key.epoch, "revoked": key.revoked,
			"publicKeySpkiBase64url": key.spkiText,
		})
	}
	payload := map[string]any{
		"checkpointDigest": state["checkpointDigest"], "checkpointVersion": state["checkpointVersion"],
		"keyMetadata": metadata, "lastWallClockMs": state["lastWallClockMs"],
		"manifestDigest": state["manifestDigest"], "manifestPayloadDigest": state["manifestPayloadDigest"],
		"policyVersion": state["policyVersion"], "replicaGeneration": state["replicaGeneration"],
		"revocationVersion": state["revocationVersion"], "rollbackFloor": state["rollbackFloor"],
		"rootEpoch": state["rootEpoch"], "rollbackTargets": state["rollbackTargets"], "thresholds": state["thresholds"],
	}
	canonical, err := canonicalizeValueImpl(payload)
	if err != nil {
		return "", err
	}
	return sha256HexImpl(canonical), nil
}

func authorityAllowed(nextState map[string]any) Decision {
	canonical, err := canonicalizeValueImpl(nextState)
	if err != nil {
		return Decision{Code: "authority_signature_invalid"}
	}
	digest, err := trustStateDigestValue(nextState)
	if err != nil {
		return Decision{Code: "authority_signature_invalid"}
	}
	return Decision{Allowed: true, Code: "authority_allow", NextState: canonical, NextStateDigest: digest}
}

func authorityObjectDigest(domain []byte, value any) (string, error) {
	canonical, err := canonicalizeValueImpl(value)
	if err != nil {
		return "", err
	}
	bytes := append(append([]byte(nil), domain...), canonical...)
	return sha256HexImpl(bytes), nil
}

func verifyAuthorityThreshold(signed any, signatures []any, role string, requiredEpoch *int64, keys map[string]authorityKey, threshold int64, domain []byte) string {
	if len(signatures) > 64 || len(keys) > 64 {
		return "authority_resource_limit"
	}
	if threshold < 1 || threshold > 64 {
		return "authority_threshold_insufficient"
	}
	canonical, err := canonicalizeValueImpl(signed)
	if err != nil {
		return "authority_signature_invalid"
	}
	message := append(append([]byte(nil), domain...), canonical...)
	seen := make(map[string]bool)
	var observedEpoch *int64
	valid := int64(0)
	for _, rawSignature := range signatures {
		signature, ok := objectValue(rawSignature)
		if !ok || !authoritySignatureShape(signature) {
			return "authority_signature_invalid"
		}
		keyID, _ := stringValue(signature["keyId"])
		if seen[keyID] {
			return "authority_duplicate_signer"
		}
		seen[keyID] = true
		key, exists := keys[keyID]
		signatureRole, _ := stringValue(signature["role"])
		epoch, _ := int64Value(signature["keyEpoch"])
		if !exists || key.role != role || signatureRole != role || key.epoch != epoch || (requiredEpoch != nil && epoch != *requiredEpoch) || (observedEpoch != nil && epoch != *observedEpoch) {
			return "authority_wrong_role"
		}
		if observedEpoch == nil {
			copyEpoch := epoch
			observedEpoch = &copyEpoch
		}
		if key.revoked {
			return "authority_key_revoked"
		}
		algorithm, _ := stringValue(signature["algorithm"])
		signatureText, _ := stringValue(signature["signatureBase64url"])
		rawBytes, decodeErr := base64.RawURLEncoding.Strict().DecodeString(signatureText)
		if decodeErr != nil || algorithm != "Ed25519" || len(rawBytes) != ed25519.SignatureSize || !ed25519.Verify(key.public, message, rawBytes) {
			return "authority_signature_invalid"
		}
		valid++
	}
	if valid < threshold {
		return "authority_threshold_insufficient"
	}
	return ""
}

func authorityKeys(state map[string]any) (map[string]authorityKey, error) {
	rawKeys, ok := objectValue(state["keys"])
	if !ok || len(rawKeys) > 64 {
		return nil, contractErr("authority_resource_limit")
	}
	result := make(map[string]authorityKey, len(rawKeys))
	for mapKey, rawValue := range rawKeys {
		value, ok := objectValue(rawValue)
		if !ok || !exactKeys(value, "keyId", "role", "epoch", "revoked", "publicKeySpkiBase64url") {
			return nil, contractErr("authority_signature_invalid")
		}
		keyID, _ := stringValue(value["keyId"])
		role, _ := stringValue(value["role"])
		epoch, epochOK := int64Value(value["epoch"])
		revoked, revokedOK := boolValue(value["revoked"])
		spkiText, spkiOK := stringValue(value["publicKeySpkiBase64url"])
		if keyID == "" || keyID != mapKey || !containsString([]string{"root", "manifest", "checkpoint", "revocation", "sidecar_capability"}, role) || !epochOK || epoch < 0 || !revokedOK || !spkiOK {
			return nil, contractErr("authority_signature_invalid")
		}
		der, decodeErr := base64.RawURLEncoding.Strict().DecodeString(spkiText)
		if decodeErr != nil {
			return nil, contractErr("authority_signature_invalid")
		}
		parsed, parseErr := x509.ParsePKIXPublicKey(der)
		public, publicOK := parsed.(ed25519.PublicKey)
		if parseErr != nil || !publicOK {
			return nil, contractErr("authority_signature_invalid")
		}
		result[keyID] = authorityKey{id: keyID, role: role, epoch: epoch, revoked: revoked, public: public, spkiText: spkiText}
	}
	return result, nil
}

func rotationAuthorityKeys(rotation map[string]any) (map[string]authorityKey, map[string]any, error) {
	rawKeys, _ := arrayValue(rotation["newKeys"])
	if len(rawKeys) == 0 || len(rawKeys) > 64 {
		return nil, nil, contractErr("authority_rotation_threshold")
	}
	newEpoch, _ := int64Value(rotation["newEpoch"])
	keys := make(map[string]authorityKey, len(rawKeys))
	wire := make(map[string]any, len(rawKeys))
	for _, rawValue := range rawKeys {
		value, ok := objectValue(rawValue)
		if !ok || !exactKeys(value, "keyId", "role", "epoch", "publicKeySpkiBase64url") {
			return nil, nil, contractErr("authority_rotation_threshold")
		}
		keyID, _ := stringValue(value["keyId"])
		role, _ := stringValue(value["role"])
		epoch, epochOK := int64Value(value["epoch"])
		spkiText, spkiOK := stringValue(value["publicKeySpkiBase64url"])
		if keyID == "" || keys[keyID].id != "" || role != "root" || !epochOK || epoch != newEpoch || !spkiOK {
			return nil, nil, contractErr("authority_rotation_threshold")
		}
		der, decodeErr := base64.RawURLEncoding.Strict().DecodeString(spkiText)
		parsed, parseErr := x509.ParsePKIXPublicKey(der)
		public, publicOK := parsed.(ed25519.PublicKey)
		if decodeErr != nil || parseErr != nil || !publicOK {
			return nil, nil, contractErr("authority_rotation_threshold")
		}
		keys[keyID] = authorityKey{id: keyID, role: role, epoch: epoch, public: public, spkiText: spkiText}
		wire[keyID] = map[string]any{"keyId": keyID, "role": role, "epoch": epoch, "revoked": false, "publicKeySpkiBase64url": spkiText}
	}
	return keys, wire, nil
}

func trustStateShape(state map[string]any) bool {
	if !exactKeys(state, "rootEpoch", "policyVersion", "rollbackFloor", "revocationVersion", "manifestDigest", "manifestPayloadDigest", "checkpointVersion", "checkpointDigest", "replicaGeneration", "lastWallClockMs", "keys", "thresholds", "rollbackTargets") {
		return false
	}
	for _, field := range []string{"rootEpoch", "policyVersion", "rollbackFloor", "revocationVersion", "checkpointVersion", "replicaGeneration", "lastWallClockMs"} {
		if !generation(state[field]) {
			return false
		}
	}
	for _, field := range []string{"manifestDigest", "manifestPayloadDigest", "checkpointDigest"} {
		digest, ok := stringValue(state[field])
		if !ok || !isSHA256(digest) {
			return false
		}
	}
	thresholds, ok := objectValue(state["thresholds"])
	if !ok || !exactKeys(thresholds, "root", "manifest", "checkpoint", "revocation") {
		return false
	}
	for _, field := range []string{"root", "manifest", "checkpoint", "revocation"} {
		value, ok := int64Value(thresholds[field])
		if !ok || value < 0 || value > 64 {
			return false
		}
	}
	_, keysOK := objectValue(state["keys"])
	_, targetsOK := objectValue(state["rollbackTargets"])
	return keysOK && targetsOK
}

func manifestUpdateShape(update map[string]any) bool {
	if !exactKeys(update, "manifest", "manifestSignatures", "checkpoint", "checkpointSignatures") {
		return false
	}
	manifest, manifestOK := objectValue(update["manifest"])
	checkpoint, checkpointOK := objectValue(update["checkpoint"])
	_, manifestSignaturesOK := arrayValue(update["manifestSignatures"])
	_, checkpointSignaturesOK := arrayValue(update["checkpointSignatures"])
	return manifestOK && checkpointOK && manifestSignaturesOK && checkpointSignaturesOK && manifestShape(manifest) && checkpointShape(checkpoint)
}

func manifestShape(value map[string]any) bool {
	if !exactKeys(value, "schemaId", "schemaMajor", "schemaRevision", "kind", "manifestId", "policyVersion", "parentDigest", "rollbackDigest", "contractDigest", "manifestPayloadDigest", "issuedAtMs", "expiresAtMs", "sourcePackageDigests", "promotionRefs", "witnessCheckpointDigest", "invalidatingDependencyDigests") {
		return false
	}
	if !camelSchemaKind(value, "manifest_authority") || !safeRef(value["manifestId"]) {
		return false
	}
	for _, field := range []string{"policyVersion", "issuedAtMs", "expiresAtMs"} {
		if !generation(value[field]) {
			return false
		}
	}
	for _, field := range []string{"parentDigest", "rollbackDigest", "contractDigest", "manifestPayloadDigest", "witnessCheckpointDigest"} {
		digest, ok := stringValue(value[field])
		if !ok || !isSHA256(digest) {
			return false
		}
	}
	for _, field := range []string{"sourcePackageDigests", "invalidatingDependencyDigests"} {
		values, ok := stringArray(value[field], 64)
		if !ok || !allSHA256(values) {
			return false
		}
	}
	_, ok := stringArray(value["promotionRefs"], 64)
	return ok
}

func checkpointShape(value map[string]any) bool {
	if !exactKeys(value, "schemaId", "schemaMajor", "schemaRevision", "kind", "version", "manifestDigest", "previousCheckpointDigest", "witnessCheckpointDigest", "issuedAtMs", "expiresAtMs") || !camelSchemaKind(value, "checkpoint") {
		return false
	}
	for _, field := range []string{"version", "issuedAtMs", "expiresAtMs"} {
		if !generation(value[field]) {
			return false
		}
	}
	for _, field := range []string{"manifestDigest", "previousCheckpointDigest", "witnessCheckpointDigest"} {
		digest, ok := stringValue(value[field])
		if !ok || !isSHA256(digest) {
			return false
		}
	}
	return true
}

func manifestContextShape(value map[string]any) bool {
	if !exactKeys(value, "nowWallClockMs", "monotonicElapsedMs", "maximumClockRollbackMs", "maximumCheckpointAgeMs", "expectedReplicaGeneration", "invalidatedDependencyDigests", "witnessedCheckpoints") {
		return false
	}
	for _, field := range []string{"nowWallClockMs", "maximumClockRollbackMs", "maximumCheckpointAgeMs", "expectedReplicaGeneration"} {
		if !generation(value[field]) {
			return false
		}
	}
	if _, ok := int64Value(value["monotonicElapsedMs"]); !ok {
		return false
	}
	invalidated, ok := stringArray(value["invalidatedDependencyDigests"], 4_096)
	if !ok || !allSHA256(invalidated) {
		return false
	}
	witnessed, ok := objectValue(value["witnessedCheckpoints"])
	if !ok {
		return false
	}
	for key, rawDigest := range witnessed {
		if _, ok := parsePositiveDecimal(key); !ok {
			return false
		}
		digest, ok := stringValue(rawDigest)
		if !ok || !isSHA256(digest) {
			return false
		}
	}
	return true
}

func rootRotationShape(value map[string]any) bool {
	if !exactKeys(value, "schemaId", "schemaMajor", "schemaRevision", "kind", "oldEpoch", "newEpoch", "newRootThreshold", "newKeys") || !camelSchemaKind(value, "root_rotation") {
		return false
	}
	if !generation(value["oldEpoch"]) || !generation(value["newEpoch"]) || !generation(value["newRootThreshold"]) {
		return false
	}
	keys, ok := arrayValue(value["newKeys"])
	return ok && len(keys) > 0 && len(keys) <= 64
}

func emergencyRevocationShape(value map[string]any) bool {
	if !exactKeys(value, "schemaId", "schemaMajor", "schemaRevision", "kind", "version", "keyEpoch", "issuedAtMs", "expiresAtMs", "revokedKeyIds", "reasonRef") || !camelSchemaKind(value, "emergency_revocation") {
		return false
	}
	for _, field := range []string{"version", "keyEpoch", "issuedAtMs", "expiresAtMs"} {
		if !generation(value[field]) {
			return false
		}
	}
	ids, ok := stringArray(value["revokedKeyIds"], 64)
	return ok && len(ids) > 0 && safeRef(value["reasonRef"])
}

func authoritySignatureShape(value map[string]any) bool {
	if !exactKeys(value, "algorithm", "keyId", "keyEpoch", "role", "signatureBase64url") {
		return false
	}
	algorithm, _ := stringValue(value["algorithm"])
	role, _ := stringValue(value["role"])
	signature, signatureOK := stringValue(value["signatureBase64url"])
	return algorithm == "Ed25519" && safeRef(value["keyId"]) && generation(value["keyEpoch"]) && containsString([]string{"root", "manifest", "checkpoint", "revocation", "sidecar_capability"}, role) && signatureOK && signature != ""
}

func camelSchemaKind(value map[string]any, kind string) bool {
	schemaID, _ := stringValue(value["schemaId"])
	major, majorOK := int64Value(value["schemaMajor"])
	revision, revisionOK := int64Value(value["schemaRevision"])
	actualKind, _ := stringValue(value["kind"])
	return schemaID == "oracle.compatibility" && majorOK && revisionOK && major == 1 && revision == 0 && actualKind == kind
}

func cloneObject(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func mustObject(value any) map[string]any {
	result, _ := objectValue(value)
	return result
}

func decimalKey(value int64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = byte(value%10) + '0'
		value /= 10
	}
	return string(buffer[position:])
}

func parsePositiveDecimal(value string) (int64, bool) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return 0, false
	}
	var parsed int64
	for _, current := range []byte(value) {
		if current < '0' || current > '9' || parsed > (maxSafeInteger-int64(current-'0'))/10 {
			return 0, false
		}
		parsed = parsed*10 + int64(current-'0')
	}
	return parsed, true
}
