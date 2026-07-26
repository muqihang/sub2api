package oracleevidence

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const mirrorRoot = "testdata/oracle_lab_contract/v1"

const (
	receiptOutputEnv      = "ORACLE_CONTRACT_RECEIPT_OUTPUT"
	receiptRecordEnv      = "ORACLE_CONTRACT_RECEIPT_RECORD"
	receiptCCMirrorEnv    = "ORACLE_CONTRACT_RECEIPT_CC_MIRROR"
	receiptSubMirrorEnv   = "ORACLE_CONTRACT_RECEIPT_SUB_MIRROR"
	receiptPredecessorEnv = "ORACLE_CONTRACT_RECEIPT_PREDECESSOR"
	receiptChildEnv       = "ORACLE_CONTRACT_RECEIPT_CHILD"
	receiptTestSelector   = "^TestOracleContract(Scaffold|StrictJSON|JCS|Normalization|CBOR|Schema|Admission|ManifestAuthority|Interface|Replay|Sidecar|Mutation|CrossRepo)$"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 {
		if err := writeCrossRepoReceiptFromEnv(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "oracle receipt: %v\n", err)
			code = 1
		}
	}
	os.Exit(code)
}

func writeCrossRepoReceiptFromEnv() error {
	values := map[string]string{
		"output":      os.Getenv(receiptOutputEnv),
		"record":      os.Getenv(receiptRecordEnv),
		"cc_mirror":   os.Getenv(receiptCCMirrorEnv),
		"sub_mirror":  os.Getenv(receiptSubMirrorEnv),
		"predecessor": os.Getenv(receiptPredecessorEnv),
	}
	configured := false
	for _, value := range values {
		configured = configured || value != ""
	}
	if !configured {
		return nil
	}
	for name, value := range values {
		if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return fmt.Errorf("%s path is missing or non-canonical", name)
		}
	}

	recordBytes, err := readRegularFile(values["record"], maxJSONBytes)
	if err != nil {
		return errors.New("receipt_record_read_failed")
	}
	mirrorDecision := InspectMirror(values["cc_mirror"], values["sub_mirror"], values["predecessor"])
	indexDecision := ValidateContractIndex(values["sub_mirror"])
	recordDecision := ValidateCrossRepoRecord(recordBytes)
	if !mirrorDecision.Allowed || !indexDecision.Allowed || !recordDecision.Allowed {
		return fmt.Errorf("validation failed: mirror=%s index=%s record=%s", mirrorDecision.Code, indexDecision.Code, recordDecision.Code)
	}

	bundleRows, err := receiptBundleRows(values["sub_mirror"])
	if err != nil {
		return err
	}
	caseSpecs, mutationSpecs, ok := frozenResultSpecs()
	if !ok {
		return errors.New("frozen result specs unavailable")
	}
	requiredSetDigest, ok := frozenRequiredSetDigest(caseSpecs, mutationSpecs)
	if !ok {
		return errors.New("frozen required set unavailable")
	}
	caseBytes, err := CanonicalizeValue(rowsForSpecs(caseSpecs))
	if err != nil {
		return fmt.Errorf("canonicalize decisions: %w", err)
	}
	mutationBytes, err := CanonicalizeValue(rowsForSpecs(mutationSpecs))
	if err != nil {
		return fmt.Errorf("canonicalize mutations: %w", err)
	}
	bundleBytes, err := CanonicalizeValue(bundleRows)
	if err != nil {
		return fmt.Errorf("canonicalize bundle: %w", err)
	}

	decisionTrace, mutationTrace, requiredTrace, err := receiptExecutionTrace()
	if err != nil {
		return err
	}
	decisionTraceBytes, err := CanonicalizeValue(decisionTrace)
	if err != nil {
		return errors.New("receipt_decision_trace_invalid")
	}
	mutationTraceBytes, err := CanonicalizeValue(mutationTrace)
	if err != nil {
		return errors.New("receipt_mutation_trace_invalid")
	}
	requiredTraceBytes, err := CanonicalizeValue(requiredTrace)
	if err != nil {
		return errors.New("receipt_required_trace_invalid")
	}

	receipt := map[string]any{
		"schema_id":                 "oracle.sub_contract_receipt",
		"schema_major":              1,
		"schema_revision":           0,
		"bundle_sha256":             SHA256Hex(append(bundleBytes, '\n')),
		"decisions_sha256":          SHA256Hex(append(decisionTraceBytes, '\n')),
		"mutation_results_sha256":   SHA256Hex(append(mutationTraceBytes, '\n')),
		"required_set_sha256":       requiredSetDigest,
		"executed_required_sha256":  SHA256Hex(append(requiredTraceBytes, '\n')),
		"declared_decisions_sha256": SHA256Hex(append(caseBytes, '\n')),
		"declared_mutations_sha256": SHA256Hex(append(mutationBytes, '\n')),
		"stable_code_count":         len(StableCodes),
		"stable_code_set_sha256":    stableCodeDigest(),
		"record_input_sha256":       SHA256Hex(recordBytes),
		"mirror_validation_code":    mirrorDecision.Code,
		"index_validation_code":     indexDecision.Code,
		"record_validation_code":    recordDecision.Code,
		"mirror_validation_allowed": mirrorDecision.Allowed,
		"index_validation_allowed":  indexDecision.Allowed,
		"record_validation_allowed": recordDecision.Allowed,
	}
	unsigned, err := CanonicalizeValue(receipt)
	if err != nil {
		return fmt.Errorf("canonicalize unsigned receipt: %w", err)
	}
	receipt["receipt_digest"] = SHA256Hex(append(unsigned, '\n'))
	encoded, err := CanonicalizeValue(receipt)
	if err != nil {
		return fmt.Errorf("canonicalize receipt: %w", err)
	}
	encoded = append(encoded, '\n')

	output, err := os.OpenFile(values["output"], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("receipt_create_failed")
	}
	writeErr := error(nil)
	if _, err := output.Write(encoded); err != nil {
		writeErr = err
	} else if err := output.Sync(); err != nil {
		writeErr = err
	}
	if err := output.Close(); writeErr == nil {
		writeErr = err
	}
	if writeErr != nil {
		return errors.New("receipt_write_failed")
	}
	info, err := os.Lstat(values["output"])
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("receipt mode/type verification failed")
	}
	return nil
}

func receiptExecutionTrace() ([]any, []any, []any, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, nil, errors.New("receipt_test_binary_unavailable")
	}
	command := exec.Command(executable, "-test.run="+receiptTestSelector, "-test.v=true", "-test.count=1")
	environment := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		key := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			key = item[:index]
		}
		switch key {
		case receiptOutputEnv, receiptRecordEnv, receiptCCMirrorEnv, receiptSubMirrorEnv, receiptPredecessorEnv, receiptChildEnv:
			continue
		default:
			environment = append(environment, item)
		}
	}
	command.Env = append(environment, receiptChildEnv+"=1")
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, nil, nil, errors.New("receipt_execution_failed")
	}
	passed := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "--- PASS: ") {
			continue
		}
		name := strings.TrimPrefix(line, "--- PASS: ")
		if index := strings.Index(name, " ("); index >= 0 {
			name = name[:index]
		}
		if strings.HasPrefix(name, "TestOracleContract") {
			passed[name] = true
		}
	}
	required := []string{
		"TestOracleContractAdmission", "TestOracleContractCBOR", "TestOracleContractCrossRepo",
		"TestOracleContractInterface", "TestOracleContractJCS", "TestOracleContractManifestAuthority",
		"TestOracleContractMutation", "TestOracleContractNormalization", "TestOracleContractReplay",
		"TestOracleContractScaffold", "TestOracleContractSchema", "TestOracleContractSidecar",
		"TestOracleContractStrictJSON",
	}
	for _, name := range required {
		if !passed[name] {
			return nil, nil, nil, errors.New("receipt_execution_incomplete")
		}
	}
	names := make([]string, 0, len(passed))
	for name := range passed {
		names = append(names, name)
	}
	sort.Strings(names)
	decisions := make([]any, 0, len(names))
	mutations := make([]any, 0, 1)
	for _, name := range names {
		row := map[string]any{"test_id": name, "status": "pass"}
		if strings.HasPrefix(name, "TestOracleContractMutation") {
			mutations = append(mutations, row)
		} else {
			decisions = append(decisions, row)
		}
	}
	requiredRows := make([]any, len(required))
	for index, name := range required {
		requiredRows[index] = name
	}
	return decisions, mutations, requiredRows, nil
}

func receiptBundleRows(root string) ([]any, error) {
	names := make([]string, 0, len(mirrorDigests))
	for name := range mirrorDigests {
		names = append(names, name)
	}
	sortStrings(names)
	rows := make([]any, 0, len(names))
	for _, name := range names {
		data, err := readContractFile(filepath.Join(root, name), maxJSONBytes)
		if err != nil {
			return nil, errors.New("receipt_mirror_read_failed")
		}
		digest := SHA256Hex(data)
		if digest != mirrorDigests[name] {
			return nil, errors.New("mirror member digest mismatch")
		}
		rows = append(rows, map[string]any{"relative_path": name, "sha256": digest})
	}
	return rows, nil
}

func TestOracleContractReceipt(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(temp, "record.json")
	outputPath := filepath.Join(temp, "receipt.json")
	if err := os.WriteFile(recordPath, crossRecordBytes(t, validCrossRepoRecord(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(receiptOutputEnv, outputPath)
	t.Setenv(receiptRecordEnv, recordPath)
	t.Setenv(receiptCCMirrorEnv, filepath.Join(root, mirrorRoot))
	t.Setenv(receiptSubMirrorEnv, filepath.Join(root, mirrorRoot))
	t.Setenv(receiptPredecessorEnv, filepath.Join(root, "..", "service", "testdata", "cc_gateway_formal_pool_contract", "vectors.json"))
	if err := writeCrossRepoReceiptFromEnv(); err != nil {
		t.Fatal(err)
	}
	data, err := readRegularFile(outputPath, maxJSONBytes)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ParseStrictJSON(data[:len(data)-1])
	if err != nil {
		t.Fatal(err)
	}
	receipt, ok := objectValue(value)
	allowed, allowedOK := boolValue(receipt["record_validation_allowed"])
	if !ok || !stringEquals(receipt["schema_id"], "oracle.sub_contract_receipt") || !allowedOK || !allowed {
		t.Fatalf("invalid receipt: %#v", value)
	}
	for _, field := range []string{
		"bundle_sha256", "decisions_sha256", "mutation_results_sha256",
		"required_set_sha256", "executed_required_sha256",
		"declared_decisions_sha256", "declared_mutations_sha256",
	} {
		if digest, ok := stringValue(receipt[field]); !ok || len(digest) != 64 {
			t.Fatalf("receipt field %s is not a SHA-256 digest", field)
		}
	}
	digest, ok := stringValue(receipt["receipt_digest"])
	if !ok {
		t.Fatal("receipt digest missing")
	}
	unsigned := cloneMap(t, receipt)
	delete(unsigned, "receipt_digest")
	if digest != SHA256Hex(append(mustBytes(t, unsigned), '\n')) {
		t.Fatal("receipt digest mismatch")
	}
	overwriteErr := writeCrossRepoReceiptFromEnv()
	if overwriteErr == nil {
		t.Fatal("receipt output overwrite was allowed")
	}
	if strings.Contains(overwriteErr.Error(), temp) {
		t.Fatal("receipt overwrite error leaked an authority path")
	}

	t.Setenv(receiptOutputEnv, filepath.Join(temp, "unused-receipt.json"))
	t.Setenv(receiptRecordEnv, filepath.Join(temp, "missing-record.json"))
	missingRecordErr := writeCrossRepoReceiptFromEnv()
	if missingRecordErr == nil {
		t.Fatal("missing receipt input was allowed")
	}
	if strings.Contains(missingRecordErr.Error(), temp) {
		t.Fatal("receipt input error leaked an authority path")
	}
}

func requireCode(t *testing.T, err error, want string) {
	t.Helper()
	var contract *ContractError
	if !errors.As(err, &contract) || contract.Code != want {
		t.Fatalf("code = %v, want %s", err, want)
	}
}

func requireAllowed(t *testing.T, decision Decision, wantCode string) {
	t.Helper()
	if !decision.Allowed || decision.Code != wantCode {
		t.Fatalf("valid control rejected: allowed=%v code=%q, want allowed with code %q", decision.Allowed, decision.Code, wantCode)
	}
}
