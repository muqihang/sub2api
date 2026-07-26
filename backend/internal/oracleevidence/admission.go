package oracleevidence

var authorityRanks = map[string]int{
	"unverified": 0, "package_observed": 1, "local_wire_observed": 2, "cross_checked": 3,
	"gateway_wire_equivalent": 4, "stateful_behavior_equivalent": 5,
	"upstream_canary_observed": 6, "production_verified": 7,
}

var localObservationScopes = map[string]bool{
	"package": true, "local_fixture": true, "local_wire": true, "gateway": true,
}

func admissionPayloadDigestImpl(certificate, signals, negativeCapabilities []byte) (string, error) {
	certificateValue, err := parseStrictJSONImpl(certificate)
	if err != nil {
		return "", err
	}
	signalsValue, err := parseStrictJSONImpl(signals)
	if err != nil {
		return "", err
	}
	negativeValue, err := parseStrictJSONImpl(negativeCapabilities)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalizeValueImpl(map[string]any{
		"certificate":           certificateValue,
		"negative_capabilities": negativeValue,
		"signals":               signalsValue,
	})
	if err != nil {
		return "", err
	}
	return sha256HexImpl(canonical), nil
}

func decideBehaviorAdmissionImpl(certificateBytes, contextBytes []byte) Decision {
	certificateValue, err := parseStrictJSONImpl(certificateBytes)
	if err != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	contextValue, err := parseStrictJSONImpl(contextBytes)
	if err != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	certificate, certificateOK := objectValue(certificateValue)
	context, contextOK := objectValue(contextValue)
	if !certificateOK || !contextOK || !validAdmissionContextShape(context) {
		return Decision{Code: "admission_schema_invalid"}
	}
	schemas, schemaErr := defaultSchemaSet()
	if schemaErr != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	if validateSchemaValue(schemas, mustSchemaDefinition(schemas, "behaviorCoherenceCertificate"), certificate, 0) != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	signals, signalsOK := arrayValue(context["signals"])
	negative, negativeOK := objectValue(context["negative_capabilities"])
	if !signalsOK || !negativeOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	signalsBytes, signalsErr := canonicalizeValueImpl(signals)
	negativeBytes, negativeErr := canonicalizeValueImpl(negative)
	if signalsErr != nil || negativeErr != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	digest, digestErr := admissionPayloadDigestImpl(certificateBytes, signalsBytes, negativeBytes)
	if digestErr != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	expected := context["expected"].(map[string]any)
	manifestDigest, manifestDigestOK := stringValue(expected["manifest_payload_digest"])
	if !manifestDigestOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	if digest != manifestDigest {
		return Decision{Code: "admission_manifest_payload_mismatch"}
	}
	for _, field := range []string{"proxy_generation", "credential_generation", "profile_generation", "sidecar_protocol_generation", "replay_ledger_generation"} {
		candidate, candidateOK := int64Value(certificate[field])
		wanted, wantedOK := int64Value(expected[field])
		if !candidateOK || !wantedOK {
			return Decision{Code: "admission_schema_invalid"}
		}
		if candidate < wanted {
			return Decision{Code: "admission_downgrade", Detail: field}
		}
		if candidate != wanted {
			return Decision{Code: "admission_tuple_mismatch", Detail: field}
		}
	}
	for _, field := range []string{"contract_digest", "manifest_digest", "package_artifact_sha256", "package_version"} {
		candidate, candidateOK := stringValue(certificate[field])
		wanted, wantedOK := stringValue(expected[field])
		if !candidateOK || !wantedOK || candidate != wanted {
			return Decision{Code: "admission_tuple_mismatch", Detail: field}
		}
	}
	selectedNegative, negativeSelectionOK := selectedNegativeCapability(certificate, context, negative)
	if !negativeSelectionOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	if selectedNegative {
		return Decision{Code: "admission_negative_capability"}
	}
	signalMap := make(map[string]map[string]any, len(signals))
	for _, rawSignal := range signals {
		signal, ok := objectValue(rawSignal)
		if !ok {
			return Decision{Code: "admission_schema_invalid"}
		}
		id, ok := stringValue(signal["signal_id"])
		if !ok || id == "" {
			return Decision{Code: "admission_schema_invalid"}
		}
		signalMap[id] = signal
	}
	gates, gatesOK := objectValue(certificate["gates"])
	if !gatesOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	for _, gateName := range []string{"wire", "semantic", "state_sequence", "failure_semantics"} {
		gate, ok := objectValue(gates[gateName])
		if !ok {
			return Decision{Code: "admission_schema_invalid"}
		}
		status, statusOK := stringValue(gate["status"])
		if !statusOK {
			return Decision{Code: "admission_schema_invalid"}
		}
		switch status {
		case "fail":
			return Decision{Code: "admission_gate_failed", Detail: gateName}
		case "unsupported":
			return Decision{Code: "admission_gate_unsupported", Detail: gateName}
		case "unobserved":
			return Decision{Code: "admission_gate_unobserved", Detail: gateName}
		case "pass":
		default:
			return Decision{Code: "admission_schema_invalid"}
		}
		signalID, signalIDOK := stringValue(gate["authority_signal_id"])
		if !signalIDOK {
			return Decision{Code: "admission_schema_invalid"}
		}
		decision := admissionAuthorityDecision(signalMap[signalID], context, negative)
		if decision.Code != "" {
			decision.Detail = gateName
			return decision
		}
	}
	return Decision{Allowed: true, Code: "admission_allow"}
}

func mustSchemaDefinition(schemas *SchemaSet, name string) map[string]any {
	definitions, _ := schemas.root["$defs"].(map[string]any)
	definition, _ := definitions[name].(map[string]any)
	return definition
}

func validAdmissionContextShape(context map[string]any) bool {
	if !exactKeys(context, "now_ms", "minimum_authority_state", "expected", "requested_capabilities", "invalidated_dependency_digests", "signals", "negative_capabilities") {
		return false
	}
	if _, ok := int64Value(context["now_ms"]); !ok {
		return false
	}
	minimum, ok := stringValue(context["minimum_authority_state"])
	if !ok {
		return false
	}
	if _, ok = authorityRanks[minimum]; !ok {
		return false
	}
	expected, ok := objectValue(context["expected"])
	if !ok || !exactKeys(expected, "contract_digest", "manifest_digest", "manifest_payload_digest", "package_artifact_sha256", "package_version", "proxy_generation", "credential_generation", "profile_generation", "sidecar_protocol_generation", "replay_ledger_generation") {
		return false
	}
	for _, field := range []string{"contract_digest", "manifest_digest", "manifest_payload_digest", "package_artifact_sha256"} {
		value, present := stringValue(expected[field])
		if !present || !isSHA256(value) {
			return false
		}
	}
	if value, present := stringValue(expected["package_version"]); !present || value == "" {
		return false
	}
	for _, field := range []string{"proxy_generation", "credential_generation", "profile_generation", "sidecar_protocol_generation", "replay_ledger_generation"} {
		value, present := int64Value(expected[field])
		if !present || value < 0 {
			return false
		}
	}
	if _, ok := stringArray(context["requested_capabilities"], 4_096); !ok {
		return false
	}
	if values, ok := stringArray(context["invalidated_dependency_digests"], 4_096); !ok || !allSHA256(values) {
		return false
	}
	if _, ok := arrayValue(context["signals"]); !ok {
		return false
	}
	negative, ok := objectValue(context["negative_capabilities"])
	return ok && validNegativeCapabilities(negative)
}

func validNegativeCapabilities(negative map[string]any) bool {
	if !exactKeys(negative, "models", "beta_tokens", "transports", "entrypoints", "fallbacks", "feature_combinations", "authority_states") {
		return false
	}
	for _, field := range []string{"models", "beta_tokens", "transports", "entrypoints", "fallbacks", "feature_combinations", "authority_states"} {
		if _, ok := stringArray(negative[field], 4_096); !ok {
			return false
		}
	}
	return true
}

func selectedNegativeCapability(certificate, context, negative map[string]any) (bool, bool) {
	denied := make(map[string]bool)
	for _, field := range []string{"models", "beta_tokens", "transports", "entrypoints", "fallbacks", "feature_combinations"} {
		values, _ := stringArray(negative[field], 4_096)
		for _, value := range values {
			denied[value] = true
		}
	}
	for _, field := range []string{"package_version", "entrypoint", "model_capability_set_ref", "tls_http_profile_ref", "persona_ref", "request_ast_profile_ref", "response_profile_ref"} {
		value, ok := stringValue(certificate[field])
		if !ok {
			return false, false
		}
		if denied[value] {
			return true, true
		}
	}
	requested, _ := stringArray(context["requested_capabilities"], 4_096)
	for _, value := range requested {
		if denied[value] {
			return true, true
		}
	}
	return false, true
}

func admissionAuthorityDecision(signal map[string]any, context, negative map[string]any) Decision {
	if signal == nil {
		return Decision{Code: "admission_authority_insufficient"}
	}
	contradiction, contradictionOK := stringValue(signal["contradiction_status"])
	contradictory, contradictoryOK := stringArray(signal["contradictory_evidence"], 4_096)
	if !contradictionOK || !contradictoryOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	if contradiction == "open" || len(contradictory) > 0 {
		return Decision{Code: "admission_authority_contradicted"}
	}
	expires, expiresOK := int64Value(signal["expires_at_ms"])
	now, _ := int64Value(context["now_ms"])
	if !expiresOK || expires < now {
		return Decision{Code: "admission_authority_expired"}
	}
	dependencies, dependenciesOK := stringArray(signal["invalidating_dependency_digests"], 4_096)
	invalidated, _ := stringArray(context["invalidated_dependency_digests"], 4_096)
	if !dependenciesOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	for _, dependency := range dependencies {
		if containsString(invalidated, dependency) {
			return Decision{Code: "admission_dependency_invalidated"}
		}
	}
	state, stateOK := stringValue(signal["authority_state"])
	minimum, minimumOK := stringValue(context["minimum_authority_state"])
	if !stateOK || !minimumOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	if authorityRanks[state] < authorityRanks[minimum] {
		return Decision{Code: "admission_authority_insufficient"}
	}
	serverDependent, serverOK := boolValue(signal["server_dependency"])
	scope, scopeOK := stringValue(signal["observation_scope"])
	if !serverOK || !scopeOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	if serverDependent && localObservationScopes[scope] {
		return Decision{Code: "admission_authority_insufficient"}
	}
	negativeStates, _ := stringArray(negative["authority_states"], 4_096)
	if containsString(negativeStates, state) {
		return Decision{Code: "admission_negative_capability"}
	}
	return Decision{}
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, current := range []byte(value) {
		if !((current >= '0' && current <= '9') || (current >= 'a' && current <= 'f')) {
			return false
		}
	}
	return true
}

func allSHA256(values []string) bool {
	for _, value := range values {
		if !isSHA256(value) {
			return false
		}
	}
	return true
}
