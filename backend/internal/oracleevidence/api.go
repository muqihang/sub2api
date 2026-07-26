package oracleevidence

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	CodeOracleNotImplemented = "oracle_not_implemented"
	CodeJSONInvalid          = "json_invalid"
	CodeJSONTypeInvalid      = "json_type_invalid"
	CodeCBORInvalid          = "cbor_invalid"
	CodeURLHostInvalid       = "url_host_invalid"
	CodeURLPortInvalid       = "url_port_invalid"
	CodeMutationPointer      = "mutation_pointer_invalid"
	CodeMutationSource       = "mutation_source_invalid"
	CodeContractBundle       = "contract_bundle_missing"
)

// StableCodes is the closed contract code registry frozen by the rebaseline.
var StableCodes = []string{
	"admission_allow", "admission_authority_contradicted", "admission_authority_expired",
	"admission_authority_insufficient", "admission_dependency_invalidated", "admission_downgrade",
	"admission_gate_failed", "admission_gate_unobserved", "admission_gate_unsupported",
	"admission_manifest_payload_mismatch", "admission_negative_capability", "admission_schema_invalid",
	"admission_tuple_mismatch", "authority_allow", "authority_checkpoint_stale",
	"authority_clock_rollback", "authority_dependency_invalidated", "authority_diagnostic_promotion",
	"authority_duplicate_signer", "authority_expired", "authority_freeze", "authority_key_revoked",
	"authority_mix_and_match", "authority_parent_mismatch", "authority_policy_rollback",
	"authority_replica_conflict", "authority_resource_limit", "authority_revocation_invalid",
	"authority_revocation_stale", "authority_rotation_threshold", "authority_signature_invalid",
	"authority_split_view", "authority_threshold_insufficient", "authority_witness_mismatch",
	"authority_wrong_role", "cbor_duplicate_key", "cbor_float_forbidden", "cbor_frame_length",
	"cbor_frame_truncated", "cbor_indefinite_length", "cbor_integer_unsafe", "cbor_invalid",
	"cbor_invalid_utf8", "cbor_map_key_invalid", "cbor_not_deterministic", "cbor_resource_limit",
	"cbor_simple_forbidden", "cbor_tag_forbidden", "cbor_trailing_data", "cbor_truncated",
	"cbor_type_invalid", "cbor_undefined_forbidden", "contract_bundle_missing",
	"contract_file_digest_mismatch", "contract_file_set_invalid", "contract_index_not_canonical",
	"contract_index_path_invalid", "contract_index_version_invalid", "contract_json_invalid",
	"contract_mirror_mismatch", "contract_predecessor_mismatch", "contract_required_set_mismatch",
	"contract_schema_invalid", "contract_schema_keyword_unsupported", "contract_schema_range_mismatch",
	"contract_symlink", "cross_repo_binding_mismatch", "cross_repo_record_expired",
	"cross_repo_result_mismatch", "interface_allow", "interface_contract_mismatch",
	"interface_deadline_expired", "interface_gateway_retry", "interface_generation_mismatch",
	"interface_generation_regression", "interface_lineage_mismatch", "interface_migration_stale",
	"interface_not_ready", "interface_owner_mismatch", "interface_schema_unsupported",
	"interface_stale_state", "interface_state_transition_invalid", "interface_sub2api_retry",
	"interface_terminal_no_retry", "json_canonicalization_failed", "json_duplicate_key", "json_invalid",
	"json_invalid_utf8", "json_lone_surrogate", "json_negative_zero", "json_number_invalid",
	"json_number_unsafe", "json_trailing_data", "json_type_invalid", "leak_detected",
	"mutation_descriptor_invalid", "mutation_executor_unexercised", "mutation_pointer_invalid",
	"mutation_source_invalid", "oracle_not_implemented", "replay_committed", "replay_expired",
	"replay_rejected", "replay_replica_conflict", "replay_reserved", "replay_revoked",
	"sidecar_capability_allow", "sidecar_capability_decode_invalid", "sidecar_capability_expired",
	"sidecar_capability_schema_invalid", "sidecar_key_epoch_mismatch", "sidecar_key_not_found",
	"sidecar_key_revoked", "sidecar_key_role_invalid", "sidecar_key_role_reuse",
	"sidecar_signature_invalid", "url_host_invalid", "url_path_invalid", "url_port_invalid",
}

type Decision struct {
	Allowed         bool
	Code            string
	Detail          string
	NextState       []byte
	NextStateDigest string
}

type ContractError struct {
	Code   string
	Detail string
}

func (e *ContractError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

type RawPort string

type SchemaSet struct {
	bundleRoot      string
	authoritySHA256 string
	root            map[string]any
}

type MutationOperation struct {
	Kind        string
	Pointer     string
	Value       any
	Offset      uint64
	DeleteCount uint64
	BytesBase64 string
	Path        string
	Mode        uint32
	Target      string
}

type MutationCase struct {
	CaseID    string
	Subject   string
	Source    SourceBinding
	Operation MutationOperation
	Expected  Decision
}

type MutationResult struct {
	CaseID       string
	Allowed      bool
	Code         string
	OutputSHA256 string
}

type SourceBinding struct {
	RelativePath string
	SHA256       string
	Size         uint64
	MaxBytes     uint64
	Kind         string
	ModeClass    string
}

type AuthorityInput struct {
	State     []byte
	Candidate []byte
	Context   []byte
}

type CrossRepoRecord struct {
	Canonical []byte
	Digest    string
}

func contractErr(code string) error { return &ContractError{Code: code} }

func notImplementedErr() error { return contractErr(CodeOracleNotImplemented) }

func notImplementedDecision() Decision {
	return Decision{Allowed: false, Code: CodeOracleNotImplemented}
}

func decodeReachedJSON(input []byte) (any, error) {
	if len(input) == 0 || len(input) > 1<<20 {
		return nil, contractErr(CodeJSONInvalid)
	}
	dec := json.NewDecoder(strings.NewReader(string(input)))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, contractErr(CodeJSONInvalid)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, contractErr(CodeJSONInvalid)
	}
	return value, nil
}

func ParseStrictJSON(input []byte) (any, error) {
	return parseStrictJSONImpl(input)
}

func ValidateJSONValue(value any) error {
	return validateJSONValueImpl(value)
}

func CanonicalizeJSON(input []byte) ([]byte, error) {
	return canonicalizeJSONImpl(input)
}

func CanonicalizeValue(value any) ([]byte, error) {
	return canonicalizeValueImpl(value)
}

func NormalizePathQuery(pathname string, pairs [][2]string) (string, error) {
	return normalizePathQueryImpl(pathname, pairs)
}

func ParseAuthorityPort(raw RawPort) (uint16, error) {
	return parseAuthorityPortImpl(raw)
}

func FormatAuthority(host string, rawPort RawPort) (string, error) {
	return formatAuthorityImpl(host, rawPort)
}

func SHA256Hex(input []byte) string {
	return sha256HexImpl(input)
}

func CanonicalizeCBOR(input []byte) ([]byte, error) {
	return canonicalizeCBORImpl(input)
}

func DecodeDeterministicCBOR(input []byte) (any, error) {
	return decodeDeterministicCBORImpl(input)
}

func EncodeCBORFrame(value any) ([]byte, error) {
	return encodeCBORFrameImpl(value)
}

func DecodeCBORFrame(input []byte) (any, error) {
	return decodeCBORFrameImpl(input)
}

func LoadContractSchema(bundleRoot string) (*SchemaSet, error) {
	return loadContractSchemaImpl(bundleRoot)
}

func ValidateContractObject(schemas *SchemaSet, definition string, input []byte) Decision {
	return validateContractObjectImpl(schemas, definition, input)
}

func AdmissionPayloadDigest(certificate, signals, negativeCapabilities []byte) (string, error) {
	return admissionPayloadDigestImpl(certificate, signals, negativeCapabilities)
}

func DecideBehaviorAdmission(certificate, context []byte) Decision {
	return decideBehaviorAdmissionImpl(certificate, context)
}

func VerifyManifestAuthorityUpdate(input AuthorityInput) Decision {
	return verifyManifestAuthorityUpdateImpl(input)
}
func VerifyRootRotation(input AuthorityInput) Decision { return verifyRootRotationImpl(input) }
func VerifyEmergencyRevocation(input AuthorityInput) Decision {
	return verifyEmergencyRevocationImpl(input)
}

func reachedAuthority(input AuthorityInput) Decision {
	if len(input.Candidate) == 0 {
		return Decision{Code: "authority_signature_invalid"}
	}
	return notImplementedDecision()
}

func TrustStateDigest(state []byte) (string, error) {
	return trustStateDigestImpl(state)
}

func DecideReadiness(handshake, expected []byte) Decision {
	return decideReadinessImpl(handshake, expected)
}

func DecideLifecycle(state, operation []byte) Decision {
	return decideLifecycleImpl(state, operation)
}

func DecideTaskLineage(state, candidate []byte, nowMS int64) Decision {
	return decideTaskLineageImpl(state, candidate, nowMS)
}

func DecideOutcome(outcome []byte) Decision {
	return decideOutcomeImpl(outcome)
}

func ExecuteReplay(state, command []byte) Decision {
	return executeReplayImpl(state, command)
}

func reachedPair(left, right []byte, invalidCode string) Decision {
	if len(left) == 0 || len(right) == 0 {
		return Decision{Code: invalidCode}
	}
	return notImplementedDecision()
}

func ValidateSidecarEnvelope(envelope []byte, schemas *SchemaSet) Decision {
	return validateSidecarEnvelopeImpl(envelope, schemas)
}

func VerifySidecarCapability(envelope, capability, keyring []byte, nowMS int64) Decision {
	return verifySidecarCapabilityImpl(envelope, capability, keyring, nowMS)
}

func ParseBoundedPointerIndex(segment string, length uint64, allowEnd bool) (uint64, error) {
	return parseBoundedPointerIndexImpl(segment, length, allowEnd)
}

func ApplyMutation(source []byte, operation MutationOperation) ([]byte, error) {
	return applyMutationImpl(source, operation)
}

func ExecuteMutationCorpus(root string, corpus []byte, schemas *SchemaSet) ([]MutationResult, error) {
	return executeMutationCorpusImpl(root, corpus, schemas)
}

var mirrorDigests = map[string]string{
	"authority-corpus.json":        "42e89c1933f7c2b9f71dfd41d739345b3f2253f0217c6ebb2ee77b25ab94d8de",
	"canonicalization-corpus.json": "a2925a1c04aa90dbc42eee3045574faf829ccddaa776d75d2497558821c0ab20",
	"coherence-corpus.json":        "85b7209d31370bd56bb4a374cf796ecabd11ee191b30e9e9a485ff65b2d03d82",
	"contract-index.json":          "2545113fb928131ee5a735541b5373a00566b279263aca5b1cc11181aaf78bce",
	"contract.schema.json":         "380c7f3db80baa2d288838f3a550c3588abd19de11627d34ae90f5d3a0add4fe",
	"expected-results.json":        "8671744730e94e88b439f05a0e934539fe5b148b3e3dfdc1243beba9774ced44",
	"interface-corpus.json":        "9c2f0864097911b3b9612ee5bb6a4b62e363b2152abe7bfd5ff07221a6c60dca",
	"sidecar-envelope.cddl":        "7697364dcaa7189449e94305a4df86d8d5476078b3dee78fac2fb34ccc60905d",
	"sidecar-envelope.schema.json": "a9256710c040d2a018fbc42f188a59f11fc1dd9dc46ea7be89ca2294aaace003",
}

func InspectMirror(ccRoot, subRoot, predecessorPath string) Decision {
	return inspectMirrorImpl(ccRoot, subRoot, predecessorPath)
}

func inspectMirrorRoot(root string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return contractErr(CodeContractBundle)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return contractErr("contract_symlink")
	}
	if !rootInfo.IsDir() {
		return contractErr("contract_file_set_invalid")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return contractErr(CodeContractBundle)
	}
	if len(entries) != len(mirrorDigests) {
		return contractErr("contract_file_set_invalid")
	}
	for _, entry := range entries {
		expected, ok := mirrorDigests[entry.Name()]
		if entry.Type()&os.ModeSymlink != 0 {
			return contractErr("contract_symlink")
		}
		if !ok || !entry.Type().IsRegular() {
			return contractErr("contract_file_set_invalid")
		}
		data, readErr := readContractFile(filepath.Join(root, entry.Name()), maxJSONBytes)
		if readErr != nil || SHA256Hex(data) != expected {
			return contractErr("contract_file_digest_mismatch")
		}
	}
	return nil
}

func ValidateContractIndex(bundleRoot string) Decision {
	return validateContractIndexImpl(bundleRoot)
}

func ValidateCrossRepoRecord(input []byte) Decision {
	return validateCrossRepoRecordImpl(input)
}

func stableCodeDigest() string {
	encoded, err := json.Marshal(StableCodes)
	if err != nil {
		panic(fmt.Sprintf("marshal stable codes: %v", err))
	}
	return SHA256Hex(encoded)
}

func parseUint(segment string) (uint64, error) {
	value, err := strconv.ParseUint(segment, 10, 64)
	if err != nil {
		return 0, contractErr(CodeMutationPointer)
	}
	return value, nil
}
