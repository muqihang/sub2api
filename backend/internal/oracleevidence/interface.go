package oracleevidence

var safeRefAlphabet = func() map[byte]bool {
	result := make(map[byte]bool)
	for current := byte('a'); current <= 'z'; current++ {
		result[current] = true
	}
	for current := byte('A'); current <= 'Z'; current++ {
		result[current] = true
	}
	for current := byte('0'); current <= '9'; current++ {
		result[current] = true
	}
	for _, current := range []byte("._:/-") {
		result[current] = true
	}
	return result
}()

func decideReadinessImpl(handshakeBytes, expectedBytes []byte) Decision {
	handshake, ok := strictObject(handshakeBytes)
	if !ok || !exactSchemaKind(handshake, "readiness_handshake") || !readinessShape(handshake) {
		return Decision{Code: "interface_schema_unsupported"}
	}
	expected, ok := strictObject(expectedBytes)
	if !ok || !readinessExpectedShape(expected) {
		return Decision{Code: "interface_schema_unsupported"}
	}
	major, _ := int64Value(expected["schema_major"])
	revision, _ := int64Value(expected["schema_revision"])
	ranges, _ := arrayValue(handshake["supported_contracts"])
	supported := false
	for _, rawRange := range ranges {
		current, _ := objectValue(rawRange)
		currentMajor, _ := int64Value(current["schema_major"])
		minimum, _ := int64Value(current["minimum_revision"])
		maximum, _ := int64Value(current["maximum_revision"])
		if currentMajor == major && minimum <= revision && maximum >= revision {
			supported = true
			break
		}
	}
	if !supported {
		return Decision{Code: "interface_schema_unsupported"}
	}
	contract, _ := stringValue(handshake["contract_digest"])
	expectedContract, _ := stringValue(expected["contract_digest"])
	if contract != expectedContract {
		return Decision{Code: "interface_contract_mismatch"}
	}
	for _, field := range []string{"build_digest", "manifest_digest", "profile_generation", "sidecar_generation", "replay_ledger_generation"} {
		if !scalarEqual(handshake[field], expected[field]) {
			return Decision{Code: "interface_generation_mismatch", Detail: field}
		}
	}
	liveness, _ := boolValue(handshake["liveness"])
	readiness, _ := boolValue(handshake["readiness"])
	protected, _ := boolValue(handshake["protected_capability"])
	expires, _ := int64Value(handshake["expires_at_ms"])
	now, _ := int64Value(expected["now_ms"])
	disabled, _ := stringArray(handshake["disabled_capabilities"], 128)
	required, _ := stringValue(expected["required_capability"])
	if !liveness || !readiness || !protected || expires < now || containsString(disabled, required) {
		return Decision{Code: "interface_not_ready"}
	}
	return Decision{Allowed: true, Code: "interface_allow"}
}

func decideLifecycleImpl(stateBytes, operationBytes []byte) Decision {
	state, stateOK := strictObject(stateBytes)
	operation, operationOK := strictObject(operationBytes)
	if !stateOK || !operationOK || !lifecycleStateShape(state) || !exactSchemaKind(operation, "lifecycle_operation") || !lifecycleOperationShape(operation) {
		return Decision{Code: "interface_schema_unsupported"}
	}
	stateOwner, _ := stringValue(state["owner"])
	operationOwner, _ := stringValue(operation["owner"])
	stateAccount, _ := stringValue(state["account_ref"])
	operationAccount, _ := stringValue(operation["account_ref"])
	if stateOwner != "sub2api" || operationOwner != "sub2api" || stateAccount != operationAccount {
		return Decision{Code: "interface_owner_mismatch"}
	}
	stateVersion, _ := int64Value(state["state_version"])
	expectedVersion, _ := int64Value(operation["expected_state_version"])
	nextVersion, _ := int64Value(operation["next_state_version"])
	if expectedVersion != stateVersion || nextVersion != stateVersion+1 {
		return Decision{Code: "interface_stale_state"}
	}
	for _, field := range []string{"account_generation", "credential_generation", "proxy_generation", "profile_generation"} {
		current, _ := int64Value(state[field])
		candidate, _ := int64Value(operation[field])
		if candidate < current {
			return Decision{Code: "interface_generation_regression", Detail: field}
		}
	}
	operationName, _ := stringValue(operation["operation"])
	status, _ := stringValue(state["status"])
	if (operationName == "register" && status != "absent") || (operationName == "replace" && status != "active") {
		return Decision{Code: "interface_state_transition_invalid"}
	}
	statusByOperation := map[string]string{
		"register": "active", "replace": "active", "freeze": "frozen", "drain": "draining",
		"revoke": "revoked", "delete": "deleted", "query": status, "reconcile": status,
	}
	nextState := map[string]any{
		"owner": "sub2api", "account_ref": stateAccount,
		"account_generation": operation["account_generation"], "credential_generation": operation["credential_generation"],
		"proxy_generation": operation["proxy_generation"], "profile_generation": operation["profile_generation"],
		"state_version": operation["next_state_version"], "status": statusByOperation[operationName],
	}
	return allowedStateDecision("interface_allow", nextState, false)
}

func decideTaskLineageImpl(stateBytes, candidateBytes []byte, nowMS int64) Decision {
	state, stateOK := strictObject(stateBytes)
	candidate, candidateOK := strictObject(candidateBytes)
	if !stateOK || !candidateOK || !lineageStateShape(state) || !exactSchemaKind(candidate, "task_lineage") || !lineageCandidateShape(candidate) {
		return Decision{Code: "interface_schema_unsupported"}
	}
	root, _ := stringValue(state["root_task_ref"])
	parent, _ := stringValue(state["current_task_ref"])
	candidateRoot, _ := stringValue(candidate["root_task_ref"])
	candidateParent, _ := stringValue(candidate["parent_task_ref"])
	current, _ := stringValue(candidate["current_task_ref"])
	if candidateRoot != root || candidateParent != parent || current == parent {
		return Decision{Code: "interface_lineage_mismatch"}
	}
	sequence, _ := int64Value(state["migration_sequence"])
	candidateSequence, _ := int64Value(candidate["migration_sequence"])
	clientGeneration, _ := int64Value(state["client_generation"])
	candidateClientGeneration, _ := int64Value(candidate["client_generation"])
	profileGeneration, _ := int64Value(state["profile_generation"])
	candidateProfileGeneration, _ := int64Value(candidate["profile_generation"])
	if candidateSequence != sequence+1 || candidateClientGeneration < clientGeneration || candidateProfileGeneration < profileGeneration {
		return Decision{Code: "interface_migration_stale"}
	}
	deadline, _ := int64Value(candidate["deadline_ms"])
	if deadline < nowMS {
		return Decision{Code: "interface_deadline_expired"}
	}
	nextState := map[string]any{
		"root_task_ref": root, "current_task_ref": current,
		"client_generation": candidate["client_generation"], "profile_generation": candidate["profile_generation"],
		"migration_sequence": candidate["migration_sequence"],
	}
	return allowedStateDecision("interface_allow", nextState, false)
}

func decideOutcomeImpl(outcomeBytes []byte) Decision {
	outcome, ok := strictObject(outcomeBytes)
	if !ok || !exactSchemaKind(outcome, "outcome_envelope") || !outcomeShape(outcome) {
		return Decision{Code: "interface_schema_unsupported"}
	}
	partial, _ := boolValue(outcome["partial_output"])
	toolSideEffect, _ := boolValue(outcome["tool_side_effect"])
	terminal, _ := boolValue(outcome["terminal"])
	if partial || toolSideEffect || terminal {
		return Decision{Allowed: true, Code: "interface_terminal_no_retry"}
	}
	semantic, _ := stringValue(outcome["semantic_outcome"])
	retryOwner, _ := stringValue(outcome["retry_owner"])
	if semantic == "rate_limited" && retryOwner == "sub2api" {
		return Decision{Allowed: true, Code: "interface_sub2api_retry"}
	}
	if retryOwner == "cc_gateway" {
		return Decision{Allowed: true, Code: "interface_gateway_retry"}
	}
	return Decision{Allowed: true, Code: "interface_terminal_no_retry"}
}

func executeReplayImpl(stateBytes, commandBytes []byte) Decision {
	state, stateOK := strictObject(stateBytes)
	command, commandOK := strictObject(commandBytes)
	if !stateOK || !commandOK || !replayStateShape(state) || !replayCommandShape(command) {
		return Decision{Code: "replay_rejected"}
	}
	generation, _ := int64Value(state["ledger_generation"])
	expectedGeneration, _ := int64Value(command["expected_generation"])
	if generation != expectedGeneration {
		return Decision{Code: "replay_replica_conflict"}
	}
	identityValue := map[string]any{
		"attempt_id": command["attempt_id"], "capability_id": command["capability_id"],
		"key_epoch": command["key_epoch"], "nonce": command["nonce"],
	}
	identityBytes, err := encodeDeterministicCBOR(identityValue)
	if err != nil {
		return Decision{Code: "replay_rejected"}
	}
	identity := sha256HexImpl(identityBytes)
	entries, _ := objectValue(state["entries"])
	current, currentExists := objectValue(entries[identity])
	operation, _ := stringValue(command["operation"])
	now, _ := int64Value(command["now_ms"])
	expires, _ := int64Value(command["expires_at_ms"])
	var nextEntry map[string]any
	switch operation {
	case "reserve":
		if currentExists || expires <= now {
			return Decision{Code: "replay_rejected"}
		}
		nextEntry = map[string]any{"state": "reserved", "expires_at_ms": command["expires_at_ms"]}
	case "commit":
		stateName, _ := stringValue(current["state"])
		currentExpiry, _ := int64Value(current["expires_at_ms"])
		if !currentExists || stateName != "reserved" || currentExpiry <= now {
			return Decision{Code: "replay_rejected"}
		}
		nextEntry = map[string]any{"state": "committed", "expires_at_ms": current["expires_at_ms"]}
	case "expire":
		stateName, _ := stringValue(current["state"])
		currentExpiry, _ := int64Value(current["expires_at_ms"])
		if !currentExists || stateName != "reserved" || currentExpiry > now {
			return Decision{Code: "replay_rejected"}
		}
		nextEntry = map[string]any{"state": "expired", "expires_at_ms": current["expires_at_ms"]}
	case "revoke":
		stateName, _ := stringValue(current["state"])
		if !currentExists || stateName != "reserved" {
			return Decision{Code: "replay_rejected"}
		}
		nextEntry = map[string]any{"state": "revoked", "expires_at_ms": current["expires_at_ms"]}
	default:
		return Decision{Code: "replay_rejected"}
	}
	nextEntries := make(map[string]any, len(entries)+1)
	for key, value := range entries {
		nextEntries[key] = value
	}
	nextEntries[identity] = nextEntry
	nextState := map[string]any{"ledger_generation": generation + 1, "entries": nextEntries}
	codeByOperation := map[string]string{"reserve": "replay_reserved", "commit": "replay_committed", "expire": "replay_expired", "revoke": "replay_revoked"}
	return allowedStateDecision(codeByOperation[operation], nextState, true)
}

func strictObject(input []byte) (map[string]any, bool) {
	value, err := parseStrictJSONImpl(input)
	if err != nil {
		return nil, false
	}
	result, ok := objectValue(value)
	return result, ok
}

func exactSchemaKind(value map[string]any, kind string) bool {
	schemaID, _ := stringValue(value["schema_id"])
	major, majorOK := int64Value(value["schema_major"])
	revision, revisionOK := int64Value(value["schema_revision"])
	actualKind, _ := stringValue(value["kind"])
	return schemaID == "oracle.compatibility" && majorOK && revisionOK && major == 1 && revision == 0 && actualKind == kind
}

func safeRef(value any) bool {
	text, ok := stringValue(value)
	if !ok || len(text) < 1 || len(text) > 200 {
		return false
	}
	for _, current := range []byte(text) {
		if !safeRefAlphabet[current] {
			return false
		}
	}
	return true
}

func generation(value any) bool {
	parsed, ok := int64Value(value)
	return ok && parsed >= 0
}

func readinessShape(value map[string]any) bool {
	if !exactKeys(value, "schema_id", "schema_major", "schema_revision", "kind", "liveness", "readiness", "protected_capability", "build_digest", "contract_digest", "manifest_digest", "profile_generation", "sidecar_generation", "replay_ledger_generation", "supported_contracts", "disabled_capabilities", "expires_at_ms") {
		return false
	}
	if _, ok := boolValue(value["liveness"]); !ok {
		return false
	}
	if _, ok := boolValue(value["readiness"]); !ok {
		return false
	}
	if _, ok := boolValue(value["protected_capability"]); !ok {
		return false
	}
	for _, field := range []string{"build_digest", "contract_digest", "manifest_digest"} {
		digest, ok := stringValue(value[field])
		if !ok || !isSHA256(digest) {
			return false
		}
	}
	for _, field := range []string{"profile_generation", "sidecar_generation", "replay_ledger_generation", "expires_at_ms"} {
		if !generation(value[field]) {
			return false
		}
	}
	ranges, ok := arrayValue(value["supported_contracts"])
	if !ok || len(ranges) == 0 || len(ranges) > 16 {
		return false
	}
	previousMajor, previousMaximum := int64(-1), int64(-1)
	for _, rawRange := range ranges {
		current, ok := objectValue(rawRange)
		if !ok || !exactKeys(current, "schema_major", "minimum_revision", "maximum_revision") {
			return false
		}
		major, majorOK := int64Value(current["schema_major"])
		minimum, minimumOK := int64Value(current["minimum_revision"])
		maximum, maximumOK := int64Value(current["maximum_revision"])
		if !majorOK || !minimumOK || !maximumOK || major < 0 || minimum < 0 || maximum < minimum || major < previousMajor || (major == previousMajor && minimum <= previousMaximum) {
			return false
		}
		previousMajor, previousMaximum = major, maximum
	}
	_, ok = stringArray(value["disabled_capabilities"], 128)
	return ok
}

func readinessExpectedShape(value map[string]any) bool {
	if !exactKeys(value, "now_ms", "schema_major", "schema_revision", "build_digest", "contract_digest", "manifest_digest", "profile_generation", "sidecar_generation", "replay_ledger_generation", "required_capability") {
		return false
	}
	for _, field := range []string{"now_ms", "schema_major", "schema_revision", "profile_generation", "sidecar_generation", "replay_ledger_generation"} {
		if !generation(value[field]) {
			return false
		}
	}
	for _, field := range []string{"build_digest", "contract_digest", "manifest_digest"} {
		digest, ok := stringValue(value[field])
		if !ok || !isSHA256(digest) {
			return false
		}
	}
	return safeRef(value["required_capability"])
}

func lifecycleStateShape(value map[string]any) bool {
	if !exactKeys(value, "owner", "account_ref", "account_generation", "credential_generation", "proxy_generation", "profile_generation", "state_version", "status") {
		return false
	}
	owner, _ := stringValue(value["owner"])
	status, _ := stringValue(value["status"])
	if owner != "sub2api" || !safeRef(value["account_ref"]) || !containsString([]string{"absent", "active", "frozen", "draining", "revoked", "deleted"}, status) {
		return false
	}
	for _, field := range []string{"account_generation", "credential_generation", "proxy_generation", "profile_generation", "state_version"} {
		if !generation(value[field]) {
			return false
		}
	}
	return true
}

func lifecycleOperationShape(value map[string]any) bool {
	if !exactKeys(value, "schema_id", "schema_major", "schema_revision", "kind", "operation", "owner", "account_ref", "account_generation", "credential_generation", "proxy_generation", "profile_generation", "expected_state_version", "next_state_version", "idempotency_key") {
		return false
	}
	operation, _ := stringValue(value["operation"])
	owner, _ := stringValue(value["owner"])
	if !containsString([]string{"register", "replace", "freeze", "drain", "revoke", "delete", "query", "reconcile"}, operation) || !containsString([]string{"sub2api", "cc_gateway"}, owner) || !safeRef(value["account_ref"]) || !safeRef(value["idempotency_key"]) {
		return false
	}
	for _, field := range []string{"account_generation", "credential_generation", "proxy_generation", "profile_generation", "expected_state_version", "next_state_version"} {
		if !generation(value[field]) {
			return false
		}
	}
	return true
}

func lineageStateShape(value map[string]any) bool {
	if !exactKeys(value, "root_task_ref", "current_task_ref", "client_generation", "profile_generation", "migration_sequence") || !safeRef(value["root_task_ref"]) || !safeRef(value["current_task_ref"]) {
		return false
	}
	return generation(value["client_generation"]) && generation(value["profile_generation"]) && generation(value["migration_sequence"])
}

func lineageCandidateShape(value map[string]any) bool {
	if !exactKeys(value, "schema_id", "schema_major", "schema_revision", "kind", "root_task_ref", "parent_task_ref", "current_task_ref", "client_generation", "profile_generation", "migration_sequence", "attempt_id", "deadline_ms", "idempotency_key") {
		return false
	}
	for _, field := range []string{"root_task_ref", "parent_task_ref", "current_task_ref", "attempt_id", "idempotency_key"} {
		if !safeRef(value[field]) {
			return false
		}
	}
	return generation(value["client_generation"]) && generation(value["profile_generation"]) && generation(value["migration_sequence"]) && generation(value["deadline_ms"])
}

func outcomeShape(value map[string]any) bool {
	if !exactKeys(value, "schema_id", "schema_major", "schema_revision", "kind", "attempt_id", "transport_fact", "semantic_outcome", "partial_output", "tool_side_effect", "retry_owner", "terminal", "final_headers_sha256", "final_body_sha256") || !safeRef(value["attempt_id"]) {
		return false
	}
	transport, _ := stringValue(value["transport_fact"])
	semantic, _ := stringValue(value["semantic_outcome"])
	retryOwner, _ := stringValue(value["retry_owner"])
	if !containsString([]string{"not_attempted", "connected", "reset", "timeout", "rejected"}, transport) || !containsString([]string{"none", "success", "client_error", "rate_limited", "capacity", "server_error", "malformed", "cancelled"}, semantic) || !containsString([]string{"none", "cc_gateway", "sub2api"}, retryOwner) {
		return false
	}
	for _, field := range []string{"partial_output", "tool_side_effect", "terminal"} {
		if _, ok := boolValue(value[field]); !ok {
			return false
		}
	}
	for _, field := range []string{"final_headers_sha256", "final_body_sha256"} {
		digest, ok := stringValue(value[field])
		if !ok || !isSHA256(digest) {
			return false
		}
	}
	return true
}

func replayStateShape(value map[string]any) bool {
	if !exactKeys(value, "ledger_generation", "entries") || !generation(value["ledger_generation"]) {
		return false
	}
	entries, ok := objectValue(value["entries"])
	if !ok || len(entries) > 4_096 {
		return false
	}
	for identity, rawEntry := range entries {
		entry, ok := objectValue(rawEntry)
		if !ok || !isSHA256(identity) || !exactKeys(entry, "state", "expires_at_ms") || !generation(entry["expires_at_ms"]) {
			return false
		}
		state, _ := stringValue(entry["state"])
		if !containsString([]string{"reserved", "committed", "expired", "revoked"}, state) {
			return false
		}
	}
	return true
}

func replayCommandShape(value map[string]any) bool {
	if !exactKeys(value, "operation", "expected_generation", "now_ms", "expires_at_ms", "key_epoch", "capability_id", "attempt_id", "nonce") {
		return false
	}
	operation, _ := stringValue(value["operation"])
	if !containsString([]string{"reserve", "commit", "expire", "revoke"}, operation) {
		return false
	}
	for _, field := range []string{"expected_generation", "now_ms", "expires_at_ms", "key_epoch"} {
		if !generation(value[field]) {
			return false
		}
	}
	return safeRef(value["capability_id"]) && safeRef(value["attempt_id"]) && safeRef(value["nonce"])
}

func scalarEqual(left, right any) bool {
	leftNumber, leftNumeric := int64Value(left)
	rightNumber, rightNumeric := int64Value(right)
	if leftNumeric || rightNumeric {
		return leftNumeric && rightNumeric && leftNumber == rightNumber
	}
	leftString, leftText := stringValue(left)
	rightString, rightText := stringValue(right)
	return leftText && rightText && leftString == rightString
}

func allowedStateDecision(code string, state map[string]any, cborDigest bool) Decision {
	canonical, err := canonicalizeValueImpl(state)
	if err != nil {
		return Decision{Code: "interface_schema_unsupported"}
	}
	digestBytes := canonical
	if cborDigest {
		digestBytes, err = encodeDeterministicCBOR(state)
		if err != nil {
			return Decision{Code: "replay_rejected"}
		}
	}
	return Decision{Allowed: true, Code: code, NextState: canonical, NextStateDigest: sha256HexImpl(digestBytes)}
}
