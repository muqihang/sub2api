package oracleevidence

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustFixtureObject(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(mirrorRoot, name))
	if err != nil {
		t.Fatal(err)
	}
	value, err := ParseStrictJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := objectValue(value)
	if !ok {
		t.Fatalf("fixture %s is not an object", name)
	}
	return result
}

func mustBytes(t *testing.T, value any) []byte {
	t.Helper()
	data, err := CanonicalizeValue(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cloneValue(t *testing.T, value any) any {
	t.Helper()
	data := mustBytes(t, value)
	cloned, err := ParseStrictJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return cloned
}

func cloneMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := objectValue(cloneValue(t, value))
	if !ok {
		t.Fatal("cloned value is not an object")
	}
	return result
}

func setDotted(t *testing.T, root map[string]any, dotted string, value any) {
	t.Helper()
	parts := strings.Split(dotted, ".")
	var current any = root
	for _, part := range parts[:len(parts)-1] {
		if object, ok := objectValue(current); ok {
			current = object[part]
			continue
		}
		array, ok := arrayValue(current)
		if !ok || len(part) != 1 || part[0] < '0' || part[0] > '9' || int(part[0]-'0') >= len(array) {
			t.Fatalf("invalid fixture mutation path %s", dotted)
		}
		current = array[int(part[0]-'0')]
	}
	parent, ok := objectValue(current)
	if !ok {
		t.Fatalf("invalid fixture mutation parent %s", dotted)
	}
	parent[parts[len(parts)-1]] = cloneValue(t, value)
}

func TestOracleContractScaffold(t *testing.T) {
	if len(StableCodes) != 119 || stableCodeDigest() != stableCodesSHA256 {
		t.Fatalf("stable code registry drift: count=%d digest=%s", len(StableCodes), stableCodeDigest())
	}
	for index := 1; index < len(StableCodes); index++ {
		if StableCodes[index-1] >= StableCodes[index] {
			t.Fatalf("stable codes are not a sorted unique set at %d", index)
		}
	}
	if decision := notImplementedDecision(); decision.Allowed || decision.Code != CodeOracleNotImplemented {
		t.Fatalf("fail-closed scaffold drift: %+v", decision)
	}
}

func TestOracleContractStrictJSON(t *testing.T) {
	corpus := mustFixtureObject(t, "canonicalization-corpus.json")
	cases, _ := arrayValue(corpus["json_cases"])
	if len(cases) != 9 {
		t.Fatalf("JSON required set = %d", len(cases))
	}
	for _, raw := range cases {
		fixture, _ := objectValue(raw)
		id, _ := stringValue(fixture["id"])
		t.Run(id, func(t *testing.T) {
			var input []byte
			if encoded, ok := stringValue(fixture["input_hex"]); ok {
				input, _ = hex.DecodeString(encoded)
			} else {
				text, _ := stringValue(fixture["input_json"])
				input = []byte(text)
			}
			_, err := ParseStrictJSON(input)
			valid, _ := boolValue(fixture["valid"])
			if valid && err != nil {
				t.Fatalf("valid JSON rejected: %v", err)
			}
			if !valid {
				code, _ := stringValue(fixture["expected_code"])
				requireCode(t, err, code)
			}
		})
	}
	requireCode(t, ValidateJSONValue(struct{}{}), CodeJSONTypeInvalid)
	requireCode(t, ValidateJSONValue(uint64(maxSafeInteger+1)), "json_number_unsafe")
	requireCode(t, ValidateJSONValue(string([]byte{0xff})), "json_invalid_utf8")
	multiFault := map[string]any{"a": string([]byte{0xff}), "b": uint64(maxSafeInteger + 1)}
	for iteration := 0; iteration < 32; iteration++ {
		requireCode(t, ValidateJSONValue(multiFault), "json_invalid_utf8")
	}
	badKeys := map[string]any{string([]byte{0xff}): uint64(maxSafeInteger + 1), string([]byte{0xfe}): struct{}{}}
	for iteration := 0; iteration < 32; iteration++ {
		requireCode(t, ValidateJSONValue(badKeys), "json_invalid_utf8")
	}
}

func TestOracleContractJCS(t *testing.T) {
	corpus := mustFixtureObject(t, "canonicalization-corpus.json")
	for _, raw := range mustArray(corpus["json_cases"]) {
		fixture := mustObject(raw)
		id, _ := stringValue(fixture["id"])
		t.Run(id, func(t *testing.T) {
			input := []byte(mustStringDefault(fixture["input_json"]))
			if encoded, ok := stringValue(fixture["input_hex"]); ok {
				input, _ = hex.DecodeString(encoded)
			}
			canonical, err := CanonicalizeJSON(input)
			valid, _ := boolValue(fixture["valid"])
			if !valid {
				code, _ := stringValue(fixture["expected_code"])
				requireCode(t, err, code)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if expected, ok := stringValue(fixture["expected_canonical_hex"]); ok && hex.EncodeToString(canonical) != expected {
				t.Fatalf("canonical = %x", canonical)
			}
			if expected, ok := stringValue(fixture["expected_sha256"]); ok && SHA256Hex(canonical) != expected {
				t.Fatalf("digest = %s", SHA256Hex(canonical))
			}
		})
	}
	canonical, err := CanonicalizeJSON([]byte(`{"n":1e-7,"small":0.000001}`))
	if err != nil || string(canonical) != `{"n":1e-7,"small":0.000001}` {
		t.Fatalf("ECMAScript number form = %q err=%v", canonical, err)
	}
}

func TestOracleContractNormalization(t *testing.T) {
	corpus := mustFixtureObject(t, "canonicalization-corpus.json")
	for _, raw := range mustArray(corpus["normalization_cases"]) {
		fixture := mustObject(raw)
		id, _ := stringValue(fixture["id"])
		t.Run(id, func(t *testing.T) {
			if expected, ok := stringValue(fixture["expected_path_query"]); ok {
				pairs := make([][2]string, 0)
				for _, rawPair := range mustArray(fixture["query_pairs"]) {
					pair := mustArray(rawPair)
					pairs = append(pairs, [2]string{mustStringDefault(pair[0]), mustStringDefault(pair[1])})
				}
				got, err := NormalizePathQuery(mustStringDefault(fixture["path"]), pairs)
				if err != nil || got != expected {
					t.Fatalf("path query = %q err=%v", got, err)
				}
			}
			if expected, ok := stringValue(fixture["expected_authority"]); ok {
				port, _ := int64Value(fixture["port"])
				got, err := FormatAuthority(mustStringDefault(fixture["host"]), RawPort(decimalKey(port)))
				if err != nil || got != expected {
					t.Fatalf("authority = %q err=%v", got, err)
				}
			}
		})
	}
	for _, invalid := range []string{"", "0", "00", "+1", "-1", " 443", "443 ", "65536", "18446744073709551616"} {
		_, err := ParseAuthorityPort(RawPort(invalid))
		requireCode(t, err, CodeURLPortInvalid)
	}
	for _, invalidHost := range []string{"", "api.example.com:443", "[2001:db8::1]", "bad host"} {
		_, err := FormatAuthority(invalidHost, RawPort("443"))
		requireCode(t, err, CodeURLHostInvalid)
	}
}

func TestOracleContractCBOR(t *testing.T) {
	corpus := mustFixtureObject(t, "canonicalization-corpus.json")
	cases := mustArray(corpus["cbor_cases"])
	if len(cases) != 5 {
		t.Fatalf("CBOR required set = %d", len(cases))
	}
	for _, raw := range cases {
		fixture := mustObject(raw)
		id, _ := stringValue(fixture["id"])
		t.Run(id, func(t *testing.T) {
			valid, _ := boolValue(fixture["valid"])
			if !valid {
				input, _ := hex.DecodeString(mustStringDefault(fixture["input_hex"]))
				_, err := DecodeDeterministicCBOR(input)
				requireCode(t, err, mustStringDefault(fixture["expected_code"]))
				return
			}
			encoded, err := encodeDeterministicCBOR(fixture["value"])
			if err != nil || hex.EncodeToString(encoded) != mustStringDefault(fixture["expected_hex"]) {
				t.Fatalf("CBOR = %x err=%v", encoded, err)
			}
			frame, err := EncodeCBORFrame(fixture["value"])
			if err != nil {
				t.Fatal(err)
			}
			if decoded, err := DecodeCBORFrame(frame); err != nil || !jsonValuesEqual(decoded, fixture["value"]) {
				t.Fatalf("frame round trip = %#v err=%v", decoded, err)
			}
		})
	}
	for input, code := range map[string]string{"c101": "cbor_tag_forbidden", "f7": "cbor_undefined_forbidden", "1817": "cbor_not_deterministic", "ff": "cbor_simple_forbidden"} {
		data, _ := hex.DecodeString(input)
		_, err := DecodeDeterministicCBOR(data)
		requireCode(t, err, code)
	}
}

func TestOracleContractSchema(t *testing.T) {
	schemas, err := LoadContractSchema(mirrorRoot)
	if err != nil {
		t.Fatal(err)
	}
	coherence := mustFixtureObject(t, "coherence-corpus.json")
	certificate := cloneMap(t, coherence["base_certificate"])
	if decision := ValidateContractObject(schemas, "behaviorCoherenceCertificate", mustBytes(t, certificate)); !decision.Allowed {
		t.Fatalf("valid certificate rejected: %+v", decision)
	}
	delete(certificate, "persona_ref")
	if decision := ValidateContractObject(schemas, "behaviorCoherenceCertificate", mustBytes(t, certificate)); decision.Allowed || decision.Code != "contract_schema_invalid" {
		t.Fatalf("missing required field = %+v", decision)
	}
	certificate = cloneMap(t, coherence["base_certificate"])
	certificate["unknown"] = true
	if decision := ValidateContractObject(schemas, "behaviorCoherenceCertificate", mustBytes(t, certificate)); decision.Allowed || decision.Code != "contract_schema_invalid" {
		t.Fatalf("unknown field = %+v", decision)
	}
	if decision := ValidateContractObject(schemas, "unknownDefinition", []byte(`{}`)); decision.Allowed || decision.Code != "contract_schema_invalid" {
		t.Fatalf("unknown definition = %+v", decision)
	}
}

func TestOracleContractAdmission(t *testing.T) {
	corpus := mustFixtureObject(t, "coherence-corpus.json")
	cases := mustArray(corpus["cases"])
	if len(cases) != 14 {
		t.Fatalf("admission required set = %d", len(cases))
	}
	for _, raw := range cases {
		fixture := mustObject(raw)
		id := mustStringDefault(fixture["id"])
		t.Run(id, func(t *testing.T) {
			certificate := cloneMap(t, corpus["base_certificate"])
			context := cloneMap(t, corpus["base_context"])
			context["negative_capabilities"] = cloneValue(t, corpus["negative_capabilities"])
			if fixture["mutation"] != nil {
				mutation := mustObject(fixture["mutation"])
				target := context
				if stringEquals(mutation["target"], "certificate") {
					target = certificate
				}
				if path, ok := stringValue(mutation["remove"]); ok {
					delete(target, path)
				} else if path, ok := stringValue(mutation["add"]); ok {
					target[path] = true
				} else {
					setDotted(t, target, mustStringDefault(mutation["set"]), mutation["value"])
				}
			}
			digest, err := AdmissionPayloadDigest(mustBytes(t, certificate), mustBytes(t, context["signals"]), mustBytes(t, context["negative_capabilities"]))
			if err != nil {
				t.Fatal(err)
			}
			mustObject(context["expected"])["manifest_payload_digest"] = digest
			decision := DecideBehaviorAdmission(mustBytes(t, certificate), mustBytes(t, context))
			want := mustStringDefault(fixture["expected_code"])
			if decision.Code != want || decision.Allowed != (want == "admission_allow") {
				t.Fatalf("decision = %+v want=%s", decision, want)
			}
		})
	}
	certificate := cloneMap(t, corpus["base_certificate"])
	context := cloneMap(t, corpus["base_context"])
	context["negative_capabilities"] = cloneValue(t, corpus["negative_capabilities"])
	digest, _ := AdmissionPayloadDigest(mustBytes(t, certificate), mustBytes(t, context["signals"]), mustBytes(t, context["negative_capabilities"]))
	mustObject(context["expected"])["manifest_payload_digest"] = digest
	certificate["persona_ref"] = "persona:unbound"
	if decision := DecideBehaviorAdmission(mustBytes(t, certificate), mustBytes(t, context)); decision.Code != "admission_manifest_payload_mismatch" {
		t.Fatalf("unbound certificate = %+v", decision)
	}
}

type testAuthorityKey struct {
	id      string
	role    string
	epoch   int64
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	spki    string
}

func newAuthorityKey(t *testing.T, id, role string, epoch int64) testAuthorityKey {
	t.Helper()
	seed := sha256.Sum256([]byte(id))
	private := ed25519.NewKeyFromSeed(seed[:])
	public := private.Public().(ed25519.PublicKey)
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return testAuthorityKey{id: id, role: role, epoch: epoch, private: private, public: public, spki: base64.RawURLEncoding.EncodeToString(der)}
}

func authorityKeyWire(key testAuthorityKey) map[string]any {
	return map[string]any{"keyId": key.id, "role": key.role, "epoch": key.epoch, "revoked": false, "publicKeySpkiBase64url": key.spki}
}

func authoritySignature(t *testing.T, key testAuthorityKey, domain []byte, signed any) map[string]any {
	t.Helper()
	canonical := mustBytes(t, signed)
	message := append(append([]byte(nil), domain...), canonical...)
	return map[string]any{"algorithm": "Ed25519", "keyId": key.id, "keyEpoch": key.epoch, "role": key.role, "signatureBase64url": base64.RawURLEncoding.EncodeToString(ed25519.Sign(key.private, message))}
}

type authorityFixture struct {
	keys       map[string]testAuthorityKey
	state      map[string]any
	context    map[string]any
	manifest   map[string]any
	checkpoint map[string]any
}

func buildAuthorityFixture(t *testing.T) authorityFixture {
	t.Helper()
	keys := make(map[string]testAuthorityKey)
	for _, spec := range [][3]any{{"root-old-1", "root", int64(1)}, {"root-old-2", "root", int64(1)}, {"root-old-3", "root", int64(1)}, {"manifest-1", "manifest", int64(1)}, {"manifest-2", "manifest", int64(1)}, {"manifest-3", "manifest", int64(1)}, {"checkpoint-1", "checkpoint", int64(1)}, {"revocation-1", "revocation", int64(1)}} {
		key := newAuthorityKey(t, spec[0].(string), spec[1].(string), spec[2].(int64))
		keys[key.id] = key
	}
	wireKeys := make(map[string]any)
	for id, key := range keys {
		wireKeys[id] = authorityKeyWire(key)
	}
	state := map[string]any{"rootEpoch": 1, "policyVersion": 10, "rollbackFloor": 10, "revocationVersion": 1, "manifestDigest": strings.Repeat("a", 64), "manifestPayloadDigest": strings.Repeat("d", 64), "checkpointVersion": 5, "checkpointDigest": strings.Repeat("b", 64), "replicaGeneration": 7, "lastWallClockMs": int64(1_799_999_999_000), "keys": wireKeys, "thresholds": map[string]any{"root": 2, "manifest": 2, "checkpoint": 1, "revocation": 1}, "rollbackTargets": map[string]any{}}
	manifest := cloneMap(t, mustFixtureObject(t, "authority-corpus.json")["manifest_authority_fixture"])
	manifestDigest, err := authorityObjectDigest(manifestDomain, manifest)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := map[string]any{"schemaId": "oracle.compatibility", "schemaMajor": 1, "schemaRevision": 0, "kind": "checkpoint", "version": 6, "manifestDigest": manifestDigest, "previousCheckpointDigest": state["checkpointDigest"], "witnessCheckpointDigest": manifest["witnessCheckpointDigest"], "issuedAtMs": int64(1_799_999_999_500), "expiresAtMs": int64(1_800_003_600_000)}
	context := map[string]any{"nowWallClockMs": int64(1_800_000_000_000), "monotonicElapsedMs": 1_000, "maximumClockRollbackMs": 300_000, "maximumCheckpointAgeMs": 3_600_000, "expectedReplicaGeneration": 7, "invalidatedDependencyDigests": []any{}, "witnessedCheckpoints": map[string]any{}}
	return authorityFixture{keys: keys, state: state, context: context, manifest: manifest, checkpoint: checkpoint}
}

func signedAuthorityUpdate(t *testing.T, fixture authorityFixture, manifestIDs, checkpointIDs []string) map[string]any {
	t.Helper()
	manifestSignatures := make([]any, 0, len(manifestIDs))
	for _, id := range manifestIDs {
		manifestSignatures = append(manifestSignatures, authoritySignature(t, fixture.keys[id], manifestDomain, fixture.manifest))
	}
	checkpointSignatures := make([]any, 0, len(checkpointIDs))
	for _, id := range checkpointIDs {
		checkpointSignatures = append(checkpointSignatures, authoritySignature(t, fixture.keys[id], checkpointDomain, fixture.checkpoint))
	}
	return map[string]any{"manifest": fixture.manifest, "manifestSignatures": manifestSignatures, "checkpoint": fixture.checkpoint, "checkpointSignatures": checkpointSignatures}
}

func rootRotationCandidate(t *testing.T, fixture authorityFixture) (map[string]any, []testAuthorityKey) {
	t.Helper()
	newKeys := []testAuthorityKey{newAuthorityKey(t, "root-new-1", "root", 2), newAuthorityKey(t, "root-new-2", "root", 2), newAuthorityKey(t, "root-new-3", "root", 2)}
	newWire := make([]any, 0, len(newKeys))
	for _, key := range newKeys {
		newWire = append(newWire, map[string]any{"keyId": key.id, "role": "root", "epoch": 2, "publicKeySpkiBase64url": key.spki})
	}
	rotation := map[string]any{"schemaId": "oracle.compatibility", "schemaMajor": 1, "schemaRevision": 0, "kind": "root_rotation", "oldEpoch": 1, "newEpoch": 2, "newRootThreshold": 2, "newKeys": newWire}
	oldSignatures := []any{authoritySignature(t, fixture.keys["root-old-1"], rootRotationDomain, rotation), authoritySignature(t, fixture.keys["root-old-2"], rootRotationDomain, rotation)}
	newSignatures := []any{authoritySignature(t, newKeys[0], rootRotationDomain, rotation), authoritySignature(t, newKeys[1], rootRotationDomain, rotation)}
	return map[string]any{"rotation": rotation, "oldSignatures": oldSignatures, "newSignatures": newSignatures}, newKeys
}

func TestOracleContractManifestAuthority(t *testing.T) {
	corpus := mustFixtureObject(t, "authority-corpus.json")
	cases := mustArray(corpus["cases"])
	if len(cases) != 21 {
		t.Fatalf("authority required set = %d", len(cases))
	}
	expectedDigests := mustObject(corpus["expected_next_state_digests"])
	for _, raw := range cases {
		row := mustObject(raw)
		id := mustStringDefault(row["id"])
		want := mustStringDefault(row["expected_code"])
		t.Run(id, func(t *testing.T) {
			fixture := buildAuthorityFixture(t)
			var decision Decision
			switch {
			case strings.HasPrefix(id, "authority-root-rotation"):
				candidate, _ := rootRotationCandidate(t, fixture)
				if strings.HasSuffix(id, "old-only") {
					candidate["newSignatures"] = []any{}
				}
				if strings.HasSuffix(id, "new-only") {
					candidate["oldSignatures"] = []any{}
				}
				decision = VerifyRootRotation(AuthorityInput{State: mustBytes(t, fixture.state), Candidate: mustBytes(t, candidate)})
			case id == "authority-emergency-revocation":
				revocation := map[string]any{"schemaId": "oracle.compatibility", "schemaMajor": 1, "schemaRevision": 0, "kind": "emergency_revocation", "version": 2, "keyEpoch": 1, "issuedAtMs": fixture.context["nowWallClockMs"], "expiresAtMs": int64(1_800_000_060_000), "revokedKeyIds": []any{"manifest-3"}, "reasonRef": "reason:key-compromise-fixture"}
				candidate := map[string]any{"revocation": revocation, "signatures": []any{authoritySignature(t, fixture.keys["revocation-1"], revocationDomain, revocation)}, "nowWallClockMs": fixture.context["nowWallClockMs"]}
				decision = VerifyEmergencyRevocation(AuthorityInput{State: mustBytes(t, fixture.state), Candidate: mustBytes(t, candidate)})
			default:
				manifestIDs := []string{"manifest-1", "manifest-2"}
				checkpointIDs := []string{"checkpoint-1"}
				if id == "authority-insufficient-threshold" {
					manifestIDs = []string{"manifest-1"}
				}
				if id == "authority-expired" {
					fixture.manifest["expiresAtMs"] = int64(1_799_999_999_999)
				}
				if id == "authority-parent-mismatch" {
					fixture.manifest["parentDigest"] = strings.Repeat("0", 64)
				}
				if id == "authority-policy-rollback" {
					fixture.manifest["policyVersion"] = 9
				}
				if id == "authority-revoked-key" {
					mustObject(mustObject(fixture.state["keys"])["manifest-1"])["revoked"] = true
				}
				if id == "authority-stale-checkpoint" {
					fixture.checkpoint["version"] = 5
				}
				if id == "authority-freeze" {
					fixture.checkpoint["issuedAtMs"] = int64(1_799_996_399_999)
				}
				if id == "authority-witness-mismatch" {
					fixture.manifest["witnessCheckpointDigest"] = strings.Repeat("0", 64)
				}
				if id == "authority-clock-rollback" {
					fixture.context["nowWallClockMs"] = int64(1_799_999_698_999)
				}
				if id == "authority-dependency-invalidated" {
					fixture.context["invalidatedDependencyDigests"] = cloneValue(t, fixture.manifest["invalidatingDependencyDigests"])
				}
				if id == "authority-replica-generation-conflict" {
					fixture.context["expectedReplicaGeneration"] = 8
				}
				manifestDigest, _ := authorityObjectDigest(manifestDomain, fixture.manifest)
				if id == "authority-mix-and-match" {
					fixture.checkpoint["manifestDigest"] = strings.Repeat("0", 64)
				} else {
					fixture.checkpoint["manifestDigest"] = manifestDigest
				}
				if id == "authority-witness-mismatch" {
					fixture.checkpoint["witnessCheckpointDigest"] = strings.Repeat("c", 64)
				} else {
					fixture.checkpoint["witnessCheckpointDigest"] = fixture.manifest["witnessCheckpointDigest"]
				}
				if id == "authority-split-view" {
					mustObject(fixture.context["witnessedCheckpoints"])["6"] = strings.Repeat("0", 64)
				}
				update := signedAuthorityUpdate(t, fixture, manifestIDs, checkpointIDs)
				if id == "authority-duplicate-signer" {
					signatures := mustArray(update["manifestSignatures"])
					update["manifestSignatures"] = append(signatures, cloneValue(t, signatures[0]))
				}
				if id == "authority-wrong-role" {
					update["manifestSignatures"] = []any{authoritySignature(t, fixture.keys["root-old-1"], manifestDomain, fixture.manifest)}
				}
				decision = VerifyManifestAuthorityUpdate(AuthorityInput{State: mustBytes(t, fixture.state), Candidate: mustBytes(t, update), Context: mustBytes(t, fixture.context)})
			}
			if decision.Code != want || decision.Allowed != (want == "authority_allow") {
				t.Fatalf("decision = %+v want=%s", decision, want)
			}
			if decision.Allowed {
				expected := mustStringDefault(expectedDigests[id])
				if decision.NextStateDigest != expected {
					t.Fatalf("next digest = %s want=%s", decision.NextStateDigest, expected)
				}
				if digest, err := TrustStateDigest(decision.NextState); err != nil || digest != decision.NextStateDigest {
					t.Fatalf("state digest replay = %s err=%v", digest, err)
				}
			}
		})
	}
	precedence := buildAuthorityFixture(t)
	for index := 0; index < 65; index++ {
		key := newAuthorityKey(t, "overflow-key-"+decimalKey(int64(index)), "manifest", 1)
		mustObject(precedence.state["keys"])[key.id] = authorityKeyWire(key)
	}
	precedence.context["nowWallClockMs"] = int64(1_799_999_698_999)
	update := signedAuthorityUpdate(t, precedence, []string{"manifest-1", "manifest-2"}, []string{"checkpoint-1"})
	if decision := VerifyManifestAuthorityUpdate(AuthorityInput{State: mustBytes(t, precedence.state), Candidate: mustBytes(t, update), Context: mustBytes(t, precedence.context)}); decision.Code != "authority_clock_rollback" {
		t.Fatalf("clock precedence = %+v", decision)
	}
	precedence.context["nowWallClockMs"] = int64(1_800_000_000_000)
	precedence.context["expectedReplicaGeneration"] = 8
	if decision := VerifyManifestAuthorityUpdate(AuthorityInput{State: mustBytes(t, precedence.state), Candidate: mustBytes(t, update), Context: mustBytes(t, precedence.context)}); decision.Code != "authority_replica_conflict" {
		t.Fatalf("replica precedence = %+v", decision)
	}
}

func TestOracleContractInterface(t *testing.T) {
	corpus := mustFixtureObject(t, "interface-corpus.json")
	fixtures := mustObject(corpus["fixtures"])
	expectedDigests := mustObject(corpus["expected_state_digests"])
	for _, raw := range mustArray(corpus["cases"]) {
		row := mustObject(raw)
		id := mustStringDefault(row["id"])
		kind := mustStringDefault(row["kind"])
		want := mustStringDefault(row["expected_code"])
		if kind == "replay" {
			continue
		}
		t.Run(id, func(t *testing.T) {
			var decision Decision
			switch kind {
			case "readiness":
				handshake := cloneMap(t, fixtures["readiness"])
				expected := cloneMap(t, fixtures["readiness_expected"])
				if id == "readiness-live-not-ready" {
					handshake["readiness"] = false
				}
				if id == "readiness-contract-mismatch" {
					handshake["contract_digest"] = strings.Repeat("0", 64)
				}
				if id == "readiness-revision-unsupported" {
					handshake["supported_contracts"] = []any{map[string]any{"schema_major": 1, "minimum_revision": 1, "maximum_revision": 1}}
				}
				decision = DecideReadiness(mustBytes(t, handshake), mustBytes(t, expected))
			case "lifecycle":
				state := cloneMap(t, fixtures["lifecycle_state"])
				operation := cloneMap(t, fixtures["lifecycle_operation"])
				if id == "lifecycle-register" {
					for _, field := range []string{"account_generation", "credential_generation", "proxy_generation", "profile_generation", "state_version"} {
						state[field] = 0
					}
					state["status"] = "absent"
					operation["operation"], operation["expected_state_version"], operation["next_state_version"] = "register", 0, 1
					for _, field := range []string{"account_generation", "credential_generation", "proxy_generation", "profile_generation"} {
						operation[field] = 1
					}
				}
				if id == "lifecycle-stale-cas" {
					operation["expected_state_version"] = 0
				}
				if id == "lifecycle-generation-regression" {
					operation["proxy_generation"] = 0
				}
				decision = DecideLifecycle(mustBytes(t, state), mustBytes(t, operation))
			case "lineage":
				state := cloneMap(t, fixtures["lineage_state"])
				candidate := cloneMap(t, fixtures["lineage_candidate"])
				if id == "lineage-root-mismatch" {
					candidate["root_task_ref"] = "task:root:other"
				}
				if id == "migration-sequence-stale" {
					candidate["migration_sequence"] = state["migration_sequence"]
				}
				decision = DecideTaskLineage(mustBytes(t, state), mustBytes(t, candidate), 1_800_000_000_000)
			case "outcome":
				name := "outcome_rate_limit"
				if id == "outcome-partial-tool-side-effect" {
					name = "outcome_partial"
				}
				decision = DecideOutcome(mustBytes(t, fixtures[name]))
			}
			allowedCode := want == "interface_allow" || want == "interface_terminal_no_retry" || want == "interface_sub2api_retry" || want == "interface_gateway_retry"
			if decision.Code != want || decision.Allowed != allowedCode {
				t.Fatalf("decision = %+v want=%s", decision, want)
			}
			if expected, ok := stringValue(expectedDigests[id]); ok && decision.NextStateDigest != expected {
				t.Fatalf("next digest = %s want=%s", decision.NextStateDigest, expected)
			}
		})
	}
	wrong := cloneMap(t, fixtures["readiness"])
	wrong["schema_id"] = "oracle.attacker"
	if decision := DecideReadiness(mustBytes(t, wrong), mustBytes(t, fixtures["readiness_expected"])); decision.Code != "interface_schema_unsupported" {
		t.Fatalf("wrong schema = %+v", decision)
	}
}

func replayCommand(operation string, expectedGeneration, now, expires int64) map[string]any {
	return map[string]any{"key_epoch": 11, "capability_id": "capability:fixture:1", "attempt_id": "attempt:fixture:1", "nonce": "nonce:fixture:1", "operation": operation, "expected_generation": expectedGeneration, "now_ms": now, "expires_at_ms": expires}
}

func TestOracleContractReplay(t *testing.T) {
	expected := mustObject(mustFixtureObject(t, "expected-results.json")["replay_state_digests"])
	initial := map[string]any{"ledger_generation": 0, "entries": map[string]any{}}
	reserved := ExecuteReplay(mustBytes(t, initial), mustBytes(t, replayCommand("reserve", 0, 1_800_000_000_000, 1_800_000_060_000)))
	requireAllowed(t, reserved, "replay_reserved")
	if reserved.NextStateDigest != mustStringDefault(expected["reserved"]) {
		t.Fatalf("reserved digest = %s", reserved.NextStateDigest)
	}
	committed := ExecuteReplay(reserved.NextState, mustBytes(t, replayCommand("commit", 1, 1_800_000_000_100, 1_800_000_060_000)))
	requireAllowed(t, committed, "replay_committed")
	if committed.NextStateDigest != mustStringDefault(expected["committed"]) {
		t.Fatalf("committed digest = %s", committed.NextStateDigest)
	}
	for name, decision := range map[string]Decision{
		"reuse": ExecuteReplay(reserved.NextState, mustBytes(t, replayCommand("reserve", 1, 1_800_000_000_100, 1_800_000_060_000))),
		"stale": ExecuteReplay(reserved.NextState, mustBytes(t, replayCommand("commit", 0, 1_800_000_000_100, 1_800_000_060_000))),
	} {
		want := "replay_rejected"
		if name == "stale" {
			want = "replay_replica_conflict"
		}
		if decision.Allowed || decision.Code != want {
			t.Fatalf("%s = %+v", name, decision)
		}
	}
	requireAllowed(t, ExecuteReplay(reserved.NextState, mustBytes(t, replayCommand("expire", 1, 1_800_000_060_000, 1_800_000_060_000))), "replay_expired")
	requireAllowed(t, ExecuteReplay(reserved.NextState, mustBytes(t, replayCommand("revoke", 1, 1_800_000_000_100, 1_800_000_060_000))), "replay_revoked")
}

func sidecarKeyring(t *testing.T, keys ...testAuthorityKey) []byte {
	t.Helper()
	items := make(map[string]any)
	for _, key := range keys {
		items[key.id] = authorityKeyWire(key)
	}
	return mustBytes(t, map[string]any{"keys": items})
}

func TestOracleContractSidecar(t *testing.T) {
	corpus := mustFixtureObject(t, "canonicalization-corpus.json")
	unsigned := cloneMap(t, corpus["sidecar_unsigned_envelope"])
	key := newAuthorityKey(t, "sidecar-key-11", "sidecar_capability", 11)
	signingBytes, err := sidecarSigningBytes(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	expected := mustObject(mustObject(mustFixtureObject(t, "expected-results.json")["canonical_results"])["sidecar_unsigned_envelope"])
	if hex.EncodeToString(signingBytes[len(sidecarCapabilityDomain):]) != mustStringDefault(expected["canonical_hex"]) {
		t.Fatal("unsigned sidecar CBOR drift")
	}
	signature := ed25519.Sign(key.private, signingBytes)
	envelope, err := encodeSignedSidecar(unsigned, signature)
	if err != nil {
		t.Fatal(err)
	}
	capability := mustBytes(t, unsigned)
	requireAllowed(t, ValidateSidecarEnvelope(envelope, nil), "sidecar_capability_allow")
	requireAllowed(t, VerifySidecarCapability(envelope, capability, sidecarKeyring(t, key), 1_800_000_000_100), "sidecar_capability_allow")
	for field := range unsigned {
		mutated := cloneMap(t, unsigned)
		switch value := mutated[field].(type) {
		case string:
			mutated[field] = value + "-changed"
		case []any:
			for left, right := 0, len(value)-1; left < right; left, right = left+1, right-1 {
				value[left], value[right] = value[right], value[left]
			}
		default:
			if number, ok := int64Value(value); ok {
				mutated[field] = number + 1
			}
		}
		mutatedEnvelope, encodeErr := encodeSignedSidecar(mutated, signature)
		if encodeErr == nil {
			decision := VerifySidecarCapability(mutatedEnvelope, mustBytes(t, mutated), sidecarKeyring(t, key), 1_800_000_000_100)
			if decision.Allowed {
				t.Fatalf("field %s was not signature-bound", field)
			}
		}
	}
	if decision := VerifySidecarCapability(envelope, capability, sidecarKeyring(t), 1_800_000_000_100); decision.Code != "sidecar_key_not_found" {
		t.Fatalf("missing key = %+v", decision)
	}
	epoch := key
	epoch.epoch = 12
	if decision := VerifySidecarCapability(envelope, capability, sidecarKeyring(t, epoch), 1_800_000_000_100); decision.Code != "sidecar_key_epoch_mismatch" {
		t.Fatalf("epoch = %+v", decision)
	}
	revokedWire := authorityKeyWire(key)
	revokedWire["revoked"] = true
	revokedRing := mustBytes(t, map[string]any{"keys": map[string]any{key.id: revokedWire}})
	if decision := VerifySidecarCapability(envelope, capability, revokedRing, 1_800_000_000_100); decision.Code != "sidecar_key_revoked" {
		t.Fatalf("revoked = %+v", decision)
	}
	reused := key
	reused.id, reused.role = "manifest-key-11", "manifest"
	if decision := VerifySidecarCapability(envelope, capability, sidecarKeyring(t, key, reused), 1_800_000_000_100); decision.Code != "sidecar_key_role_reuse" {
		t.Fatalf("role reuse = %+v", decision)
	}
	if decision := VerifySidecarCapability(envelope, capability, sidecarKeyring(t, key), 1_800_000_060_001); decision.Code != "sidecar_capability_expired" {
		t.Fatalf("expired = %+v", decision)
	}
}

func TestOracleContractMutation(t *testing.T) {
	invalid := []string{"18446744073709551616", "9223372036854775808", "4097", strings.Repeat("9", 1_000), "-1", "+1", "01", "", "x"}
	for _, segment := range invalid {
		_, err := ParseBoundedPointerIndex(segment, 4, false)
		requireCode(t, err, CodeMutationPointer)
	}
	source := []byte(`{"a":[{"b":[1,2]}]}`)
	mutated, err := ApplyMutation(source, MutationOperation{Kind: "remove_pointer", Pointer: "/a/0/b/0"})
	if err != nil || string(mutated) != `{"a":[{"b":[2]}]}` {
		t.Fatalf("nested mutation = %q err=%v", mutated, err)
	}
	_, err = ApplyMutation(source, MutationOperation{Kind: "set_pointer", Pointer: "/a/~2", Value: 1})
	requireCode(t, err, CodeMutationPointer)
	overlay := make(map[string]virtualMutationFile, len(mirrorDigests))
	for name := range mirrorDigests {
		data, readErr := readContractFile(filepath.Join(mirrorRoot, name), maxJSONBytes)
		if readErr != nil {
			t.Fatal(readErr)
		}
		overlay[name] = virtualMutationFile{data: data, mode: 0o600}
	}
	if decision := executeVirtualMutationSubject("mirror", overlay); !decision.Allowed {
		t.Fatalf("virtual mirror = %+v", decision)
	}
	if err := applyVirtualFileMutation(overlay, MutationOperation{Kind: "replace_with_symlink", Path: "contract-index.json", Target: "contract.schema.json"}); err != nil {
		t.Fatal(err)
	}
	if decision := executeVirtualMutationSubject("mirror", overlay); decision.Code != "contract_symlink" {
		t.Fatalf("virtual symlink = %+v", decision)
	}
	if _, err := mutationOperationFromMap(map[string]any{"kind": "add_file", "path": "extra.json", "bytes_base64": "e30=", "mode": int64(0o600)}); err != nil {
		t.Fatalf("add-file descriptor rejected: %v", err)
	}
	if err := applyVirtualFileMutation(overlay, MutationOperation{Kind: "replace_with_symlink", Path: "contract-index.json", Target: "../outside"}); err == nil {
		t.Fatal("escaping virtual symlink accepted")
	}
	root := "testdata/rebaseline/v1"
	corpus, err := os.ReadFile(filepath.Join(root, "mutation-corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := ExecuteMutationCorpus(root, corpus, nil)
	if err != nil || len(first) != 1 || !first[0].Allowed || first[0].Code != "admission_allow" {
		t.Fatalf("committed mutation corpus = %+v err=%v", first, err)
	}
	second, err := ExecuteMutationCorpus(root, corpus, nil)
	if err != nil || !bytes.Equal(mustBytes(t, mutationResultsValue(first)), mustBytes(t, mutationResultsValue(second))) {
		t.Fatalf("mutation execution is not deterministic: %v", err)
	}
	if decision := executeAdmissionMutation([]byte(`{"kind":"synthetic-control","version":2}`)); decision.Allowed || decision.Code != "admission_schema_invalid" {
		t.Fatalf("mutated admission control bypassed validator: %+v", decision)
	}
	linkRoot := t.TempDir()
	regular := filepath.Join(linkRoot, "regular.json")
	if err := os.WriteFile(regular, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(linkRoot, "hard.json")
	if err := os.Link(regular, hard); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegularFile(regular, 16); err == nil {
		t.Fatal("multi-link mutation source accepted")
	}
	realDir := filepath.Join(linkRoot, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	componentFile := filepath.Join(realDir, "source.json")
	if err := os.WriteFile(componentFile, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedDir := filepath.Join(linkRoot, "linked")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegularFile(filepath.Join(linkedDir, "source.json"), 16); err == nil {
		t.Fatal("symlink-component mutation source accepted")
	}
}

func mutationResultsValue(results []MutationResult) []any {
	rows := make([]any, 0, len(results))
	for _, result := range results {
		rows = append(rows, map[string]any{"case_id": result.CaseID, "allowed": result.Allowed, "code": result.Code, "output_sha256": result.OutputSHA256})
	}
	return rows
}

func TestOracleContractCrossRepo(t *testing.T) {
	predecessor := filepath.Join("..", "service", "testdata", "cc_gateway_formal_pool_contract", "vectors.json")
	if decision := InspectMirror(mirrorRoot, mirrorRoot, predecessor); !decision.Allowed {
		t.Fatalf("mirror = %+v", decision)
	}
	if decision := ValidateContractIndex(mirrorRoot); !decision.Allowed {
		t.Fatalf("index = %+v", decision)
	}
	if decision := InspectMirror(filepath.Join(t.TempDir(), "missing"), mirrorRoot, predecessor); decision.Allowed || decision.Code != CodeContractBundle {
		t.Fatalf("missing mirror = %+v", decision)
	}
	record := validCrossRepoRecord(t)
	if decision := ValidateCrossRepoRecord(crossRecordBytes(t, record)); !decision.Allowed {
		t.Fatalf("record = %+v", decision)
	}
	if decision := ValidateCrossRepoRecord(mustBytes(t, record)); decision.Allowed || decision.Code != "cross_repo_binding_mismatch" {
		t.Fatalf("record without final LF = %+v", decision)
	}
	pretty, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if decision := ValidateCrossRepoRecord(append(pretty, '\n')); decision.Allowed || decision.Code != "cross_repo_binding_mismatch" {
		t.Fatalf("non-JCS record = %+v", decision)
	}
	record["file_count"] = 3_064
	bindCrossRepoDigest(t, record)
	if decision := ValidateCrossRepoRecord(crossRecordBytes(t, record)); decision.Code != "authority_diagnostic_promotion" {
		t.Fatalf("diagnostic promotion = %+v", decision)
	}
	record = validCrossRepoRecord(t)
	mustObject(mustObject(record["authority"])["cc"])["selected_remote_oid"] = strings.Repeat("0", 40)
	bindCrossRepoDigest(t, record)
	if decision := ValidateCrossRepoRecord(crossRecordBytes(t, record)); decision.Code != "cross_repo_binding_mismatch" {
		t.Fatalf("unbound selected OID = %+v", decision)
	}
	record = validCrossRepoRecord(t)
	result := mustObject(record["result"])
	rows := mustArray(result["case_rows"])
	result["case_rows"] = rows[:len(rows)-1]
	result["decisions_sha256"] = SHA256Hex(append(mustBytes(t, rows[:len(rows)-1]), '\n'))
	bindCrossRepoDigest(t, record)
	if decision := ValidateCrossRepoRecord(crossRecordBytes(t, record)); decision.Code != "cross_repo_result_mismatch" {
		t.Fatalf("incomplete result = %+v", decision)
	}
	record = validCrossRepoRecord(t)
	mustObject(record["result"])["required_set_sha256"] = strings.Repeat("0", 64)
	bindCrossRepoDigest(t, record)
	if decision := ValidateCrossRepoRecord(crossRecordBytes(t, record)); decision.Code != "cross_repo_result_mismatch" {
		t.Fatalf("caller-selected required set = %+v", decision)
	}
	record = validCrossRepoRecord(t)
	mustObject(mustArray(mustObject(record["commit_dag"])["nodes"])[0])["head"] = nil
	bindCrossRepoDigest(t, record)
	if decision := ValidateCrossRepoRecord(crossRecordBytes(t, record)); decision.Code != "cross_repo_binding_mismatch" {
		t.Fatalf("null DAG binding = %+v", decision)
	}
	record = validCrossRepoRecord(t)
	mustObject(mustObject(record["authority"])["cc"])["selected_remote_oid"] = strings.Repeat("1", 64)
	bindCrossRepoDigest(t, record)
	if decision := ValidateCrossRepoRecord(crossRecordBytes(t, record)); decision.Code != "cross_repo_binding_mismatch" {
		t.Fatalf("digest accepted as OID = %+v", decision)
	}
	for _, leak := range []string{"prefix Bearer secret-token", "x Basic dXNlcjpwYXNz", "-----BEGIN RSA PRIVATE KEY-----", "-----begin openssh private key-----", "api_key=secret", "access-token = secret", "password: secret"} {
		if !containsLeakString(leak) {
			t.Fatalf("leak not detected: %q", leak)
		}
	}
}

func validCrossRepoRecord(t *testing.T) map[string]any {
	t.Helper()
	files := make([]any, 0, len(mirrorDigests))
	names := make([]string, 0, len(mirrorDigests))
	for name := range mirrorDigests {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		files = append(files, map[string]any{"relative_path": name, "sha256": mirrorDigests[name]})
	}
	caseSpecs, mutationSpecs, ok := frozenResultSpecs()
	if !ok {
		t.Fatal("frozen result specs unavailable")
	}
	caseRows := rowsForSpecs(caseSpecs)
	mutationRows := rowsForSpecs(mutationSpecs)
	caseBytes := append(mustBytes(t, caseRows), '\n')
	mutationBytes := append(mustBytes(t, mutationRows), '\n')
	requiredDigest, ok := frozenRequiredSetDigest(caseSpecs, mutationSpecs)
	if !ok {
		t.Fatal("required-set digest unavailable")
	}
	ids := []string{"C0", "S0", "S1", "R1", "I1", "SR", "C1", "CR"}
	roles := []string{"merge-this-cc-docs-amendment", "fresh-true-sub-mandatory-entry-and-docs-plan", "independent-review-and-merge-true-sub-docs-plan", "compileable-fail-closed-behavioral-red-scaffold", "single-implementation-wave", "independent-exact-head-true-sub-review", "cc-checker-integration", "cross-repo-exact-head-review-and-controller-decision"}
	nodes := make([]any, 0, len(ids))
	edges := make([]any, 0, len(ids)-1)
	for index, id := range ids {
		parents := []any{}
		if index > 0 {
			parents = []any{ids[index-1]}
			edges = append(edges, []any{ids[index-1], id})
		}
		head, tree := strings.Repeat(decimalKey(int64(index+1)), 40), strings.Repeat(decimalKey(int64(index+2)), 40)
		if id == "C0" {
			head, tree = "debe0360384132d6e66c0296219ea6066193e187", "ffca2a0a892b2292b486533089f3276f28b39d4e"
		}
		if id == "R1" {
			head, tree = "795c1f810b5647840fec508951cfc3272066d8b6", "efb99a079e76817a38a9a48b053cdc6504e37025"
		}
		if id == "I1" {
			head, tree = strings.Repeat("1", 40), strings.Repeat("2", 40)
		}
		nodes = append(nodes, map[string]any{"id": id, "role": roles[index], "parent_ids": parents, "head": head, "tree": tree})
	}
	now := time.Now().UnixMilli()
	digest := strings.Repeat("1", 64)
	oid := strings.Repeat("1", 40)
	record := map[string]any{
		"schema_id": "oracle.cross_repo_record", "schema_major": 1, "schema_revision": 0, "kind": "oracle_contract_rebaseline",
		"authority": map[string]any{
			"command_id": "command:boundary-a", "reviewer_model": "gpt-5.6-sol",
			"cc":  map[string]any{"repository_url": "https://github.com/muqihang/cc-gateway.git", "selected_remote_name": "muqihang", "selected_remote_ref": "refs/remotes/muqihang/main", "selected_remote_oid": "debe0360384132d6e66c0296219ea6066193e187", "commit": "debe0360384132d6e66c0296219ea6066193e187", "tree": "ffca2a0a892b2292b486533089f3276f28b39d4e", "amendment_sha256": "eeaefeddbfe740003288f9d8ec8ba4673b57cca91f4fc3bf5cea5db02feaefaf"},
			"sub": map[string]any{"repository_url": "https://github.com/Wei-Shaw/sub2api.git", "selected_local_ref": "refs/heads/codex/native-search-gateway", "selected_local_oid": "3ac410ea02edc53c3925f28eddcbc22b51c0a137", "commit": "3ac410ea02edc53c3925f28eddcbc22b51c0a137", "tree": "f7d51fb57c64fbaf6e2db3a7a2d423a491d5788d", "parent": "04e42ae0f6c556daad21ac393eb284585092e805", "ancestor": "fc0b1989d7ba9ce06ff151b17c94b50df4170a93", "go_mod_sha256": "e637999a38f974c9172c8f69c8fbb9c0d727bacf257558307e97e927cbb468de", "go_sum_sha256": "d3e1fd1510b41f218136b719fdf2c4ef239b05650d3b575fb93c18f25f3dc981", "go_directive": "1.26.5", "predecessor_relative_path": "backend/internal/service/testdata/cc_gateway_formal_pool_contract/vectors.json", "predecessor_sha256": predecessorSHA256, "codegraph_config_sha256": "a7f3ad7c17d655f9d2494b5b05e55ceb4ea9c7667456ff785c5f2a9291c3783a", "codegraph_version": "1.1.6", "codegraph_extraction_revision": 24, "selection": map[string]any{"mode": "total_controller_local_override", "selection_override_sha256": digest, "selection_override_controller_id": "controller:master", "selection_override_task_id": "task:boundary-a", "selection_override_issued_at_ms": now, "selection_override_decision": "authorize_refs/heads/codex/native-search-gateway_at_3ac410ea02edc53c3925f28eddcbc22b51c0a137"}, "sub_plan_commit": oid, "sub_plan_tree": oid, "sub_plan_sha256": digest, "r1_commit": "795c1f810b5647840fec508951cfc3272066d8b6", "r1_tree": "efb99a079e76817a38a9a48b053cdc6504e37025", "i1_commit": oid, "i1_tree": strings.Repeat("2", 40)},
		},
		"bundle":       map[string]any{"files": files, "contract_index_sha256": contractIndexSHA256, "predecessor_sha256": predecessorSHA256, "schema_range": "1:0-0", "mirror_root": "backend/internal/oracleevidence/testdata/oracle_lab_contract/v1", "framing": "core-raw-exact;record-jcs-final-lf"},
		"commit_dag":   map[string]any{"nodes": nodes, "edges": edges},
		"result":       map[string]any{"case_rows": caseRows, "mutation_rows": mutationRows, "decisions_sha256": SHA256Hex(caseBytes), "mutation_results_sha256": SHA256Hex(mutationBytes), "required_set_sha256": requiredDigest, "stable_code_count": 119, "stable_code_set_sha256": stableCodesSHA256, "semantic_surfaces": map[string]any{"strict_json": true, "jcs": true, "normalization": true, "cbor": true, "schema": true, "admission": true, "authority": true, "interface": true, "replay": true, "sidecar": true}, "protected_file_count": 0, "protected_node_count": 0, "egress_count": 0, "command_ids": []any{"cc-focused-contract-suite-v1", "sub-focused-oracleevidence-v1"}},
		"review":       map[string]any{"sub": map[string]any{"task_id": "task:sub-review", "model": "gpt-5.6-sol", "artifact_sha256": digest, "critical": 0, "important": 0, "verdict": "PLAN_REVIEW_PASS"}, "cross": map[string]any{"task_id": "task:cross-review", "model": "gpt-5.6-sol", "artifact_sha256": digest, "critical": 0, "important": 0, "verdict": "CROSS_REPO_PASS"}},
		"issued_at_ms": now, "expires_at_ms": now + crossRepoLeaseMS, "record_digest": digest,
	}
	bindCrossRepoDigest(t, record)
	return record
}

func bindCrossRepoDigest(t *testing.T, record map[string]any) {
	t.Helper()
	unsigned := cloneMap(t, record)
	delete(unsigned, "record_digest")
	record["record_digest"] = SHA256Hex(append(mustBytes(t, unsigned), '\n'))
}

func rowsForSpecs(specs []frozenDecisionSpec) []any {
	rows := make([]any, len(specs))
	for index, spec := range specs {
		rows[index] = map[string]any{
			"case_id": spec.id, "allowed": spec.allowed, "code": spec.code,
			"next_state_digest": spec.nextDigest, "canonical_hex": spec.canonicalHex,
		}
	}
	return rows
}

func crossRecordBytes(t *testing.T, record map[string]any) []byte {
	t.Helper()
	return append(mustBytes(t, record), '\n')
}

func mustArray(value any) []any {
	result, _ := arrayValue(value)
	return result
}

func mustStringDefault(value any) string {
	result, _ := stringValue(value)
	return result
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for position := index; position > 0 && values[position] < values[position-1]; position-- {
			values[position], values[position-1] = values[position-1], values[position]
		}
	}
}
