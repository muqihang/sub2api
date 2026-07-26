package oracleevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
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
	if _, err := decodeReachedJSON(input); err != nil {
		return nil, err
	}
	return nil, notImplementedErr()
}

func ValidateJSONValue(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil || !json.Valid(encoded) {
		return contractErr(CodeJSONTypeInvalid)
	}
	return notImplementedErr()
}

func CanonicalizeJSON(input []byte) ([]byte, error) {
	if _, err := decodeReachedJSON(input); err != nil {
		return nil, err
	}
	return nil, notImplementedErr()
}

func CanonicalizeValue(value any) ([]byte, error) {
	if err := ValidateJSONValue(value); err != nil {
		if ce, ok := err.(*ContractError); ok && ce.Code == CodeJSONTypeInvalid {
			return nil, err
		}
	}
	return nil, notImplementedErr()
}

func NormalizePathQuery(pathname string, pairs [][2]string) (string, error) {
	if pathname == "" || !strings.HasPrefix(pathname, "/") {
		return "", contractErr("url_path_invalid")
	}
	return "", notImplementedErr()
}

func ParseAuthorityPort(raw RawPort) (uint16, error) {
	s := string(raw)
	if len(s) == 0 || len(s) > 5 || s[0] == '0' {
		return 0, contractErr(CodeURLPortInvalid)
	}
	var value uint32
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, contractErr(CodeURLPortInvalid)
		}
		value = value*10 + uint32(s[i]-'0')
		if value > math.MaxUint16 {
			return 0, contractErr(CodeURLPortInvalid)
		}
	}
	return 0, notImplementedErr()
}

func FormatAuthority(host string, rawPort RawPort) (string, error) {
	if _, err := ParseAuthorityPort(rawPort); err != nil {
		return "", err
	}
	if host == "" {
		return "", contractErr(CodeURLHostInvalid)
	}
	return "", notImplementedErr()
}

func SHA256Hex(input []byte) string {
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])
}

func CanonicalizeCBOR(input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, contractErr(CodeCBORInvalid)
	}
	return nil, notImplementedErr()
}

func DecodeDeterministicCBOR(input []byte) (any, error) {
	if len(input) == 0 {
		return nil, contractErr(CodeCBORInvalid)
	}
	return nil, notImplementedErr()
}

func EncodeCBORFrame(value any) ([]byte, error) {
	if value == nil {
		return nil, contractErr("cbor_type_invalid")
	}
	return nil, notImplementedErr()
}

func DecodeCBORFrame(input []byte) (any, error) {
	if len(input) == 0 {
		return nil, contractErr("cbor_frame_truncated")
	}
	return nil, notImplementedErr()
}

func LoadContractSchema(bundleRoot string) (*SchemaSet, error) {
	path := filepath.Join(bundleRoot, "contract.schema.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, contractErr(CodeContractBundle)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return nil, contractErr(CodeContractBundle)
	}
	if SHA256Hex(data) != "380c7f3db80baa2d288838f3a550c3588abd19de11627d34ae90f5d3a0add4fe" {
		return nil, contractErr("contract_file_digest_mismatch")
	}
	return nil, notImplementedErr()
}

func ValidateContractObject(_ *SchemaSet, _ string, input []byte) Decision {
	if _, err := decodeReachedJSON(input); err != nil {
		return Decision{Code: "contract_json_invalid"}
	}
	return notImplementedDecision()
}

func AdmissionPayloadDigest(certificate, signals, negativeCapabilities []byte) (string, error) {
	for _, input := range [][]byte{certificate, signals, negativeCapabilities} {
		if _, err := decodeReachedJSON(input); err != nil {
			return "", err
		}
	}
	return "", notImplementedErr()
}

func DecideBehaviorAdmission(certificate, context []byte) Decision {
	if _, err := decodeReachedJSON(certificate); err != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	if _, err := decodeReachedJSON(context); err != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	return notImplementedDecision()
}

func VerifyManifestAuthorityUpdate(input AuthorityInput) Decision { return reachedAuthority(input) }
func VerifyRootRotation(input AuthorityInput) Decision            { return reachedAuthority(input) }
func VerifyEmergencyRevocation(input AuthorityInput) Decision     { return reachedAuthority(input) }

func reachedAuthority(input AuthorityInput) Decision {
	if len(input.Candidate) == 0 {
		return Decision{Code: "authority_signature_invalid"}
	}
	return notImplementedDecision()
}

func TrustStateDigest(state []byte) (string, error) {
	if _, err := decodeReachedJSON(state); err != nil {
		return "", err
	}
	return "", notImplementedErr()
}

func DecideReadiness(handshake, expected []byte) Decision {
	return reachedPair(handshake, expected, "interface_not_ready")
}

func DecideLifecycle(state, operation []byte) Decision {
	return reachedPair(state, operation, "interface_state_transition_invalid")
}

func DecideTaskLineage(state, candidate []byte, _ int64) Decision {
	return reachedPair(state, candidate, "interface_lineage_mismatch")
}

func DecideOutcome(outcome []byte) Decision {
	if len(outcome) == 0 {
		return Decision{Code: "interface_terminal_no_retry"}
	}
	return notImplementedDecision()
}

func ExecuteReplay(state, command []byte) Decision {
	return reachedPair(state, command, "replay_rejected")
}

func reachedPair(left, right []byte, invalidCode string) Decision {
	if len(left) == 0 || len(right) == 0 {
		return Decision{Code: invalidCode}
	}
	return notImplementedDecision()
}

func ValidateSidecarEnvelope(envelope []byte, _ *SchemaSet) Decision {
	if len(envelope) == 0 {
		return Decision{Code: "sidecar_capability_decode_invalid"}
	}
	return notImplementedDecision()
}

func VerifySidecarCapability(envelope, capability, keyring []byte, _ int64) Decision {
	if len(envelope) == 0 || len(capability) == 0 || len(keyring) == 0 {
		return Decision{Code: "sidecar_capability_decode_invalid"}
	}
	return notImplementedDecision()
}

func ParseBoundedPointerIndex(segment string, length uint64, allowEnd bool) (uint64, error) {
	if segment == "" || segment == "-" || (len(segment) > 1 && segment[0] == '0') {
		return 0, contractErr(CodeMutationPointer)
	}
	var value uint64
	for i := 0; i < len(segment); i++ {
		if segment[i] < '0' || segment[i] > '9' {
			return 0, contractErr(CodeMutationPointer)
		}
		digit := uint64(segment[i] - '0')
		if value > (4096-digit)/10 {
			return 0, contractErr(CodeMutationPointer)
		}
		value = value*10 + digit
	}
	if value > 4096 || value > length || (!allowEnd && value == length) {
		return 0, contractErr(CodeMutationPointer)
	}
	return 0, notImplementedErr()
}

func ApplyMutation(source []byte, operation MutationOperation) ([]byte, error) {
	if len(source) == 0 || operation.Kind == "" {
		return nil, contractErr(CodeMutationSource)
	}
	return nil, notImplementedErr()
}

func ExecuteMutationCorpus(root string, corpus []byte, _ *SchemaSet) ([]MutationResult, error) {
	if root == "" {
		return nil, contractErr(CodeMutationSource)
	}
	if _, err := decodeReachedJSON(corpus); err != nil {
		return nil, contractErr("mutation_descriptor_invalid")
	}
	return nil, notImplementedErr()
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
	if predecessorPath == "" {
		return Decision{Code: "contract_predecessor_mismatch"}
	}
	for _, root := range []string{ccRoot, subRoot} {
		if err := inspectMirrorRoot(root); err != nil {
			if ce, ok := err.(*ContractError); ok {
				return Decision{Code: ce.Code}
			}
			return Decision{Code: CodeContractBundle}
		}
	}
	return notImplementedDecision()
}

func inspectMirrorRoot(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return contractErr(CodeContractBundle)
	}
	if len(entries) != len(mirrorDigests) {
		return contractErr("contract_file_set_invalid")
	}
	for _, entry := range entries {
		expected, ok := mirrorDigests[entry.Name()]
		if !ok || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return contractErr("contract_file_set_invalid")
		}
		data, readErr := os.ReadFile(filepath.Join(root, entry.Name()))
		if readErr != nil || SHA256Hex(data) != expected {
			return contractErr("contract_file_digest_mismatch")
		}
	}
	return nil
}

func ValidateContractIndex(bundleRoot string) Decision {
	path := filepath.Join(bundleRoot, "contract-index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Decision{Code: CodeContractBundle}
	}
	if SHA256Hex(data) != mirrorDigests["contract-index.json"] {
		return Decision{Code: "contract_index_not_canonical"}
	}
	return notImplementedDecision()
}

func ValidateCrossRepoRecord(input []byte) Decision {
	if _, err := decodeReachedJSON(input); err != nil {
		return Decision{Code: "contract_json_invalid"}
	}
	return notImplementedDecision()
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
