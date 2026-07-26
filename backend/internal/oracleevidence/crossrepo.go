package oracleevidence

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
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
	}
	predecessor, err := readContractFile(predecessorPath, maxJSONBytes)
	if err != nil || sha256HexImpl(predecessor) != predecessorSHA256 {
		return Decision{Code: "contract_predecessor_mismatch"}
	}
	return Decision{Allowed: true}
}

func readContractFile(path string, maximum int64) ([]byte, error) {
	if err := rejectSymlinkComponents(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, contractErr(CodeContractBundle)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, contractErr("contract_symlink")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 != 0 || info.Size() < 1 || info.Size() > maximum {
		return nil, contractErr("contract_file_set_invalid")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
		return nil, contractErr("contract_file_set_invalid")
	}
	return os.ReadFile(path)
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
	value, err := parseStrictJSONImpl(input)
	if err != nil {
		return Decision{Code: "contract_json_invalid"}
	}
	record, ok := objectValue(value)
	if !ok {
		return Decision{Code: "cross_repo_binding_mismatch"}
	}
	if containsDiagnosticPromotion(record) {
		return Decision{Code: "authority_diagnostic_promotion"}
	}
	if containsLeak(record) {
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
	if !validCrossAuthority(record["authority"]) || !validCrossBundle(record["bundle"]) || !validCommitDAG(record["commit_dag"]) || !validCrossReview(record["review"]) {
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
			if leakKeys[normalized] || containsLeak(child) {
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
		lower := strings.ToLower(typed)
		return strings.Contains(typed, "/Users/") || strings.Contains(typed, "/home/") || strings.Contains(typed, "/var/folders/") || strings.HasPrefix(typed, `\\`) ||
			(len(typed) >= 3 && ((typed[0] >= 'A' && typed[0] <= 'Z') || (typed[0] >= 'a' && typed[0] <= 'z')) && typed[1] == ':' && (typed[2] == '\\' || typed[2] == '/')) ||
			strings.HasPrefix(lower, "bearer ") || strings.HasPrefix(lower, "basic ") || strings.Contains(typed, "-----BEGIN PRIVATE KEY-----")
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
	if !stringEquals(cc["commit"], "debe0360384132d6e66c0296219ea6066193e187") || !stringEquals(cc["tree"], "ffca2a0a892b2292b486533089f3276f28b39d4e") || !stringEquals(cc["amendment_sha256"], "eeaefeddbfe740003288f9d8ec8ba4673b57cca91f4fc3bf5cea5db02feaefaf") {
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
	if !caseOK || !mutationOK || len(caseRows) == 0 || len(mutationRows) == 0 {
		return false
	}
	if !validDecisionRows(caseRows) || !validDecisionRows(mutationRows) {
		return false
	}
	caseCanonical, _ := canonicalizeValueImpl(caseRows)
	mutationCanonical, _ := canonicalizeValueImpl(mutationRows)
	return stringEquals(result["decisions_sha256"], sha256HexImpl(append(caseCanonical, '\n'))) && stringEquals(result["mutation_results_sha256"], sha256HexImpl(append(mutationCanonical, '\n')))
}

func validDecisionRows(rows []any) bool {
	codes := make(map[string]bool, len(StableCodes))
	for _, code := range StableCodes {
		codes[code] = true
	}
	for _, raw := range rows {
		row, ok := objectValue(raw)
		if !ok || !exactKeys(row, "case_id", "allowed", "code", "next_state_digest", "canonical_hex") || !safeRef(row["case_id"]) {
			return false
		}
		if _, ok := boolValue(row["allowed"]); !ok {
			return false
		}
		code, ok := stringValue(row["code"])
		if !ok || !codes[code] || !nullableDigest(row["next_state_digest"]) || !nullableHex(row["canonical_hex"]) {
			return false
		}
	}
	return true
}

func validCommitDAG(value any) bool {
	dag, ok := objectValue(value)
	if !ok || !exactKeys(dag, "nodes", "edges") {
		return false
	}
	nodes, nodesOK := arrayValue(dag["nodes"])
	edges, edgesOK := arrayValue(dag["edges"])
	ids := []string{"C0", "S0", "S1", "R1", "I1", "SR", "C1", "CR"}
	if !nodesOK || !edgesOK || len(nodes) != len(ids) || len(edges) != len(ids)-1 {
		return false
	}
	for index, raw := range nodes {
		node, ok := objectValue(raw)
		if !ok || !exactKeys(node, "id", "role", "parent_ids", "head", "tree") || !stringEquals(node["id"], ids[index]) || !safeRef(node["role"]) || !nullableOID(node["head"]) || !nullableOID(node["tree"]) {
			return false
		}
		parents, ok := stringArray(node["parent_ids"], 2)
		if !ok || (index == 0 && len(parents) != 0) || (index > 0 && !equalStrings(parents, []string{ids[index-1]})) {
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
	if !ok || (len(oid) != 40 && len(oid) != 64) {
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
