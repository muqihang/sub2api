package oracleevidence

import (
	_ "embed"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed testdata/oracle_lab_contract/v1/coherence-corpus.json
var embeddedCoherenceCorpus []byte

type virtualMutationFile struct {
	data    []byte
	mode    uint32
	symlink bool
	target  string
}

func parseBoundedPointerIndexImpl(segment string, length uint64, allowEnd bool) (uint64, error) {
	if segment == "" || segment == "-" || (len(segment) > 1 && segment[0] == '0') {
		return 0, contractErr(CodeMutationPointer)
	}
	var value uint64
	for _, current := range []byte(segment) {
		if current < '0' || current > '9' {
			return 0, contractErr(CodeMutationPointer)
		}
		digit := uint64(current - '0')
		if value > (4_096-digit)/10 {
			return 0, contractErr(CodeMutationPointer)
		}
		value = value*10 + digit
	}
	if value > 4_096 || value > length || (!allowEnd && value == length) {
		return 0, contractErr(CodeMutationPointer)
	}
	return value, nil
}

func applyMutationImpl(source []byte, operation MutationOperation) ([]byte, error) {
	if len(source) == 0 || len(source) > maxJSONBytes || operation.Kind == "" {
		return nil, contractErr(CodeMutationSource)
	}
	switch operation.Kind {
	case "replace_bytes":
		decoded, err := base64.StdEncoding.Strict().DecodeString(operation.BytesBase64)
		if err != nil || operation.Offset > uint64(len(source)) || operation.DeleteCount > uint64(len(source))-operation.Offset {
			return nil, contractErr(CodeMutationSource)
		}
		resultSize := uint64(len(source)) - operation.DeleteCount + uint64(len(decoded))
		if resultSize > maxJSONBytes {
			return nil, contractErr(CodeMutationSource)
		}
		result := make([]byte, 0, resultSize)
		result = append(result, source[:operation.Offset]...)
		result = append(result, decoded...)
		result = append(result, source[operation.Offset+operation.DeleteCount:]...)
		return result, nil
	case "set_pointer", "remove_pointer":
		value, err := parseStrictJSONImpl(source)
		if err != nil {
			return nil, contractErr(CodeMutationSource)
		}
		segments, err := decodeJSONPointer(operation.Pointer)
		if err != nil {
			return nil, err
		}
		mutated, err := mutateJSONPointer(value, segments, operation)
		if err != nil {
			return nil, err
		}
		return canonicalizeValueImpl(mutated)
	case "add_file", "remove_file", "replace_with_symlink":
		return nil, contractErr(CodeMutationSource)
	default:
		return nil, contractErr("mutation_descriptor_invalid")
	}
}

func decodeJSONPointer(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") || len(pointer) > 8_192 {
		return nil, contractErr(CodeMutationPointer)
	}
	rawSegments := strings.Split(pointer[1:], "/")
	segments := make([]string, len(rawSegments))
	for index, raw := range rawSegments {
		var output strings.Builder
		for offset := 0; offset < len(raw); offset++ {
			if raw[offset] != '~' {
				output.WriteByte(raw[offset])
				continue
			}
			if offset+1 >= len(raw) {
				return nil, contractErr(CodeMutationPointer)
			}
			offset++
			switch raw[offset] {
			case '0':
				output.WriteByte('~')
			case '1':
				output.WriteByte('/')
			default:
				return nil, contractErr(CodeMutationPointer)
			}
		}
		segments[index] = output.String()
	}
	return segments, nil
}

func mutateJSONPointer(root any, segments []string, operation MutationOperation) (any, error) {
	if len(segments) == 0 {
		if operation.Kind == "set_pointer" {
			if err := validateJSONValueImpl(operation.Value); err != nil {
				return nil, contractErr(CodeMutationSource)
			}
			return operation.Value, nil
		}
		return nil, contractErr(CodeMutationPointer)
	}
	return mutateJSONPointerAt(root, segments, operation)
}

func mutateJSONPointerAt(current any, segments []string, operation MutationOperation) (any, error) {
	segment := segments[0]
	last := len(segments) == 1
	switch typed := current.(type) {
	case map[string]any:
		if last {
			if operation.Kind == "remove_pointer" {
				if _, exists := typed[segment]; !exists {
					return nil, contractErr(CodeMutationPointer)
				}
				delete(typed, segment)
			} else {
				if err := validateJSONValueImpl(operation.Value); err != nil {
					return nil, contractErr(CodeMutationSource)
				}
				typed[segment] = operation.Value
			}
			return typed, nil
		}
		next, exists := typed[segment]
		if !exists {
			return nil, contractErr(CodeMutationPointer)
		}
		mutated, err := mutateJSONPointerAt(next, segments[1:], operation)
		if err != nil {
			return nil, err
		}
		typed[segment] = mutated
		return typed, nil
	case []any:
		allowEnd := last && operation.Kind == "set_pointer"
		index, err := parseBoundedPointerIndexImpl(segment, uint64(len(typed)), allowEnd)
		if err != nil {
			return nil, err
		}
		if last {
			if operation.Kind == "remove_pointer" {
				typed = append(typed[:index], typed[index+1:]...)
			} else {
				if err := validateJSONValueImpl(operation.Value); err != nil {
					return nil, contractErr(CodeMutationSource)
				}
				if index == uint64(len(typed)) {
					typed = append(typed, operation.Value)
				} else {
					typed[index] = operation.Value
				}
			}
			return typed, nil
		}
		if index >= uint64(len(typed)) {
			return nil, contractErr(CodeMutationPointer)
		}
		mutated, err := mutateJSONPointerAt(typed[index], segments[1:], operation)
		if err != nil {
			return nil, err
		}
		typed[index] = mutated
		return typed, nil
	default:
		return nil, contractErr(CodeMutationPointer)
	}
}

func executeMutationCorpusImpl(root string, corpusBytes []byte, schemas *SchemaSet) ([]MutationResult, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, contractErr(CodeMutationSource)
	}
	corpusValue, err := parseStrictJSONImpl(corpusBytes)
	if err != nil {
		return nil, contractErr("mutation_descriptor_invalid")
	}
	corpus, ok := objectValue(corpusValue)
	if !ok || !exactKeys(corpus, "cases", "schema_major", "schema_revision") {
		return nil, contractErr("mutation_descriptor_invalid")
	}
	cases, ok := arrayValue(corpus["cases"])
	if !ok || len(cases) == 0 || len(cases) > 4_096 {
		return nil, contractErr("mutation_descriptor_invalid")
	}
	manifest, err := loadSourceManifest(root)
	if err != nil {
		return nil, err
	}
	results := make([]MutationResult, 0, len(cases))
	anyAllowed := false
	for _, rawCase := range cases {
		mutation, parseErr := parseMutationCase(rawCase)
		if parseErr != nil {
			return nil, parseErr
		}
		binding, exists := manifest[mutation.Source.RelativePath]
		if !exists || binding.SHA256 != mutation.Source.SHA256 {
			return nil, contractErr(CodeMutationSource)
		}
		source, readErr := readBoundedSource(root, binding)
		if readErr != nil {
			return nil, readErr
		}
		var mutated []byte
		var applyErr error
		var overlay map[string]virtualMutationFile
		if isFileMutation(mutation.Operation.Kind) {
			overlay, applyErr = buildVirtualOverlay(root, manifest)
			if applyErr == nil {
				applyErr = applyVirtualFileMutation(overlay, mutation.Operation)
			}
			if applyErr == nil {
				mutated, applyErr = canonicalVirtualOverlay(overlay)
			}
		} else {
			mutated, applyErr = applyMutationImpl(source, mutation.Operation)
		}
		decision := Decision{}
		if applyErr != nil {
			decision = decisionFromError(applyErr, CodeMutationSource)
		} else if overlay != nil {
			decision = executeVirtualMutationSubject(mutation.Subject, overlay)
		} else {
			decision = executeMutationSubject(mutation.Subject, mutated, schemas)
		}
		if decision.Allowed {
			anyAllowed = true
		}
		results = append(results, MutationResult{CaseID: mutation.CaseID, Allowed: decision.Allowed, Code: decision.Code, OutputSHA256: sha256HexImpl(mutated)})
	}
	if !anyAllowed {
		return nil, contractErr("mutation_executor_unexercised")
	}
	return results, nil
}

func executeMutationSubject(subject string, input []byte, schemas *SchemaSet) Decision {
	switch subject {
	case "strict_json", "jcs":
		if _, err := canonicalizeJSONImpl(input); err != nil {
			return decisionFromError(err, CodeJSONInvalid)
		}
		return Decision{Allowed: true}
	case "schema":
		return validateContractObjectImpl(schemas, "behaviorCoherenceCertificate", input)
	case "admission":
		return executeAdmissionMutation(input)
	case "authority_record":
		return validateCrossRepoRecordImpl(input)
	case "cbor":
		if _, err := canonicalizeCBORImpl(input); err != nil {
			return decisionFromError(err, CodeCBORInvalid)
		}
		return Decision{Allowed: true}
	case "normalization":
		return executeNormalizationMutation(input)
	case "authority":
		return executeAuthorityMutation(input)
	case "interface":
		return executeInterfaceMutation(input)
	case "replay":
		return executeReplayMutation(input)
	case "sidecar":
		return executeSidecarMutation(input, schemas)
	default:
		return Decision{Code: "mutation_source_invalid"}
	}
}

func executeAdmissionMutation(input []byte) Decision {
	value, err := parseStrictJSONImpl(input)
	if err != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	control, ok := objectValue(value)
	if !ok {
		return Decision{Code: "admission_schema_invalid"}
	}
	if exactKeys(control, "certificate", "context") {
		certificate, certificateErr := canonicalizeValueImpl(control["certificate"])
		context, contextErr := canonicalizeValueImpl(control["context"])
		if certificateErr != nil || contextErr != nil {
			return Decision{Code: "admission_schema_invalid"}
		}
		return decideBehaviorAdmissionImpl(certificate, context)
	}
	if !exactKeys(control, "kind", "version") || !stringEquals(control["kind"], "synthetic-control") || !intEquals(control["version"], 1) {
		return Decision{Code: "admission_schema_invalid"}
	}
	corpusValue, corpusErr := parseStrictJSONImpl(embeddedCoherenceCorpus)
	corpus, corpusOK := objectValue(corpusValue)
	if corpusErr != nil || !corpusOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	certificate, certificateOK := objectValue(corpus["base_certificate"])
	baseContext, contextOK := objectValue(corpus["base_context"])
	negative, negativeOK := objectValue(corpus["negative_capabilities"])
	if !certificateOK || !contextOK || !negativeOK {
		return Decision{Code: "admission_schema_invalid"}
	}
	context := cloneObject(baseContext)
	context["negative_capabilities"] = negative
	signals, _ := canonicalizeValueImpl(context["signals"])
	negativeBytes, _ := canonicalizeValueImpl(negative)
	certificateBytes, _ := canonicalizeValueImpl(certificate)
	digest, digestErr := admissionPayloadDigestImpl(certificateBytes, signals, negativeBytes)
	if digestErr != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	expected := cloneObject(mustObject(context["expected"]))
	expected["manifest_payload_digest"] = digest
	context["expected"] = expected
	contextBytes, contextErr := canonicalizeValueImpl(context)
	if contextErr != nil {
		return Decision{Code: "admission_schema_invalid"}
	}
	return decideBehaviorAdmissionImpl(certificateBytes, contextBytes)
}

func executeNormalizationMutation(input []byte) Decision {
	value, ok := strictObject(input)
	if !ok || !exactKeys(value, "path", "query_pairs", "host", "port") {
		return Decision{Code: CodeMutationSource}
	}
	pairsValue, ok := arrayValue(value["query_pairs"])
	if !ok {
		return Decision{Code: CodeMutationSource}
	}
	pairs := make([][2]string, 0, len(pairsValue))
	for _, rawPair := range pairsValue {
		pair, ok := arrayValue(rawPair)
		if !ok || len(pair) != 2 {
			return Decision{Code: CodeMutationSource}
		}
		left, leftOK := stringValue(pair[0])
		right, rightOK := stringValue(pair[1])
		if !leftOK || !rightOK {
			return Decision{Code: CodeMutationSource}
		}
		pairs = append(pairs, [2]string{left, right})
	}
	path, pathOK := stringValue(value["path"])
	host, hostOK := stringValue(value["host"])
	port, portOK := stringValue(value["port"])
	if !pathOK || !hostOK || !portOK {
		return Decision{Code: CodeMutationSource}
	}
	if _, err := normalizePathQueryImpl(path, pairs); err != nil {
		return decisionFromError(err, "url_path_invalid")
	}
	if _, err := formatAuthorityImpl(host, RawPort(port)); err != nil {
		return decisionFromError(err, CodeURLHostInvalid)
	}
	return Decision{Allowed: true}
}

func executeAuthorityMutation(input []byte) Decision {
	wrapper, ok := strictObject(input)
	if !ok || !exactKeys(wrapper, "operation", "state", "candidate", "context") {
		return Decision{Code: "authority_signature_invalid"}
	}
	state, stateErr := canonicalizeValueImpl(wrapper["state"])
	candidate, candidateErr := canonicalizeValueImpl(wrapper["candidate"])
	context, contextErr := canonicalizeValueImpl(wrapper["context"])
	if stateErr != nil || candidateErr != nil || contextErr != nil {
		return Decision{Code: "authority_signature_invalid"}
	}
	switch operation, _ := stringValue(wrapper["operation"]); operation {
	case "manifest_update":
		return verifyManifestAuthorityUpdateImpl(AuthorityInput{State: state, Candidate: candidate, Context: context})
	case "root_rotation":
		return verifyRootRotationImpl(AuthorityInput{State: state, Candidate: candidate, Context: context})
	case "emergency_revocation":
		return verifyEmergencyRevocationImpl(AuthorityInput{State: state, Candidate: candidate, Context: context})
	default:
		return Decision{Code: "authority_signature_invalid"}
	}
}

func executeInterfaceMutation(input []byte) Decision {
	wrapper, ok := strictObject(input)
	if !ok {
		return Decision{Code: "interface_schema_unsupported"}
	}
	kind, _ := stringValue(wrapper["operation"])
	switch kind {
	case "readiness":
		if !exactKeys(wrapper, "operation", "handshake", "expected") {
			return Decision{Code: "interface_schema_unsupported"}
		}
		left, leftErr := canonicalizeValueImpl(wrapper["handshake"])
		right, rightErr := canonicalizeValueImpl(wrapper["expected"])
		if leftErr != nil || rightErr != nil {
			return Decision{Code: "interface_schema_unsupported"}
		}
		return decideReadinessImpl(left, right)
	case "lifecycle":
		if !exactKeys(wrapper, "operation", "state", "candidate") {
			return Decision{Code: "interface_schema_unsupported"}
		}
		left, leftErr := canonicalizeValueImpl(wrapper["state"])
		right, rightErr := canonicalizeValueImpl(wrapper["candidate"])
		if leftErr != nil || rightErr != nil {
			return Decision{Code: "interface_schema_unsupported"}
		}
		return decideLifecycleImpl(left, right)
	case "lineage":
		if !exactKeys(wrapper, "operation", "state", "candidate", "now_ms") {
			return Decision{Code: "interface_schema_unsupported"}
		}
		left, leftErr := canonicalizeValueImpl(wrapper["state"])
		right, rightErr := canonicalizeValueImpl(wrapper["candidate"])
		now, nowOK := int64Value(wrapper["now_ms"])
		if leftErr != nil || rightErr != nil || !nowOK {
			return Decision{Code: "interface_schema_unsupported"}
		}
		return decideTaskLineageImpl(left, right, now)
	case "outcome":
		if !exactKeys(wrapper, "operation", "outcome") {
			return Decision{Code: "interface_schema_unsupported"}
		}
		outcome, err := canonicalizeValueImpl(wrapper["outcome"])
		if err != nil {
			return Decision{Code: "interface_schema_unsupported"}
		}
		return decideOutcomeImpl(outcome)
	default:
		return Decision{Code: "interface_schema_unsupported"}
	}
}

func executeReplayMutation(input []byte) Decision {
	wrapper, ok := strictObject(input)
	if !ok || !exactKeys(wrapper, "state", "command") {
		return Decision{Code: "replay_rejected"}
	}
	state, stateErr := canonicalizeValueImpl(wrapper["state"])
	command, commandErr := canonicalizeValueImpl(wrapper["command"])
	if stateErr != nil || commandErr != nil {
		return Decision{Code: "replay_rejected"}
	}
	return executeReplayImpl(state, command)
}

func executeSidecarMutation(input []byte, schemas *SchemaSet) Decision {
	wrapper, ok := strictObject(input)
	if !ok || !exactKeys(wrapper, "envelope_base64", "capability", "keyring", "now_ms") {
		return Decision{Code: "sidecar_capability_schema_invalid"}
	}
	encoded, encodedOK := stringValue(wrapper["envelope_base64"])
	envelope, err := base64.StdEncoding.Strict().DecodeString(encoded)
	capability, capabilityErr := canonicalizeValueImpl(wrapper["capability"])
	keyring, keyringErr := canonicalizeValueImpl(wrapper["keyring"])
	now, nowOK := int64Value(wrapper["now_ms"])
	if !encodedOK || err != nil || capabilityErr != nil || keyringErr != nil || !nowOK {
		return Decision{Code: "sidecar_capability_schema_invalid"}
	}
	if envelopeDecision := validateSidecarEnvelopeImpl(envelope, schemas); !envelopeDecision.Allowed {
		return envelopeDecision
	}
	return verifySidecarCapabilityImpl(envelope, capability, keyring, now)
}

func isFileMutation(kind string) bool {
	return kind == "add_file" || kind == "remove_file" || kind == "replace_with_symlink"
}

func buildVirtualOverlay(root string, manifest map[string]SourceBinding) (map[string]virtualMutationFile, error) {
	overlay := make(map[string]virtualMutationFile, len(manifest))
	for path, binding := range manifest {
		data, err := readBoundedSource(root, binding)
		if err != nil {
			return nil, err
		}
		overlay[path] = virtualMutationFile{data: data, mode: 0o600}
	}
	return overlay, nil
}

func applyVirtualFileMutation(overlay map[string]virtualMutationFile, operation MutationOperation) error {
	if !safeRelativePath(operation.Path) {
		return contractErr(CodeMutationSource)
	}
	switch operation.Kind {
	case "add_file":
		if _, exists := overlay[operation.Path]; exists || operation.Mode > 0o777 || operation.Mode&0o111 != 0 {
			return contractErr(CodeMutationSource)
		}
		data, err := base64.StdEncoding.Strict().DecodeString(operation.BytesBase64)
		if err != nil || len(data) > maxJSONBytes {
			return contractErr(CodeMutationSource)
		}
		overlay[operation.Path] = virtualMutationFile{data: data, mode: operation.Mode}
	case "remove_file":
		if _, exists := overlay[operation.Path]; !exists {
			return contractErr(CodeMutationSource)
		}
		delete(overlay, operation.Path)
	case "replace_with_symlink":
		if _, exists := overlay[operation.Path]; !exists || !safeRelativePath(operation.Target) {
			return contractErr(CodeMutationSource)
		}
		resolved := filepath.Clean(filepath.Join(filepath.Dir(operation.Path), operation.Target))
		if !safeRelativePath(filepath.ToSlash(resolved)) {
			return contractErr(CodeMutationSource)
		}
		overlay[operation.Path] = virtualMutationFile{symlink: true, target: operation.Target}
	default:
		return contractErr("mutation_descriptor_invalid")
	}
	return nil
}

func canonicalVirtualOverlay(overlay map[string]virtualMutationFile) ([]byte, error) {
	rows := make([]any, 0, len(overlay))
	paths := make([]string, 0, len(overlay))
	for path := range overlay {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		file := overlay[path]
		rows = append(rows, map[string]any{"path": path, "sha256": sha256HexImpl(file.data), "mode": uint64(file.mode), "symlink": file.symlink, "target": file.target})
	}
	return canonicalizeValueImpl(rows)
}

func executeVirtualMutationSubject(subject string, overlay map[string]virtualMutationFile) Decision {
	if subject != "mirror" {
		return Decision{Code: CodeMutationSource}
	}
	if len(overlay) != len(mirrorDigests) {
		return Decision{Code: "contract_file_set_invalid"}
	}
	for path, expected := range mirrorDigests {
		file, ok := overlay[path]
		if !ok {
			return Decision{Code: "contract_required_set_mismatch"}
		}
		if file.symlink {
			return Decision{Code: "contract_symlink"}
		}
		if file.mode&0o111 != 0 {
			return Decision{Code: "contract_file_set_invalid"}
		}
		if sha256HexImpl(file.data) != expected {
			return Decision{Code: "contract_file_digest_mismatch"}
		}
	}
	return Decision{Allowed: true}
}

func parseMutationCase(value any) (MutationCase, error) {
	record, ok := objectValue(value)
	if !ok || !exactKeys(record, "case_id", "subject", "source", "operation", "expected") {
		return MutationCase{}, contractErr("mutation_descriptor_invalid")
	}
	caseID, caseOK := stringValue(record["case_id"])
	subject, subjectOK := stringValue(record["subject"])
	source, sourceOK := objectValue(record["source"])
	operation, operationOK := objectValue(record["operation"])
	expected, expectedOK := objectValue(record["expected"])
	if !caseOK || !subjectOK || !sourceOK || !operationOK || !expectedOK || !safeRef(caseID) || !containsString([]string{"strict_json", "jcs", "normalization", "cbor", "schema", "admission", "authority", "interface", "replay", "sidecar", "mirror", "authority_record"}, subject) || !exactKeys(source, "relative_path", "sha256") || !exactKeys(expected, "allowed", "code") {
		return MutationCase{}, contractErr("mutation_descriptor_invalid")
	}
	relativePath, pathOK := stringValue(source["relative_path"])
	digest, digestOK := stringValue(source["sha256"])
	if !pathOK || !digestOK || !safeRelativePath(relativePath) || !isSHA256(digest) {
		return MutationCase{}, contractErr("mutation_descriptor_invalid")
	}
	mutationOperation, err := mutationOperationFromMap(operation)
	if err != nil {
		return MutationCase{}, err
	}
	allowed, allowedOK := boolValue(expected["allowed"])
	code, codeOK := stringValue(expected["code"])
	if !allowedOK || !codeOK {
		return MutationCase{}, contractErr("mutation_descriptor_invalid")
	}
	return MutationCase{CaseID: caseID, Subject: subject, Source: SourceBinding{RelativePath: relativePath, SHA256: digest}, Operation: mutationOperation, Expected: Decision{Allowed: allowed, Code: code}}, nil
}

func mutationOperationFromMap(value map[string]any) (MutationOperation, error) {
	kind, ok := stringValue(value["kind"])
	if !ok {
		return MutationOperation{}, contractErr("mutation_descriptor_invalid")
	}
	result := MutationOperation{Kind: kind}
	switch kind {
	case "replace_bytes":
		offset, offsetOK := uint64Value(value["offset"])
		deleteCount, deleteOK := uint64Value(value["delete_count"])
		bytesBase64, base64OK := stringValue(value["bytes_base64"])
		if !exactKeys(value, "kind", "offset", "delete_count", "bytes_base64") || !offsetOK || !deleteOK || !base64OK {
			return MutationOperation{}, contractErr("mutation_descriptor_invalid")
		}
		result.Offset, result.DeleteCount, result.BytesBase64 = offset, deleteCount, bytesBase64
	case "set_pointer":
		pointer, pointerOK := stringValue(value["pointer"])
		if !exactKeys(value, "kind", "pointer", "value") || !pointerOK {
			return MutationOperation{}, contractErr("mutation_descriptor_invalid")
		}
		result.Pointer, result.Value = pointer, value["value"]
	case "remove_pointer":
		pointer, pointerOK := stringValue(value["pointer"])
		if !exactKeys(value, "kind", "pointer") || !pointerOK {
			return MutationOperation{}, contractErr("mutation_descriptor_invalid")
		}
		result.Pointer = pointer
	case "add_file":
		path, pathOK := stringValue(value["path"])
		bytesBase64, base64OK := stringValue(value["bytes_base64"])
		mode, modeOK := uint64Value(value["mode"])
		if !exactKeys(value, "kind", "path", "bytes_base64", "mode") || !pathOK || !base64OK || !modeOK || mode > 0o777 {
			return MutationOperation{}, contractErr("mutation_descriptor_invalid")
		}
		result.Path, result.BytesBase64, result.Mode = path, bytesBase64, uint32(mode)
	case "remove_file":
		path, pathOK := stringValue(value["path"])
		if !exactKeys(value, "kind", "path") || !pathOK {
			return MutationOperation{}, contractErr("mutation_descriptor_invalid")
		}
		result.Path = path
	case "replace_with_symlink":
		path, pathOK := stringValue(value["path"])
		target, targetOK := stringValue(value["target"])
		if !exactKeys(value, "kind", "path", "target") || !pathOK || !targetOK {
			return MutationOperation{}, contractErr("mutation_descriptor_invalid")
		}
		result.Path, result.Target = path, target
	default:
		return MutationOperation{}, contractErr("mutation_descriptor_invalid")
	}
	return result, nil
}

func loadSourceManifest(root string) (map[string]SourceBinding, error) {
	data, err := readRegularFile(filepath.Join(root, "source-manifest.json"), maxJSONBytes)
	if err != nil {
		return nil, contractErr(CodeMutationSource)
	}
	value, err := parseStrictJSONImpl(data)
	if err != nil {
		return nil, contractErr(CodeMutationSource)
	}
	record, ok := objectValue(value)
	if !ok || !exactKeys(record, "schema_major", "schema_revision", "sources") {
		return nil, contractErr(CodeMutationSource)
	}
	rawSources, ok := arrayValue(record["sources"])
	if !ok || len(rawSources) > 4_096 {
		return nil, contractErr(CodeMutationSource)
	}
	result := make(map[string]SourceBinding, len(rawSources))
	for _, rawSource := range rawSources {
		source, ok := objectValue(rawSource)
		if !ok || !exactKeys(source, "kind", "max_bytes", "mode_class", "relative_path", "sha256", "size") {
			return nil, contractErr(CodeMutationSource)
		}
		relativePath, pathOK := stringValue(source["relative_path"])
		digest, digestOK := stringValue(source["sha256"])
		size, sizeOK := uint64Value(source["size"])
		maximum, maximumOK := uint64Value(source["max_bytes"])
		kind, kindOK := stringValue(source["kind"])
		modeClass, modeOK := stringValue(source["mode_class"])
		if !pathOK || !digestOK || !sizeOK || !maximumOK || !kindOK || !modeOK || !safeRelativePath(relativePath) || !isSHA256(digest) || size > maximum || maximum > maxJSONBytes || modeClass != "regular_non_executable" {
			return nil, contractErr(CodeMutationSource)
		}
		if _, duplicate := result[relativePath]; duplicate {
			return nil, contractErr(CodeMutationSource)
		}
		result[relativePath] = SourceBinding{RelativePath: relativePath, SHA256: digest, Size: size, MaxBytes: maximum, Kind: kind, ModeClass: modeClass}
	}
	return result, nil
}

func readBoundedSource(root string, binding SourceBinding) ([]byte, error) {
	path := filepath.Join(root, filepath.FromSlash(binding.RelativePath))
	rootAbsolute, rootErr := filepath.Abs(root)
	pathAbsolute, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil || !strings.HasPrefix(pathAbsolute, rootAbsolute+string(os.PathSeparator)) {
		return nil, contractErr(CodeMutationSource)
	}
	data, err := readRegularFile(path, binding.MaxBytes)
	if err != nil || uint64(len(data)) != binding.Size || sha256HexImpl(data) != binding.SHA256 {
		return nil, contractErr(CodeMutationSource)
	}
	return data, nil
}

func readRegularFile(path string, maximum uint64) ([]byte, error) {
	file, openErr := openNoFollow(path)
	if openErr != nil {
		return nil, contractErr(CodeMutationSource)
	}
	defer file.Close()
	opened, statErr := file.Stat()
	if statErr != nil || !stableRegularFile(opened, maximum, false) {
		return nil, contractErr(CodeMutationSource)
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil || uint64(len(data)) > maximum {
		return nil, contractErr(CodeMutationSource)
	}
	after, statErr := file.Stat()
	reopened, reopenErr := openNoFollow(path)
	if reopenErr != nil {
		return nil, contractErr(CodeMutationSource)
	}
	pathAfter, pathErr := reopened.Stat()
	reopened.Close()
	if statErr != nil || pathErr != nil || !sameStableFile(opened, after) || !sameStableFile(after, pathAfter) {
		return nil, contractErr(CodeMutationSource)
	}
	return data, nil
}

func safeRelativePath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || filepath.IsAbs(value) {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return filepath.ToSlash(filepath.Clean(value)) == value
}
