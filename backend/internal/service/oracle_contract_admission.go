package service

import "encoding/json"

func oracleAdmissionPayloadDigest(certificate OracleBehaviorCoherenceCertificate, signals []OracleAuthoritySignal, negative OracleNegativeCapabilities) (string, error) {
	payload := struct {
		Certificate          OracleBehaviorCoherenceCertificate `json:"certificate"`
		NegativeCapabilities OracleNegativeCapabilities         `json:"negative_capabilities"`
		Signals              []OracleAuthoritySignal            `json:"signals"`
	}{certificate, negative, signals}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	canonical, err := CanonicalizeOracleJSON(raw)
	if err != nil {
		return "", err
	}
	return canonical.SHA256, nil
}

func OracleAdmissionPayloadDigest(certificateRaw []byte, signals []OracleAuthoritySignal, negative OracleNegativeCapabilities) (string, error) {
	var certificate OracleBehaviorCoherenceCertificate
	if err := decodeOracleStrictJSON(certificateRaw, &certificate); err != nil {
		return "", err
	}
	if err := validateOracleBehaviorCertificate(certificate); err != nil {
		return "", err
	}
	return oracleAdmissionPayloadDigest(certificate, signals, negative)
}

func oracleAdmissionDeny(code, action, gate, signalID, detail string) OracleAdmissionDecision {
	if action == "" {
		action = "disable"
	}
	return OracleAdmissionDecision{Code: code, Action: action, Gate: gate, SignalID: signalID, Detail: detail}
}

var oracleAuthorityRank = map[string]int{
	"unverified":                   0,
	"package_observed":             1,
	"local_wire_observed":          2,
	"cross_checked":                3,
	"gateway_wire_equivalent":      4,
	"stateful_behavior_equivalent": 5,
	"upstream_canary_observed":     6,
	"production_verified":          7,
}

func oracleContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func oracleAdmissionTuple(value OracleBehaviorCoherenceCertificate, context OracleAdmissionContext) *OracleAdmissionDecision {
	generations := []struct {
		Name     string
		Actual   int64
		Expected int64
	}{
		{"proxy_generation", value.ProxyGeneration, context.Expected.ProxyGeneration},
		{"credential_generation", value.CredentialGeneration, context.Expected.CredentialGeneration},
		{"profile_generation", value.ProfileGeneration, context.Expected.ProfileGeneration},
		{"sidecar_protocol_generation", value.SidecarProtocolGeneration, context.Expected.SidecarProtocolGeneration},
		{"replay_ledger_generation", value.ReplayLedgerGeneration, context.Expected.ReplayLedgerGeneration},
	}
	for _, generation := range generations {
		if generation.Actual < generation.Expected {
			decision := oracleAdmissionDeny("admission_downgrade", "rollback", "", "", generation.Name)
			return &decision
		}
		if generation.Actual != generation.Expected {
			decision := oracleAdmissionDeny("admission_tuple_mismatch", "disable", "", "", generation.Name)
			return &decision
		}
	}
	exact := []struct{ Name, Actual, Expected string }{
		{"contract_digest", value.ContractDigest, context.Expected.ContractDigest},
		{"manifest_digest", value.ManifestDigest, context.Expected.ManifestDigest},
		{"package_artifact_sha256", value.PackageArtifactSHA256, context.Expected.PackageArtifactSHA256},
		{"package_version", value.PackageVersion, context.Expected.PackageVersion},
	}
	for _, item := range exact {
		if item.Actual != item.Expected {
			decision := oracleAdmissionDeny("admission_tuple_mismatch", "disable", "", "", item.Name)
			return &decision
		}
	}
	return nil
}

func oracleAdmissionNegative(value OracleBehaviorCoherenceCertificate, context OracleAdmissionContext) *OracleAdmissionDecision {
	negative := context.NegativeCapabilities
	denied := append([]string{}, negative.Models...)
	denied = append(denied, negative.BetaTokens...)
	denied = append(denied, negative.Transports...)
	denied = append(denied, negative.Entrypoints...)
	denied = append(denied, negative.Fallbacks...)
	denied = append(denied, negative.FeatureCombinations...)
	selected := []string{
		value.PackageVersion, value.Entrypoint, value.ModelCapabilitySetRef, value.TLSHTTPProfileRef,
		value.PersonaRef, value.RequestASTProfileRef, value.ResponseProfileRef,
	}
	selected = append(selected, context.RequestedCapabilities...)
	for _, item := range selected {
		if oracleContains(denied, item) {
			decision := oracleAdmissionDeny("admission_negative_capability", "disable", "", "", item)
			return &decision
		}
	}
	return nil
}

func oracleAdmissionAuthority(signal *OracleAuthoritySignal, context OracleAdmissionContext) *OracleAdmissionDecision {
	if signal == nil {
		decision := oracleAdmissionDeny("admission_authority_insufficient", "disable", "", "", "")
		return &decision
	}
	action := signal.FailureAction
	if signal.ContradictionStatus == "open" || len(signal.ContradictoryEvidence) > 0 {
		decision := oracleAdmissionDeny("admission_authority_contradicted", action, "", signal.SignalID, "")
		return &decision
	}
	if signal.ExpiresAtMS < context.NowMS {
		decision := oracleAdmissionDeny("admission_authority_expired", action, "", signal.SignalID, "")
		return &decision
	}
	for _, digest := range signal.InvalidatingDependencyDigests {
		if oracleContains(context.InvalidatedDependencyDigests, digest) {
			decision := oracleAdmissionDeny("admission_dependency_invalidated", action, "", signal.SignalID, "")
			return &decision
		}
	}
	if oracleAuthorityRank[signal.AuthorityState] < oracleAuthorityRank[context.MinimumAuthorityState] {
		decision := oracleAdmissionDeny("admission_authority_insufficient", action, "", signal.SignalID, "")
		return &decision
	}
	if signal.ServerDependency && oracleContains([]string{"package", "local_fixture", "local_wire", "gateway"}, signal.ObservationScope) {
		decision := oracleAdmissionDeny("admission_authority_insufficient", action, "", signal.SignalID, "")
		return &decision
	}
	if oracleContains(context.NegativeCapabilities.AuthorityStates, signal.AuthorityState) {
		decision := oracleAdmissionDeny("admission_negative_capability", action, "", signal.SignalID, "")
		return &decision
	}
	return nil
}

func DecideOracleBehaviorAdmission(raw []byte, context OracleAdmissionContext, onAllowed func()) OracleAdmissionDecision {
	var certificate OracleBehaviorCoherenceCertificate
	if err := decodeOracleStrictJSON(raw, &certificate); err != nil || validateOracleBehaviorCertificate(certificate) != nil {
		return oracleAdmissionDeny("admission_schema_invalid", "disable", "", "", "")
	}
	payloadDigest, err := oracleAdmissionPayloadDigest(certificate, context.Signals, context.NegativeCapabilities)
	if err != nil || payloadDigest != context.Expected.ManifestPayloadDigest {
		return oracleAdmissionDeny("admission_manifest_payload_mismatch", "disable", "", "", "")
	}
	if decision := oracleAdmissionTuple(certificate, context); decision != nil {
		return *decision
	}
	if decision := oracleAdmissionNegative(certificate, context); decision != nil {
		return *decision
	}
	signals := make(map[string]*OracleAuthoritySignal, len(context.Signals))
	for index := range context.Signals {
		signal := &context.Signals[index]
		signals[signal.SignalID] = signal
	}
	gates := []struct {
		Name string
		Gate OracleCompatibilityGate
	}{
		{"wire", certificate.Gates.Wire},
		{"semantic", certificate.Gates.Semantic},
		{"state_sequence", certificate.Gates.StateSequence},
		{"failure_semantics", certificate.Gates.FailureSemantics},
	}
	for _, current := range gates {
		switch current.Gate.Status {
		case "fail":
			return oracleAdmissionDeny("admission_gate_failed", "disable", current.Name, "", "")
		case "unsupported":
			return oracleAdmissionDeny("admission_gate_unsupported", "disable", current.Name, "", "")
		case "unobserved":
			return oracleAdmissionDeny("admission_gate_unobserved", "disable", current.Name, "", "")
		}
		if decision := oracleAdmissionAuthority(signals[current.Gate.AuthoritySignalID], context); decision != nil {
			decision.Gate = current.Name
			return *decision
		}
	}
	if onAllowed != nil {
		onAllowed()
	}
	return OracleAdmissionDecision{Allowed: true, Code: "admission_allow"}
}
