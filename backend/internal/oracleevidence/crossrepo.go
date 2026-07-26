package oracleevidence

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	contractIndexSHA256 = "2545113fb928131ee5a735541b5373a00566b279263aca5b1cc11181aaf78bce"
	predecessorSHA256   = "70c26db06e9135db31d08f097573e3fd55bd9a8894614832eefeecabf6b1a3d1"
	stableCodesSHA256   = "f6f89d48519aaa46b362a474cc6bd8e470b638e1c7f4c3c0a7ac99413a85fa5c"
	crossRepoLeaseMS    = int64(86_400_000)
)

var indexFileOrder = []string{
	"authority-corpus.json",
	"canonicalization-corpus.json",
	"coherence-corpus.json",
	"contract.schema.json",
	"expected-results.json",
	"interface-corpus.json",
	"sidecar-envelope.cddl",
	"sidecar-envelope.schema.json",
}

//go:embed testdata/oracle_lab_contract/v1/canonicalization-corpus.json
var embeddedCanonicalCorpus []byte

//go:embed testdata/oracle_lab_contract/v1/coherence-corpus.json
var embeddedCrossCoherenceCorpus []byte

//go:embed testdata/oracle_lab_contract/v1/authority-corpus.json
var embeddedAuthorityCorpus []byte

//go:embed testdata/oracle_lab_contract/v1/interface-corpus.json
var embeddedInterfaceCorpus []byte

//go:embed testdata/oracle_lab_contract/v1/expected-results.json
var embeddedExpectedResults []byte

//go:embed testdata/oracle_lab_contract/v1/contract-index.json
var embeddedContractIndex []byte

//go:embed testdata/rebaseline/v1/mutation-corpus.json
var embeddedMutationCorpus []byte

type frozenDecisionSpec struct {
	id           string
	allowed      bool
	code         string
	nextDigest   any
	canonicalHex any
}

func inspectMirrorImpl(ccRoot, subRoot, predecessorPath string) Decision {
	if predecessorPath == "" {
		return Decision{Code: "contract_predecessor_mismatch"}
	}
	for _, root := range []string{ccRoot, subRoot} {
		if err := inspectMirrorRoot(root); err != nil {
			return decisionFromError(err, CodeContractBundle)
		}
		if decision := validateContractIndexImpl(root); !decision.Allowed {
			return decision
		}
	}
	for name := range mirrorDigests {
		left, leftErr := readContractFile(filepath.Join(ccRoot, name), maxJSONBytes)
		right, rightErr := readContractFile(filepath.Join(subRoot, name), maxJSONBytes)
		if leftErr != nil || rightErr != nil || !bytes.Equal(left, right) {
			return Decision{Code: "contract_mirror_mismatch"}
		}
		if containsLeakBytes(left) || containsLeakBytes(right) {
			return Decision{Code: "leak_detected", Detail: name}
		}
	}
	predecessor, err := readContractFile(predecessorPath, maxJSONBytes)
	if err != nil || sha256HexImpl(predecessor) != predecessorSHA256 {
		return Decision{Code: "contract_predecessor_mismatch"}
	}
	return Decision{Allowed: true}
}

func readContractFile(path string, maximum int64) ([]byte, error) {
	file, openErr := openNoFollow(path)
	if openErr != nil {
		return nil, contractErr("contract_symlink")
	}
	defer file.Close()
	opened, statErr := file.Stat()
	if statErr != nil || !stableRegularFile(opened, uint64(maximum), true) {
		return nil, contractErr("contract_file_digest_mismatch")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maximum+1))
	if readErr != nil || int64(len(data)) > maximum {
		return nil, contractErr("contract_file_digest_mismatch")
	}
	after, statErr := file.Stat()
	reopened, reopenErr := openNoFollow(path)
	if reopenErr != nil {
		return nil, contractErr("contract_file_digest_mismatch")
	}
	pathAfter, pathErr := reopened.Stat()
	reopened.Close()
	if statErr != nil || pathErr != nil || !sameStableFile(opened, after) || !sameStableFile(after, pathAfter) {
		return nil, contractErr("contract_file_digest_mismatch")
	}
	return data, nil
}

func openNoFollow(path string) (*os.File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	volume := filepath.VolumeName(absolute)
	root := volume + string(os.PathSeparator)
	segments := strings.Split(strings.TrimPrefix(absolute, root), string(os.PathSeparator))
	if len(segments) == 0 || segments[len(segments)-1] == "" {
		return nil, contractErr("contract_index_path_invalid")
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	for _, segment := range segments[:len(segments)-1] {
		if segment == "" || segment == "." || segment == ".." {
			directory.Close()
			return nil, contractErr("contract_index_path_invalid")
		}
		before, statErr := directory.Lstat(segment)
		if statErr != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			directory.Close()
			return nil, contractErr("contract_symlink")
		}
		next, openErr := directory.OpenRoot(segment)
		if openErr != nil {
			directory.Close()
			return nil, openErr
		}
		after, afterErr := next.Stat(".")
		directory.Close()
		if afterErr != nil || !sameFileIdentity(before, after) {
			next.Close()
			return nil, contractErr("contract_file_digest_mismatch")
		}
		directory = next
	}
	leaf := segments[len(segments)-1]
	before, statErr := directory.Lstat(leaf)
	if statErr != nil || before.Mode()&os.ModeSymlink != 0 {
		directory.Close()
		return nil, contractErr("contract_symlink")
	}
	file, openErr := directory.Open(leaf)
	directory.Close()
	if openErr != nil {
		return nil, openErr
	}
	opened, openedErr := file.Stat()
	if openedErr != nil || !sameFileIdentity(before, opened) {
		file.Close()
		return nil, contractErr("contract_file_digest_mismatch")
	}
	return file, nil
}

func stableRegularFile(info os.FileInfo, maximum uint64, requireNonempty bool) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o111 != 0 || info.Size() < 0 || uint64(info.Size()) > maximum || (requireNonempty && info.Size() == 0) {
		return false
	}
	links, ok := statUintField(info, "Nlink")
	return ok && links == 1
}

func sameStableFile(left, right os.FileInfo) bool {
	if !sameFileIdentity(left, right) {
		return false
	}
	leftLinks, leftOK := statUintField(left, "Nlink")
	rightLinks, rightOK := statUintField(right, "Nlink")
	return leftOK && rightOK && leftLinks == 1 && rightLinks == 1
}

func sameFileIdentity(left, right os.FileInfo) bool {
	if left == nil || right == nil || !os.SameFile(left, right) || left.Mode() != right.Mode() || left.Size() != right.Size() || !left.ModTime().Equal(right.ModTime()) {
		return false
	}
	leftDev, leftDevOK := statUintField(left, "Dev")
	rightDev, rightDevOK := statUintField(right, "Dev")
	leftInode, leftInodeOK := statUintField(left, "Ino")
	rightInode, rightInodeOK := statUintField(right, "Ino")
	leftSeconds, leftNanos, leftTimeOK := statChangeTime(left)
	rightSeconds, rightNanos, rightTimeOK := statChangeTime(right)
	return leftDevOK && rightDevOK && leftInodeOK && rightInodeOK && leftTimeOK && rightTimeOK &&
		leftDev == rightDev && leftInode == rightInode && leftSeconds == rightSeconds && leftNanos == rightNanos
}

func statValue(info os.FileInfo) (reflect.Value, bool) {
	if info == nil || info.Sys() == nil {
		return reflect.Value{}, false
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	return value, value.Kind() == reflect.Struct
}

func statUintField(info os.FileInfo, name string) (uint64, bool) {
	value, ok := statValue(info)
	if !ok {
		return 0, false
	}
	field := value.FieldByName(name)
	if !field.IsValid() {
		return 0, false
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return field.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		integer := field.Int()
		return uint64(integer), integer >= 0
	default:
		return 0, false
	}
}

func statChangeTime(info os.FileInfo) (int64, int64, bool) {
	value, ok := statValue(info)
	if !ok {
		return 0, 0, false
	}
	for _, name := range []string{"Ctimespec", "Ctim"} {
		field := value.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.Struct {
			seconds := field.FieldByName("Sec")
			nanos := field.FieldByName("Nsec")
			if seconds.IsValid() && nanos.IsValid() && seconds.CanInt() && nanos.CanInt() {
				return seconds.Int(), nanos.Int(), true
			}
		}
	}
	return 0, 0, false
}

func rejectSymlinkComponents(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return contractErr("contract_index_path_invalid")
	}
	volume := filepath.VolumeName(absolute)
	current := volume + string(os.PathSeparator)
	for _, segment := range strings.Split(strings.TrimPrefix(absolute, current), string(os.PathSeparator)) {
		if segment == "" {
			continue
		}
		current = filepath.Join(current, segment)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return contractErr(CodeContractBundle)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return contractErr("contract_symlink")
		}
	}
	return nil
}

func validateContractIndexImpl(bundleRoot string) Decision {
	data, err := readContractFile(filepath.Join(bundleRoot, "contract-index.json"), maxJSONBytes)
	if err != nil {
		return decisionFromError(err, CodeContractBundle)
	}
	canonical, err := canonicalizeJSONImpl(data)
	if err != nil || !bytes.Equal(canonical, data) {
		return Decision{Code: "contract_index_not_canonical"}
	}
	value, err := parseStrictJSONImpl(data)
	if err != nil {
		return Decision{Code: "contract_json_invalid"}
	}
	index, ok := objectValue(value)
	if !ok || !exactKeys(index, "bundle_id", "compatibility", "files", "predecessor", "schema_id", "schema_major", "schema_revision") {
		return Decision{Code: "contract_required_set_mismatch"}
	}
	schemaID, _ := stringValue(index["schema_id"])
	major, majorOK := int64Value(index["schema_major"])
	revision, revisionOK := int64Value(index["schema_revision"])
	bundleID, _ := stringValue(index["bundle_id"])
	if schemaID != "oracle.compatibility" || !majorOK || major != 1 || !revisionOK || revision != 0 || bundleID != "oracle.compatibility.v1" {
		return Decision{Code: "contract_index_version_invalid"}
	}
	compatibility, ok := arrayValue(index["compatibility"])
	if !ok || len(compatibility) != 1 {
		return Decision{Code: "contract_schema_range_mismatch"}
	}
	rangeRow, ok := objectValue(compatibility[0])
	if !ok || !exactKeys(rangeRow, "maximum_revision", "minimum_revision", "schema_major") || !intEquals(rangeRow["schema_major"], 1) || !intEquals(rangeRow["minimum_revision"], 0) || !intEquals(rangeRow["maximum_revision"], 0) {
		return Decision{Code: "contract_schema_range_mismatch"}
	}
	files, ok := arrayValue(index["files"])
	if !ok || len(files) != len(indexFileOrder) {
		return Decision{Code: "contract_required_set_mismatch"}
	}
	for position, expectedPath := range indexFileOrder {
		binding, ok := objectValue(files[position])
		if !ok || !exactKeys(binding, "relative_path", "sha256") {
			return Decision{Code: "contract_required_set_mismatch"}
		}
		relativePath, pathOK := stringValue(binding["relative_path"])
		digest, digestOK := stringValue(binding["sha256"])
		if !pathOK || relativePath != expectedPath || !safeRelativePath(relativePath) {
			return Decision{Code: "contract_index_path_invalid"}
		}
		if !digestOK || digest != mirrorDigests[relativePath] {
			return Decision{Code: "contract_file_digest_mismatch"}
		}
	}
	predecessor, ok := objectValue(index["predecessor"])
	if !ok || !exactKeys(predecessor, "path", "repository", "sha256") {
		return Decision{Code: "contract_predecessor_mismatch"}
	}
	path, _ := stringValue(predecessor["path"])
	repository, _ := stringValue(predecessor["repository"])
	digest, _ := stringValue(predecessor["sha256"])
	if path != "backend/internal/service/testdata/cc_gateway_formal_pool_contract/vectors.json" || repository != "sub2api" || digest != predecessorSHA256 {
		return Decision{Code: "contract_predecessor_mismatch"}
	}
	if sha256HexImpl(data) != contractIndexSHA256 {
		return Decision{Code: "contract_index_not_canonical"}
	}
	return Decision{Allowed: true}
}

func validateCrossRepoRecordImpl(input []byte) Decision {
	if len(input) < 2 || input[len(input)-1] != '\n' || input[len(input)-2] == '\n' || input[len(input)-2] == '\r' || input[len(input)-2] == ' ' || input[len(input)-2] == '\t' {
		return Decision{Code: "cross_repo_binding_mismatch"}
	}
	core := input[:len(input)-1]
	value, err := parseStrictJSONImpl(core)
	if err != nil {
		return Decision{Code: "contract_json_invalid"}
	}
	canonicalRecord, err := canonicalizeValueImpl(value)
	if err != nil || !bytes.Equal(core, canonicalRecord) {
		return Decision{Code: "cross_repo_binding_mismatch"}
	}
	record, ok := objectValue(value)
	if !ok {
		return Decision{Code: "cross_repo_binding_mismatch"}
	}
	if containsDiagnosticPromotion(record) {
		return Decision{Code: "authority_diagnostic_promotion"}
	}
	if containsLeak(record) || containsLeakString(string(core)) {
		return Decision{Code: "leak_detected"}
	}
	if !exactKeys(record, "schema_id", "schema_major", "schema_revision", "kind", "authority", "bundle", "commit_dag", "result", "review", "issued_at_ms", "expires_at_ms", "record_digest") {
		return Decision{Code: "cross_repo_binding_mismatch"}
	}
	schemaID, _ := stringValue(record["schema_id"])
	kind, _ := stringValue(record["kind"])
	if schemaID != "oracle.cross_repo_record" || kind != "oracle_contract_rebaseline" || !intEquals(record["schema_major"], 1) || !intEquals(record["schema_revision"], 0) {
		return Decision{Code: "cross_repo_binding_mismatch"}
	}
	issued, issuedOK := int64Value(record["issued_at_ms"])
	expires, expiresOK := int64Value(record["expires_at_ms"])
	if !issuedOK || !expiresOK || issued < 0 || expires != issued+crossRepoLeaseMS {
		return Decision{Code: "cross_repo_binding_mismatch"}
	}
	if time.Now().UnixMilli() > expires {
		return Decision{Code: "cross_repo_record_expired"}
	}
	if !validCrossAuthority(record["authority"]) || !validCrossBundle(record["bundle"]) || !validCommitDAG(record["commit_dag"], record["authority"]) || !validCrossReview(record["review"]) {
		return Decision{Code: "cross_repo_binding_mismatch"}
	}
	if !validCrossResult(record["result"]) {
		return Decision{Code: "cross_repo_result_mismatch"}
	}
	digest, digestOK := stringValue(record["record_digest"])
	unsigned := cloneObject(record)
	delete(unsigned, "record_digest")
	canonical, canonicalErr := canonicalizeValueImpl(unsigned)
	canonical = append(canonical, '\n')
	if !digestOK || canonicalErr != nil || digest != sha256HexImpl(canonical) {
		return Decision{Code: "cross_repo_binding_mismatch"}
	}
	return Decision{Allowed: true}
}

var diagnosticKeys = map[string]bool{
	"absolute_worktree_path": true, "db_mtime": true, "db_size_bytes": true, "divergence": true,
	"edge_count": true, "file_count": true, "full_remote_config_digest": true, "last_indexed": true,
	"node_count": true, "remote_projection_digest": true, "worktree_directory_mtime": true,
}

func containsDiagnosticPromotion(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if diagnosticKeys[key] || containsDiagnosticPromotion(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsDiagnosticPromotion(child) {
				return true
			}
		}
	}
	return false
}

var leakKeys = map[string]bool{
	"authorization": true, "proxy_authorization": true, "x_api_key": true, "api_key": true,
	"anthropic_api_key": true, "access_token": true, "refresh_token": true, "password": true,
	"cookie": true, "set_cookie": true, "prompt": true, "body": true, "request_body": true,
	"response_body": true, "raw": true, "raw_bytes": true, "raw_material": true, "client_hello": true,
	"cch": true, "credential": true, "credentials": true, "secret": true, "private_key": true,
	"session_id": true, "conversation_id": true, "message_id": true,
}

func containsLeak(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			if (leakKeys[normalized] && containsSensitiveValue(child)) || containsLeak(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsLeak(child) {
				return true
			}
		}
	case string:
		return containsLeakString(typed)
	}
	return false
}

func containsSensitiveValue(value any) bool {
	members := 0
	return containsSensitiveValueAt(value, 0, &members)
}

func containsSensitiveValueAt(value any, depth int, members *int) bool {
	if depth > maxJSONDepth {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		*members += len(typed)
		if *members > maxJSONMembers {
			return true
		}
		for _, child := range typed {
			if containsSensitiveValueAt(child, depth+1, members) {
				return true
			}
		}
		return false
	case map[string]any:
		*members += len(typed)
		if *members > maxJSONMembers {
			return true
		}
		for _, child := range typed {
			if containsSensitiveValueAt(child, depth+1, members) {
				return true
			}
		}
		return false
	case nil:
		return false
	default:
		return true
	}
}

func containsLeakBytes(input []byte) bool {
	if value, err := parseStrictJSONImpl(input); err == nil && containsLeak(value) {
		return true
	}
	return containsLeakString(string(input))
}

func containsLeakString(value string) bool {
	lower := strings.ToLower(value)
	if strings.Contains(value, "/Users/") || strings.Contains(value, "/home/") || strings.Contains(value, "/var/folders/") || strings.HasPrefix(value, `\\`) ||
		(len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')) ||
		strings.Contains(lower, "bearer ") || strings.Contains(lower, "basic ") || containsPEMPrivateKeyOpener(value) {
		return true
	}
	for _, key := range []string{"authorization", "proxy_authorization", "x_api_key", "api_key", "anthropic_api_key", "access_token", "refresh_token", "password", "cookie", "set_cookie", "credential", "credentials", "secret", "private_key"} {
		if containsCredentialAssignment(lower, key) || containsCredentialAssignment(lower, strings.ReplaceAll(key, "_", "-")) {
			return true
		}
	}
	return false
}

func containsCredentialAssignment(value, key string) bool {
	for offset := 0; offset < len(value); {
		index := strings.Index(value[offset:], key)
		if index < 0 {
			return false
		}
		index += offset
		beforeOK := index == 0 || !isCredentialWordByte(value[index-1])
		position := index + len(key)
		for position < len(value) && (value[position] == ' ' || value[position] == '\t') {
			position++
		}
		separatorOK := position < len(value) && value[position] == '='
		if position < len(value) && value[position] == ':' {
			separatorOK = true
		}
		if beforeOK && separatorOK {
			position++
			for position < len(value) && (value[position] == ' ' || value[position] == '\t') {
				position++
			}
			if position < len(value) && value[position] != '\r' && value[position] != '\n' {
				return true
			}
		}
		offset = index + len(key)
	}
	return false
}

func isCredentialWordByte(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9') || value == '_'
}

func containsPEMPrivateKeyOpener(value string) bool {
	value = strings.ToUpper(value)
	for _, opener := range []string{"-----BEGIN PRIVATE KEY-----", "-----BEGIN ENCRYPTED PRIVATE KEY-----", "-----BEGIN RSA PRIVATE KEY-----", "-----BEGIN EC PRIVATE KEY-----", "-----BEGIN DSA PRIVATE KEY-----", "-----BEGIN OPENSSH PRIVATE KEY-----"} {
		if strings.Contains(value, opener) {
			return true
		}
	}
	return false
}

func validCrossAuthority(value any) bool {
	authority, ok := objectValue(value)
	if !ok || !exactKeys(authority, "cc", "sub", "command_id", "reviewer_model") || !safeRef(authority["command_id"]) || !stringEquals(authority["reviewer_model"], "gpt-5.6-sol") {
		return false
	}
	cc, ok := objectValue(authority["cc"])
	if !ok || !exactKeys(cc, "repository_url", "selected_remote_name", "selected_remote_ref", "selected_remote_oid", "commit", "tree", "amendment_sha256") {
		return false
	}
	if !stringEquals(cc["repository_url"], "https://github.com/muqihang/cc-gateway.git") || !stringEquals(cc["selected_remote_name"], "muqihang") || !stringEquals(cc["selected_remote_ref"], "refs/remotes/muqihang/main") || !validOID(cc["selected_remote_oid"]) || !validOID(cc["commit"]) || !validOID(cc["tree"]) || !validDigest(cc["amendment_sha256"]) {
		return false
	}
	if !stringEquals(cc["selected_remote_oid"], "debe0360384132d6e66c0296219ea6066193e187") || !stringEquals(cc["commit"], "debe0360384132d6e66c0296219ea6066193e187") || !stringEquals(cc["tree"], "ffca2a0a892b2292b486533089f3276f28b39d4e") || !stringEquals(cc["amendment_sha256"], "eeaefeddbfe740003288f9d8ec8ba4673b57cca91f4fc3bf5cea5db02feaefaf") {
		return false
	}
	sub, ok := objectValue(authority["sub"])
	if !ok || !exactKeys(sub, "repository_url", "selected_local_ref", "selected_local_oid", "commit", "tree", "parent", "ancestor", "go_mod_sha256", "go_sum_sha256", "go_directive", "predecessor_relative_path", "predecessor_sha256", "codegraph_config_sha256", "codegraph_version", "codegraph_extraction_revision", "selection", "sub_plan_commit", "sub_plan_tree", "sub_plan_sha256", "r1_commit", "r1_tree", "i1_commit", "i1_tree") {
		return false
	}
	fixed := map[string]string{
		"repository_url": "https://github.com/Wei-Shaw/sub2api.git", "selected_local_ref": "refs/heads/codex/native-search-gateway",
		"selected_local_oid": "3ac410ea02edc53c3925f28eddcbc22b51c0a137", "commit": "3ac410ea02edc53c3925f28eddcbc22b51c0a137",
		"tree": "f7d51fb57c64fbaf6e2db3a7a2d423a491d5788d", "parent": "04e42ae0f6c556daad21ac393eb284585092e805",
		"ancestor": "fc0b1989d7ba9ce06ff151b17c94b50df4170a93", "go_mod_sha256": "e637999a38f974c9172c8f69c8fbb9c0d727bacf257558307e97e927cbb468de",
		"go_sum_sha256": "d3e1fd1510b41f218136b719fdf2c4ef239b05650d3b575fb93c18f25f3dc981", "go_directive": "1.26.5",
		"predecessor_relative_path": "backend/internal/service/testdata/cc_gateway_formal_pool_contract/vectors.json", "predecessor_sha256": predecessorSHA256,
		"codegraph_config_sha256": "a7f3ad7c17d655f9d2494b5b05e55ceb4ea9c7667456ff785c5f2a9291c3783a", "codegraph_version": "1.1.6",
		"r1_commit": "795c1f810b5647840fec508951cfc3272066d8b6", "r1_tree": "efb99a079e76817a38a9a48b053cdc6504e37025",
	}
	for field, expected := range fixed {
		if !stringEquals(sub[field], expected) {
			return false
		}
	}
	if !intEquals(sub["codegraph_extraction_revision"], 24) || !validOID(sub["sub_plan_commit"]) || !validOID(sub["sub_plan_tree"]) || !validDigest(sub["sub_plan_sha256"]) || !validOID(sub["i1_commit"]) || !validOID(sub["i1_tree"]) {
		return false
	}
	return validSubSelection(sub["selection"])
}

func validSubSelection(value any) bool {
	selection, ok := objectValue(value)
	if !ok {
		return false
	}
	mode, _ := stringValue(selection["mode"])
	switch mode {
	case "remote_ref":
		if !exactKeys(selection, "mode", "selected_remote_name", "selected_remote_url", "selected_remote_ref", "selected_remote_oid") || !safeRef(selection["selected_remote_name"]) || !safeRef(selection["selected_remote_ref"]) || !stringEquals(selection["selected_remote_oid"], "3ac410ea02edc53c3925f28eddcbc22b51c0a137") {
			return false
		}
		url, ok := stringValue(selection["selected_remote_url"])
		return ok && strings.HasPrefix(url, "https://") && len(url) <= 2_048 && !strings.ContainsAny(url, "\x00\r\n")
	case "total_controller_local_override":
		return exactKeys(selection, "mode", "selection_override_sha256", "selection_override_controller_id", "selection_override_task_id", "selection_override_issued_at_ms", "selection_override_decision") &&
			validDigest(selection["selection_override_sha256"]) && safeRef(selection["selection_override_controller_id"]) && safeRef(selection["selection_override_task_id"]) && generation(selection["selection_override_issued_at_ms"]) &&
			stringEquals(selection["selection_override_decision"], "authorize_refs/heads/codex/native-search-gateway_at_3ac410ea02edc53c3925f28eddcbc22b51c0a137")
	default:
		return false
	}
}

func validCrossBundle(value any) bool {
	bundle, ok := objectValue(value)
	if !ok || !exactKeys(bundle, "files", "contract_index_sha256", "predecessor_sha256", "schema_range", "mirror_root", "framing") || !stringEquals(bundle["contract_index_sha256"], contractIndexSHA256) || !stringEquals(bundle["predecessor_sha256"], predecessorSHA256) || !stringEquals(bundle["schema_range"], "1:0-0") || !stringEquals(bundle["mirror_root"], "backend/internal/oracleevidence/testdata/oracle_lab_contract/v1") || !stringEquals(bundle["framing"], "core-raw-exact;record-jcs-final-lf") {
		return false
	}
	files, ok := arrayValue(bundle["files"])
	if !ok || len(files) != len(mirrorDigests) {
		return false
	}
	names := make([]string, 0, len(files))
	seen := make(map[string]bool)
	for _, raw := range files {
		binding, ok := objectValue(raw)
		if !ok || !exactKeys(binding, "relative_path", "sha256") {
			return false
		}
		path, pathOK := stringValue(binding["relative_path"])
		digest, digestOK := stringValue(binding["sha256"])
		if !pathOK || !digestOK || seen[path] || mirrorDigests[path] != digest {
			return false
		}
		seen[path] = true
		names = append(names, path)
	}
	expected := make([]string, 0, len(mirrorDigests))
	for name := range mirrorDigests {
		expected = append(expected, name)
	}
	sort.Strings(expected)
	return equalStrings(names, expected)
}

func validCrossResult(value any) bool {
	result, ok := objectValue(value)
	if !ok || !exactKeys(result, "case_rows", "mutation_rows", "decisions_sha256", "mutation_results_sha256", "required_set_sha256", "stable_code_count", "stable_code_set_sha256", "semantic_surfaces", "protected_file_count", "protected_node_count", "egress_count", "command_ids") {
		return false
	}
	if !intEquals(result["stable_code_count"], int64(len(StableCodes))) || !stringEquals(result["stable_code_set_sha256"], stableCodesSHA256) || !intEquals(result["protected_file_count"], 0) || !intEquals(result["protected_node_count"], 0) || !intEquals(result["egress_count"], 0) || !allDigestFields(result, "decisions_sha256", "mutation_results_sha256", "required_set_sha256") {
		return false
	}
	surfaces, ok := objectValue(result["semantic_surfaces"])
	expectedSurfaces := []string{"strict_json", "jcs", "normalization", "cbor", "schema", "admission", "authority", "interface", "replay", "sidecar"}
	if !ok || !exactKeys(surfaces, expectedSurfaces...) {
		return false
	}
	for _, name := range expectedSurfaces {
		if allowed, ok := boolValue(surfaces[name]); !ok || !allowed {
			return false
		}
	}
	commands, ok := stringArray(result["command_ids"], 8)
	if !ok || !equalStrings(commands, []string{"cc-focused-contract-suite-v1", "sub-focused-oracleevidence-v1"}) {
		return false
	}
	caseRows, caseOK := arrayValue(result["case_rows"])
	mutationRows, mutationOK := arrayValue(result["mutation_rows"])
	caseSpecs, mutationSpecs, specsOK := frozenResultSpecs()
	if !caseOK || !mutationOK || !specsOK || !validRowsAgainstSpecs(caseRows, caseSpecs) || !validRowsAgainstSpecs(mutationRows, mutationSpecs) {
		return false
	}
	requiredDigest, digestOK := frozenRequiredSetDigest(caseSpecs, mutationSpecs)
	if !digestOK || !stringEquals(result["required_set_sha256"], requiredDigest) {
		return false
	}
	caseCanonical, _ := canonicalizeValueImpl(caseRows)
	mutationCanonical, _ := canonicalizeValueImpl(mutationRows)
	return stringEquals(result["decisions_sha256"], sha256HexImpl(append(caseCanonical, '\n'))) && stringEquals(result["mutation_results_sha256"], sha256HexImpl(append(mutationCanonical, '\n')))
}

func validRowsAgainstSpecs(rows []any, specs []frozenDecisionSpec) bool {
	if len(rows) != len(specs) {
		return false
	}
	codes := make(map[string]bool, len(StableCodes))
	for _, code := range StableCodes {
		codes[code] = true
	}
	seen := make(map[string]bool, len(rows))
	for index, raw := range rows {
		row, ok := objectValue(raw)
		if !ok || !exactKeys(row, "case_id", "allowed", "code", "next_state_digest", "canonical_hex") || !safeRef(row["case_id"]) {
			return false
		}
		caseID, _ := stringValue(row["case_id"])
		allowed, allowedOK := boolValue(row["allowed"])
		code, codeOK := stringValue(row["code"])
		spec := specs[index]
		if !allowedOK || !codeOK || seen[caseID] || caseID != spec.id || allowed != spec.allowed || code != spec.code || !codes[code] {
			return false
		}
		seen[caseID] = true
		if !jsonValuesEqual(row["next_state_digest"], spec.nextDigest) || !jsonValuesEqual(row["canonical_hex"], spec.canonicalHex) || !nullableDigest(row["next_state_digest"]) || !nullableHex(row["canonical_hex"]) {
			return false
		}
	}
	return true
}

func frozenResultSpecs() ([]frozenDecisionSpec, []frozenDecisionSpec, bool) {
	canonical, canonicalOK := embeddedObject(embeddedCanonicalCorpus)
	coherence, coherenceOK := embeddedObject(embeddedCrossCoherenceCorpus)
	authority, authorityOK := embeddedObject(embeddedAuthorityCorpus)
	interfaces, interfaceOK := embeddedObject(embeddedInterfaceCorpus)
	expected, expectedOK := embeddedObject(embeddedExpectedResults)
	mutations, mutationOK := embeddedObject(embeddedMutationCorpus)
	if !canonicalOK || !coherenceOK || !authorityOK || !interfaceOK || !expectedOK || !mutationOK {
		return nil, nil, false
	}
	caseSpecs := make([]frozenDecisionSpec, 0, 69)
	for _, raw := range mustArrayValue(canonical["json_cases"]) {
		row, ok := objectValue(raw)
		if !ok {
			return nil, nil, false
		}
		id, _ := stringValue(row["id"])
		valid, _ := boolValue(row["valid"])
		code := "authority_allow"
		var canonicalHex any
		if valid {
			var input []byte
			if inputHex, ok := stringValue(row["input_hex"]); ok {
				input, _ = hex.DecodeString(inputHex)
			} else {
				inputText, _ := stringValue(row["input_json"])
				input = []byte(inputText)
			}
			encoded, err := canonicalizeJSONImpl(input)
			if err != nil {
				return nil, nil, false
			}
			canonicalHex = hex.EncodeToString(encoded)
		} else {
			code, _ = stringValue(row["expected_code"])
		}
		caseSpecs = append(caseSpecs, frozenDecisionSpec{id: id, allowed: valid, code: code, canonicalHex: canonicalHex})
	}
	for _, raw := range mustArrayValue(canonical["cbor_cases"]) {
		row, ok := objectValue(raw)
		if !ok {
			return nil, nil, false
		}
		id, _ := stringValue(row["id"])
		valid, _ := boolValue(row["valid"])
		code := "authority_allow"
		var canonicalHex any
		if valid {
			canonicalHex, _ = stringValue(row["expected_hex"])
		} else {
			code, _ = stringValue(row["expected_code"])
		}
		caseSpecs = append(caseSpecs, frozenDecisionSpec{id: id, allowed: valid, code: code, canonicalHex: canonicalHex})
	}
	for _, raw := range mustArrayValue(canonical["normalization_cases"]) {
		row, _ := objectValue(raw)
		id, _ := stringValue(row["id"])
		caseSpecs = append(caseSpecs, frozenDecisionSpec{id: id, allowed: true, code: "authority_allow"})
	}
	appendExpectedCases := func(rows []any, digestMap map[string]any) bool {
		for _, raw := range rows {
			row, ok := objectValue(raw)
			if !ok {
				return false
			}
			id, idOK := stringValue(row["id"])
			code, codeOK := stringValue(row["expected_code"])
			if !idOK || !codeOK {
				return false
			}
			allowed := code == "admission_allow" || code == "authority_allow" || code == "interface_allow" || code == "interface_terminal_no_retry" || code == "interface_sub2api_retry" || code == "interface_gateway_retry" || code == "replay_reserved" || code == "replay_committed" || code == "replay_expired" || code == "replay_revoked"
			var next any
			if digest, ok := stringValue(digestMap[id]); ok {
				next = digest
			}
			caseSpecs = append(caseSpecs, frozenDecisionSpec{id: id, allowed: allowed, code: code, nextDigest: next})
		}
		return true
	}
	if !appendExpectedCases(mustArrayValue(coherence["cases"]), nil) || !appendExpectedCases(mustArrayValue(authority["cases"]), mustObject(authority["expected_next_state_digests"])) {
		return nil, nil, false
	}
	interfaceDigests := cloneObject(mustObject(interfaces["expected_state_digests"]))
	replayDigests := mustObject(expected["replay_state_digests"])
	interfaceDigests["replay-reserve"] = replayDigests["reserved"]
	interfaceDigests["replay-commit"] = replayDigests["committed"]
	if !appendExpectedCases(mustArrayValue(interfaces["cases"]), interfaceDigests) {
		return nil, nil, false
	}
	sidecarCanonical := mustObject(mustObject(expected["canonical_results"])["sidecar_unsigned_envelope"])
	caseSpecs = append(caseSpecs, frozenDecisionSpec{id: "sidecar_unsigned_envelope", allowed: true, code: "sidecar_capability_allow", canonicalHex: sidecarCanonical["canonical_hex"]})
	mutationSpecs := make([]frozenDecisionSpec, 0)
	for _, raw := range mustArrayValue(mutations["cases"]) {
		row, ok := objectValue(raw)
		if !ok {
			return nil, nil, false
		}
		expectedRow, ok := objectValue(row["expected"])
		id, idOK := stringValue(row["case_id"])
		allowed, allowedOK := boolValue(expectedRow["allowed"])
		code, codeOK := stringValue(expectedRow["code"])
		if !ok || !idOK || !allowedOK || !codeOK {
			return nil, nil, false
		}
		mutationSpecs = append(mutationSpecs, frozenDecisionSpec{id: id, allowed: allowed, code: code})
	}
	return caseSpecs, mutationSpecs, len(caseSpecs) == 69 && len(mutationSpecs) > 0
}

func embeddedObject(input []byte) (map[string]any, bool) {
	value, err := parseStrictJSONImpl(input)
	if err != nil {
		return nil, false
	}
	result, ok := objectValue(value)
	return result, ok
}

func mustArrayValue(value any) []any {
	result, _ := arrayValue(value)
	return result
}

func frozenRequiredSetDigest(caseSpecs, mutationSpecs []frozenDecisionSpec) (string, bool) {
	caseIDs := make([]any, len(caseSpecs))
	for index, spec := range caseSpecs {
		caseIDs[index] = spec.id
	}
	mutationIDs := make([]any, len(mutationSpecs))
	for index, spec := range mutationSpecs {
		mutationIDs[index] = spec.id
	}
	index, indexOK := embeddedObject(embeddedContractIndex)
	indexFiles, filesOK := arrayValue(index["files"])
	predecessor, predecessorOK := objectValue(index["predecessor"])
	predecessorDigest, predecessorDigestOK := stringValue(predecessor["sha256"])
	if !indexOK || !filesOK || !predecessorOK || !predecessorDigestOK || sha256HexImpl(embeddedContractIndex) != contractIndexSHA256 || predecessorDigest != predecessorSHA256 || len(indexFiles) != len(indexFileOrder) {
		return "", false
	}
	bindings := make(map[string]string, len(indexFiles)+1)
	bindings["contract-index.json"] = contractIndexSHA256
	for position, raw := range indexFiles {
		binding, bindingOK := objectValue(raw)
		path, pathOK := stringValue(binding["relative_path"])
		digest, digestOK := stringValue(binding["sha256"])
		if !bindingOK || !pathOK || !digestOK || path != indexFileOrder[position] || digest != mirrorDigests[path] {
			return "", false
		}
		bindings[path] = digest
	}
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	files := make([]any, 0, len(names))
	for _, name := range names {
		files = append(files, map[string]any{"relative_path": name, "sha256": bindings[name]})
	}
	value := map[string]any{
		"case_ids": caseIDs, "mutation_case_ids": mutationIDs, "bundle_files": files,
		"contract_index_sha256": contractIndexSHA256, "predecessor_sha256": predecessorSHA256,
		"stable_codes": StableCodes, "command_ids": []string{"cc-focused-contract-suite-v1", "sub-focused-oracleevidence-v1"},
		"semantic_surfaces": []string{"strict_json", "jcs", "normalization", "cbor", "schema", "admission", "authority", "interface", "replay", "sidecar"},
	}
	canonical, err := canonicalizeValueImpl(value)
	if err != nil {
		return "", false
	}
	return sha256HexImpl(append(canonical, '\n')), true
}

func sortedMirrorNames() []string {
	names := make([]string, 0, len(mirrorDigests))
	for name := range mirrorDigests {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validCommitDAG(value, authorityValue any) bool {
	dag, ok := objectValue(value)
	if !ok || !exactKeys(dag, "nodes", "edges") {
		return false
	}
	nodes, nodesOK := arrayValue(dag["nodes"])
	edges, edgesOK := arrayValue(dag["edges"])
	ids := []string{"C0", "S0", "S1", "R1", "I1", "SR", "C1", "CR"}
	roles := []string{"merge-this-cc-docs-amendment", "fresh-true-sub-mandatory-entry-and-docs-plan", "independent-review-and-merge-true-sub-docs-plan", "compileable-fail-closed-behavioral-red-scaffold", "single-implementation-wave", "independent-exact-head-true-sub-review", "cc-checker-integration", "cross-repo-exact-head-review-and-controller-decision"}
	if !nodesOK || !edgesOK || len(nodes) != len(ids) || len(edges) != len(ids)-1 {
		return false
	}
	authority, authorityOK := objectValue(authorityValue)
	cc, ccOK := objectValue(authority["cc"])
	sub, subOK := objectValue(authority["sub"])
	if !authorityOK || !ccOK || !subOK {
		return false
	}
	for index, raw := range nodes {
		node, ok := objectValue(raw)
		if !ok || !exactKeys(node, "id", "role", "parent_ids", "head", "tree") || !stringEquals(node["id"], ids[index]) || !stringEquals(node["role"], roles[index]) || !validOID(node["head"]) || !validOID(node["tree"]) {
			return false
		}
		parents, ok := stringArray(node["parent_ids"], 2)
		if !ok || (index == 0 && len(parents) != 0) || (index > 0 && !equalStrings(parents, []string{ids[index-1]})) {
			return false
		}
		if ids[index] == "C0" && (!jsonValuesEqual(node["head"], cc["commit"]) || !jsonValuesEqual(node["tree"], cc["tree"])) {
			return false
		}
		if ids[index] == "R1" && (!jsonValuesEqual(node["head"], sub["r1_commit"]) || !jsonValuesEqual(node["tree"], sub["r1_tree"])) {
			return false
		}
		if ids[index] == "I1" && (!jsonValuesEqual(node["head"], sub["i1_commit"]) || !jsonValuesEqual(node["tree"], sub["i1_tree"])) {
			return false
		}
	}
	for index, raw := range edges {
		edge, ok := arrayValue(raw)
		if !ok || len(edge) != 2 || !stringEquals(edge[0], ids[index]) || !stringEquals(edge[1], ids[index+1]) {
			return false
		}
	}
	return true
}

func validCrossReview(value any) bool {
	review, ok := objectValue(value)
	if !ok || !exactKeys(review, "sub", "cross") {
		return false
	}
	for name, verdict := range map[string]string{"sub": "PLAN_REVIEW_PASS", "cross": "CROSS_REPO_PASS"} {
		item, ok := objectValue(review[name])
		if !ok || !exactKeys(item, "task_id", "model", "artifact_sha256", "critical", "important", "verdict") || !safeRef(item["task_id"]) || !stringEquals(item["model"], "gpt-5.6-sol") || !validDigest(item["artifact_sha256"]) || !intEquals(item["critical"], 0) || !intEquals(item["important"], 0) || !stringEquals(item["verdict"], verdict) {
			return false
		}
	}
	return true
}

func stringEquals(value any, expected string) bool {
	actual, ok := stringValue(value)
	return ok && actual == expected
}

func intEquals(value any, expected int64) bool {
	actual, ok := int64Value(value)
	return ok && actual == expected
}

func validDigest(value any) bool {
	digest, ok := stringValue(value)
	return ok && isSHA256(digest)
}

func validOID(value any) bool {
	oid, ok := stringValue(value)
	if !ok || len(oid) != 40 {
		return false
	}
	for _, current := range []byte(oid) {
		if !((current >= '0' && current <= '9') || (current >= 'a' && current <= 'f')) {
			return false
		}
	}
	return true
}

func allDigestFields(value map[string]any, fields ...string) bool {
	for _, field := range fields {
		if !validDigest(value[field]) {
			return false
		}
	}
	return true
}

func nullableDigest(value any) bool { return value == nil || validDigest(value) }

func nullableOID(value any) bool { return value == nil || validOID(value) }

func nullableHex(value any) bool {
	if value == nil {
		return true
	}
	text, ok := stringValue(value)
	if !ok || len(text)%2 != 0 {
		return false
	}
	for _, current := range []byte(text) {
		if !((current >= '0' && current <= '9') || (current >= 'a' && current <= 'f')) {
			return false
		}
	}
	return true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
