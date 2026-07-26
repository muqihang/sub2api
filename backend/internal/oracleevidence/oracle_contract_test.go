package oracleevidence

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const mirrorRoot = "testdata/oracle_lab_contract/v1"

const (
	receiptOutputEnv      = "ORACLE_CONTRACT_RECEIPT_OUTPUT"
	receiptRecordEnv      = "ORACLE_CONTRACT_RECEIPT_RECORD"
	receiptCCMirrorEnv    = "ORACLE_CONTRACT_RECEIPT_CC_MIRROR"
	receiptSubMirrorEnv   = "ORACLE_CONTRACT_RECEIPT_SUB_MIRROR"
	receiptPredecessorEnv = "ORACLE_CONTRACT_RECEIPT_PREDECESSOR"
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
		return fmt.Errorf("read record: %w", err)
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

	receipt := map[string]any{
		"schema_id":                 "oracle.sub_contract_receipt",
		"schema_major":              1,
		"schema_revision":           0,
		"bundle_sha256":             SHA256Hex(append(bundleBytes, '\n')),
		"decisions_sha256":          SHA256Hex(append(caseBytes, '\n')),
		"mutation_results_sha256":   SHA256Hex(append(mutationBytes, '\n')),
		"required_set_sha256":       requiredSetDigest,
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
		return fmt.Errorf("create receipt: %w", err)
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
		return fmt.Errorf("write receipt: %w", writeErr)
	}
	info, err := os.Lstat(values["output"])
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("receipt mode/type verification failed")
	}
	return nil
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
			return nil, fmt.Errorf("read mirror member: %w", err)
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
	digest, ok := stringValue(receipt["receipt_digest"])
	if !ok {
		t.Fatal("receipt digest missing")
	}
	unsigned := cloneMap(t, receipt)
	delete(unsigned, "receipt_digest")
	if digest != SHA256Hex(append(mustBytes(t, unsigned), '\n')) {
		t.Fatal("receipt digest mismatch")
	}
	if err := writeCrossRepoReceiptFromEnv(); err == nil {
		t.Fatal("receipt output overwrite was allowed")
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
