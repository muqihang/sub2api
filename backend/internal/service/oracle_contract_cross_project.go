package service

import "encoding/json"

const oracleMaximumGeneration int64 = 9007199254740991

func oracleSafeRef(value string) bool {
	if len(value) < 1 || len(value) > 200 {
		return false
	}
	for index := 0; index < len(value); index++ {
		current := value[index]
		if current >= 'A' && current <= 'Z' || current >= 'a' && current <= 'z' || current >= '0' && current <= '9' || current == '.' || current == '_' || current == ':' || current == '/' || current == '-' {
			continue
		}
		return false
	}
	return true
}

func oracleSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' && value[index] < 'a' || value[index] > 'f' {
			return false
		}
	}
	return true
}

func oracleGeneration(value int64) bool {
	return value >= 0 && value <= oracleMaximumGeneration
}

func oracleUniqueSafeRefs(values []string, maximum int) bool {
	if len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !oracleSafeRef(value) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

type OracleSupportedContractRange struct {
	SchemaMajor     int `json:"schema_major"`
	MinimumRevision int `json:"minimum_revision"`
	MaximumRevision int `json:"maximum_revision"`
}

type OracleReadinessHandshake struct {
	SchemaID               string                         `json:"schema_id"`
	SchemaMajor            int                            `json:"schema_major"`
	SchemaRevision         int                            `json:"schema_revision"`
	Kind                   string                         `json:"kind"`
	Liveness               bool                           `json:"liveness"`
	Readiness              bool                           `json:"readiness"`
	ProtectedCapability    bool                           `json:"protected_capability"`
	BuildDigest            string                         `json:"build_digest"`
	ContractDigest         string                         `json:"contract_digest"`
	ManifestDigest         string                         `json:"manifest_digest"`
	ProfileGeneration      int64                          `json:"profile_generation"`
	SidecarGeneration      int64                          `json:"sidecar_generation"`
	ReplayLedgerGeneration int64                          `json:"replay_ledger_generation"`
	SupportedContracts     []OracleSupportedContractRange `json:"supported_contracts"`
	DisabledCapabilities   []string                       `json:"disabled_capabilities"`
	ExpiresAtMS            int64                          `json:"expires_at_ms"`
}

type OracleReadinessExpected struct {
	NowMS                  int64  `json:"now_ms"`
	SchemaMajor            int    `json:"schema_major"`
	SchemaRevision         int    `json:"schema_revision"`
	BuildDigest            string `json:"build_digest"`
	ContractDigest         string `json:"contract_digest"`
	ManifestDigest         string `json:"manifest_digest"`
	ProfileGeneration      int64  `json:"profile_generation"`
	SidecarGeneration      int64  `json:"sidecar_generation"`
	ReplayLedgerGeneration int64  `json:"replay_ledger_generation"`
	RequiredCapability     string `json:"required_capability"`
}

type OracleCrossProjectDecision struct {
	Allowed         bool
	Code            string
	NextState       any
	NextStateDigest string
}

func oracleExactInterfaceSchema(schemaID string, schemaMajor, schemaRevision int, kind, expectedKind string) bool {
	return schemaID == "oracle.compatibility" && schemaMajor == 1 && schemaRevision == 0 && kind == expectedKind
}

func oracleSupportedRangesValid(ranges []OracleSupportedContractRange) bool {
	if len(ranges) == 0 || len(ranges) > 16 {
		return false
	}
	for index, current := range ranges {
		if current.SchemaMajor < 0 || current.MinimumRevision < 0 || current.MaximumRevision < current.MinimumRevision {
			return false
		}
		if index > 0 {
			previous := ranges[index-1]
			if current.SchemaMajor < previous.SchemaMajor || current.SchemaMajor == previous.SchemaMajor && current.MinimumRevision <= previous.MaximumRevision {
				return false
			}
		}
	}
	return true
}

func oracleReadinessShapeValid(handshake OracleReadinessHandshake) bool {
	return oracleSHA256(handshake.BuildDigest) && oracleSHA256(handshake.ContractDigest) && oracleSHA256(handshake.ManifestDigest) &&
		oracleGeneration(handshake.ProfileGeneration) && oracleGeneration(handshake.SidecarGeneration) && oracleGeneration(handshake.ReplayLedgerGeneration) &&
		oracleSupportedRangesValid(handshake.SupportedContracts) && oracleUniqueSafeRefs(handshake.DisabledCapabilities, 128) && oracleGeneration(handshake.ExpiresAtMS)
}

func DecideOracleReadiness(handshake OracleReadinessHandshake, expected OracleReadinessExpected, onReady func()) OracleCrossProjectDecision {
	if !oracleExactInterfaceSchema(handshake.SchemaID, handshake.SchemaMajor, handshake.SchemaRevision, handshake.Kind, "readiness_handshake") || !oracleReadinessShapeValid(handshake) {
		return OracleCrossProjectDecision{Code: "interface_schema_unsupported"}
	}
	supported := false
	for _, candidate := range handshake.SupportedContracts {
		if candidate.SchemaMajor == expected.SchemaMajor && candidate.MinimumRevision <= expected.SchemaRevision && candidate.MaximumRevision >= expected.SchemaRevision && candidate.MinimumRevision <= candidate.MaximumRevision {
			supported = true
			break
		}
	}
	if !supported {
		return OracleCrossProjectDecision{Code: "interface_schema_unsupported"}
	}
	if handshake.ContractDigest != expected.ContractDigest {
		return OracleCrossProjectDecision{Code: "interface_contract_mismatch"}
	}
	if handshake.BuildDigest != expected.BuildDigest || handshake.ManifestDigest != expected.ManifestDigest || handshake.ProfileGeneration != expected.ProfileGeneration || handshake.SidecarGeneration != expected.SidecarGeneration || handshake.ReplayLedgerGeneration != expected.ReplayLedgerGeneration {
		return OracleCrossProjectDecision{Code: "interface_generation_mismatch"}
	}
	if !handshake.Liveness || !handshake.Readiness || !handshake.ProtectedCapability || handshake.ExpiresAtMS < expected.NowMS || oracleContains(handshake.DisabledCapabilities, expected.RequiredCapability) {
		return OracleCrossProjectDecision{Code: "interface_not_ready"}
	}
	if onReady != nil {
		onReady()
	}
	return OracleCrossProjectDecision{Allowed: true, Code: "interface_allow"}
}

type OracleLifecycleState struct {
	Owner                string `json:"owner"`
	AccountRef           string `json:"account_ref"`
	AccountGeneration    int64  `json:"account_generation"`
	CredentialGeneration int64  `json:"credential_generation"`
	ProxyGeneration      int64  `json:"proxy_generation"`
	ProfileGeneration    int64  `json:"profile_generation"`
	StateVersion         int64  `json:"state_version"`
	Status               string `json:"status"`
}

type OracleLifecycleOperation struct {
	SchemaID             string `json:"schema_id"`
	SchemaMajor          int    `json:"schema_major"`
	SchemaRevision       int    `json:"schema_revision"`
	Kind                 string `json:"kind"`
	Operation            string `json:"operation"`
	Owner                string `json:"owner"`
	AccountRef           string `json:"account_ref"`
	AccountGeneration    int64  `json:"account_generation"`
	CredentialGeneration int64  `json:"credential_generation"`
	ProxyGeneration      int64  `json:"proxy_generation"`
	ProfileGeneration    int64  `json:"profile_generation"`
	ExpectedStateVersion int64  `json:"expected_state_version"`
	NextStateVersion     int64  `json:"next_state_version"`
	IdempotencyKey       string `json:"idempotency_key"`
}

func oracleLifecycleOperationShapeValid(operation OracleLifecycleOperation) bool {
	return oracleContains([]string{"register", "replace", "freeze", "drain", "revoke", "delete", "query", "reconcile"}, operation.Operation) &&
		oracleContains([]string{"sub2api", "cc_gateway"}, operation.Owner) && oracleSafeRef(operation.AccountRef) && oracleSafeRef(operation.IdempotencyKey) &&
		oracleGeneration(operation.AccountGeneration) && oracleGeneration(operation.CredentialGeneration) && oracleGeneration(operation.ProxyGeneration) &&
		oracleGeneration(operation.ProfileGeneration) && oracleGeneration(operation.ExpectedStateVersion) && oracleGeneration(operation.NextStateVersion)
}

func oracleCrossProjectStateDigest(value any) string {
	raw, _ := json.Marshal(value)
	canonical, err := CanonicalizeOracleJSON(raw)
	if err != nil {
		return ""
	}
	return canonical.SHA256
}

func TransitionOracleLifecycle(state OracleLifecycleState, operation OracleLifecycleOperation) OracleCrossProjectDecision {
	if !oracleExactInterfaceSchema(operation.SchemaID, operation.SchemaMajor, operation.SchemaRevision, operation.Kind, "lifecycle_operation") || !oracleLifecycleOperationShapeValid(operation) {
		return OracleCrossProjectDecision{Code: "interface_schema_unsupported"}
	}
	if operation.Owner != "sub2api" || state.Owner != "sub2api" || operation.AccountRef != state.AccountRef {
		return OracleCrossProjectDecision{Code: "interface_owner_mismatch"}
	}
	if operation.ExpectedStateVersion != state.StateVersion || operation.NextStateVersion != state.StateVersion+1 {
		return OracleCrossProjectDecision{Code: "interface_stale_state"}
	}
	if operation.AccountGeneration < state.AccountGeneration || operation.CredentialGeneration < state.CredentialGeneration || operation.ProxyGeneration < state.ProxyGeneration || operation.ProfileGeneration < state.ProfileGeneration {
		return OracleCrossProjectDecision{Code: "interface_generation_regression"}
	}
	if operation.Operation == "register" && state.Status != "absent" || operation.Operation == "replace" && state.Status != "active" {
		return OracleCrossProjectDecision{Code: "interface_state_transition_invalid"}
	}
	statuses := map[string]string{"register": "active", "replace": "active", "freeze": "frozen", "drain": "draining", "revoke": "revoked", "delete": "deleted", "query": state.Status, "reconcile": state.Status}
	next := OracleLifecycleState{
		Owner: "sub2api", AccountRef: state.AccountRef, AccountGeneration: operation.AccountGeneration,
		CredentialGeneration: operation.CredentialGeneration, ProxyGeneration: operation.ProxyGeneration,
		ProfileGeneration: operation.ProfileGeneration, StateVersion: operation.NextStateVersion, Status: statuses[operation.Operation],
	}
	return OracleCrossProjectDecision{Allowed: true, Code: "interface_allow", NextState: next, NextStateDigest: oracleCrossProjectStateDigest(next)}
}

type OracleTaskLineageState struct {
	RootTaskRef       string `json:"root_task_ref"`
	CurrentTaskRef    string `json:"current_task_ref"`
	ClientGeneration  int64  `json:"client_generation"`
	ProfileGeneration int64  `json:"profile_generation"`
	MigrationSequence int64  `json:"migration_sequence"`
}

type OracleTaskLineage struct {
	SchemaID          string `json:"schema_id"`
	SchemaMajor       int    `json:"schema_major"`
	SchemaRevision    int    `json:"schema_revision"`
	Kind              string `json:"kind"`
	RootTaskRef       string `json:"root_task_ref"`
	ParentTaskRef     string `json:"parent_task_ref"`
	CurrentTaskRef    string `json:"current_task_ref"`
	ClientGeneration  int64  `json:"client_generation"`
	ProfileGeneration int64  `json:"profile_generation"`
	MigrationSequence int64  `json:"migration_sequence"`
	AttemptID         string `json:"attempt_id"`
	DeadlineMS        int64  `json:"deadline_ms"`
	IdempotencyKey    string `json:"idempotency_key"`
}

func oracleTaskLineageShapeValid(candidate OracleTaskLineage) bool {
	return oracleSafeRef(candidate.RootTaskRef) && oracleSafeRef(candidate.ParentTaskRef) && oracleSafeRef(candidate.CurrentTaskRef) &&
		oracleSafeRef(candidate.AttemptID) && oracleSafeRef(candidate.IdempotencyKey) && oracleGeneration(candidate.ClientGeneration) &&
		oracleGeneration(candidate.ProfileGeneration) && oracleGeneration(candidate.MigrationSequence) && oracleGeneration(candidate.DeadlineMS)
}

func DecideOracleTaskLineage(state OracleTaskLineageState, candidate OracleTaskLineage, nowMS int64) OracleCrossProjectDecision {
	if !oracleExactInterfaceSchema(candidate.SchemaID, candidate.SchemaMajor, candidate.SchemaRevision, candidate.Kind, "task_lineage") || !oracleTaskLineageShapeValid(candidate) {
		return OracleCrossProjectDecision{Code: "interface_schema_unsupported"}
	}
	if candidate.RootTaskRef != state.RootTaskRef || candidate.ParentTaskRef != state.CurrentTaskRef || candidate.CurrentTaskRef == state.CurrentTaskRef {
		return OracleCrossProjectDecision{Code: "interface_lineage_mismatch"}
	}
	if candidate.MigrationSequence != state.MigrationSequence+1 || candidate.ClientGeneration < state.ClientGeneration || candidate.ProfileGeneration < state.ProfileGeneration {
		return OracleCrossProjectDecision{Code: "interface_migration_stale"}
	}
	if candidate.DeadlineMS < nowMS {
		return OracleCrossProjectDecision{Code: "interface_deadline_expired"}
	}
	next := OracleTaskLineageState{RootTaskRef: state.RootTaskRef, CurrentTaskRef: candidate.CurrentTaskRef, ClientGeneration: candidate.ClientGeneration, ProfileGeneration: candidate.ProfileGeneration, MigrationSequence: candidate.MigrationSequence}
	return OracleCrossProjectDecision{Allowed: true, Code: "interface_allow", NextState: next, NextStateDigest: oracleCrossProjectStateDigest(next)}
}

type OracleOutcomeEnvelope struct {
	SchemaID           string `json:"schema_id"`
	SchemaMajor        int    `json:"schema_major"`
	SchemaRevision     int    `json:"schema_revision"`
	Kind               string `json:"kind"`
	AttemptID          string `json:"attempt_id"`
	TransportFact      string `json:"transport_fact"`
	SemanticOutcome    string `json:"semantic_outcome"`
	PartialOutput      bool   `json:"partial_output"`
	ToolSideEffect     bool   `json:"tool_side_effect"`
	RetryOwner         string `json:"retry_owner"`
	Terminal           bool   `json:"terminal"`
	FinalHeadersSHA256 string `json:"final_headers_sha256"`
	FinalBodySHA256    string `json:"final_body_sha256"`
}

func oracleOutcomeShapeValid(outcome OracleOutcomeEnvelope) bool {
	return oracleSafeRef(outcome.AttemptID) && oracleSHA256(outcome.FinalHeadersSHA256) && oracleSHA256(outcome.FinalBodySHA256) &&
		oracleContains([]string{"not_attempted", "connected", "reset", "timeout", "rejected"}, outcome.TransportFact) &&
		oracleContains([]string{"none", "success", "client_error", "rate_limited", "capacity", "server_error", "malformed", "cancelled"}, outcome.SemanticOutcome) &&
		oracleContains([]string{"none", "cc_gateway", "sub2api"}, outcome.RetryOwner)
}

func DecideOracleOutcome(outcome OracleOutcomeEnvelope) OracleCrossProjectDecision {
	if !oracleExactInterfaceSchema(outcome.SchemaID, outcome.SchemaMajor, outcome.SchemaRevision, outcome.Kind, "outcome_envelope") || !oracleOutcomeShapeValid(outcome) {
		return OracleCrossProjectDecision{Code: "interface_schema_unsupported"}
	}
	if outcome.PartialOutput || outcome.ToolSideEffect || outcome.Terminal {
		return OracleCrossProjectDecision{Allowed: true, Code: "interface_terminal_no_retry"}
	}
	if outcome.SemanticOutcome == "rate_limited" && outcome.RetryOwner == "sub2api" {
		return OracleCrossProjectDecision{Allowed: true, Code: "interface_sub2api_retry"}
	}
	if outcome.RetryOwner == "cc_gateway" {
		return OracleCrossProjectDecision{Allowed: true, Code: "interface_gateway_retry"}
	}
	return OracleCrossProjectDecision{Allowed: true, Code: "interface_terminal_no_retry"}
}

type OracleInterfaceReplayEntry struct {
	State       string `json:"state"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
}

type OracleInterfaceReplayState struct {
	LedgerGeneration int64                                 `json:"ledger_generation"`
	Entries          map[string]OracleInterfaceReplayEntry `json:"entries"`
}

type OracleInterfaceReplayCommand struct {
	Operation          string
	ExpectedGeneration int64
	NowMS              int64
	ExpiresAtMS        int64
	KeyEpoch           int64
	CapabilityID       string
	AttemptID          string
	Nonce              string
}

func oracleInterfaceReplayIdentity(command OracleInterfaceReplayCommand) string {
	return oracleCrossProjectStateDigest(struct {
		AttemptID    string `json:"attempt_id"`
		CapabilityID string `json:"capability_id"`
		KeyEpoch     int64  `json:"key_epoch"`
		Nonce        string `json:"nonce"`
	}{command.AttemptID, command.CapabilityID, command.KeyEpoch, command.Nonce})
}

func TransitionOracleInterfaceReplay(state OracleInterfaceReplayState, command OracleInterfaceReplayCommand) OracleCrossProjectDecision {
	if command.ExpectedGeneration != state.LedgerGeneration {
		return OracleCrossProjectDecision{Code: "replay_replica_conflict"}
	}
	identity := oracleInterfaceReplayIdentity(command)
	current, exists := state.Entries[identity]
	var nextEntry OracleInterfaceReplayEntry
	switch command.Operation {
	case "reserve":
		if exists || command.ExpiresAtMS <= command.NowMS {
			return OracleCrossProjectDecision{Code: "replay_rejected"}
		}
		nextEntry = OracleInterfaceReplayEntry{State: "reserved", ExpiresAtMS: command.ExpiresAtMS}
	case "commit":
		if !exists || current.State != "reserved" || current.ExpiresAtMS <= command.NowMS {
			return OracleCrossProjectDecision{Code: "replay_rejected"}
		}
		nextEntry = current
		nextEntry.State = "committed"
	default:
		return OracleCrossProjectDecision{Code: "replay_rejected"}
	}
	next := OracleInterfaceReplayState{LedgerGeneration: state.LedgerGeneration + 1, Entries: make(map[string]OracleInterfaceReplayEntry, len(state.Entries)+1)}
	for key, entry := range state.Entries {
		next.Entries[key] = entry
	}
	next.Entries[identity] = nextEntry
	code := "replay_reserved"
	if command.Operation == "commit" {
		code = "replay_committed"
	}
	return OracleCrossProjectDecision{Allowed: true, Code: code, NextState: next, NextStateDigest: oracleCrossProjectStateDigest(next)}
}
