package oracleevidence

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
)

var sidecarCapabilityDomain = []byte("oracle-sidecar-capability-v1\x00")

var sidecarUnsignedFields = []string{
	"schema_id", "schema_major", "schema_revision", "key_epoch", "capability_id", "attempt_id", "nonce",
	"issued_at_ms", "deadline_ms", "method", "authority", "normalized_path_query", "ordered_headers_sha256",
	"body_sha256", "content_length", "content_encoding", "profile_generation", "proxy_generation",
	"credential_generation", "transport_cell_generation", "contract_digest", "manifest_digest",
	"destination_policy_generation", "destination_class", "allowed_destinations", "response_policy_ref",
	"retry_owner", "key_id", "key_role",
}

var sidecarDigestFields = []string{"ordered_headers_sha256", "body_sha256", "contract_digest", "manifest_digest"}

func validateSidecarEnvelopeImpl(envelope []byte, _ *SchemaSet) Decision {
	payload, err := unframeCBOR(envelope)
	if err != nil {
		return decisionFromError(err, "sidecar_capability_decode_invalid")
	}
	decoded, err := decodeDeterministicCBORImpl(payload)
	if err != nil {
		return decisionFromError(err, "sidecar_capability_decode_invalid")
	}
	record, ok := objectValue(decoded)
	if !ok {
		return Decision{Code: "sidecar_capability_schema_invalid"}
	}
	unsigned := cloneObject(record)
	if signature, present := unsigned["signature"]; present {
		raw, rawOK := signature.([]byte)
		if !rawOK || len(raw) != ed25519.SignatureSize {
			return Decision{Code: "sidecar_capability_schema_invalid"}
		}
		delete(unsigned, "signature")
	}
	if _, ok := strictWireSidecarUnsigned(unsigned); !ok {
		return Decision{Code: "sidecar_capability_schema_invalid"}
	}
	return Decision{Allowed: true, Code: "sidecar_capability_allow"}
}

func verifySidecarCapabilityImpl(envelope, capability, keyring []byte, nowMS int64) Decision {
	payload, err := unframeCBOR(envelope)
	if err != nil {
		return decisionFromError(err, "sidecar_capability_decode_invalid")
	}
	decoded, err := decodeDeterministicCBORImpl(payload)
	if err != nil {
		return decisionFromError(err, "sidecar_capability_decode_invalid")
	}
	record, ok := objectValue(decoded)
	if !ok {
		return Decision{Code: "sidecar_capability_schema_invalid"}
	}
	signature, signatureOK := record["signature"].([]byte)
	unsignedWire := cloneObject(record)
	delete(unsignedWire, "signature")
	unsigned, unsignedOK := strictWireSidecarUnsigned(unsignedWire)
	if !signatureOK || len(signature) != ed25519.SignatureSize || !unsignedOK {
		return Decision{Code: "sidecar_capability_schema_invalid"}
	}
	expected, expectedOK := strictObject(capability)
	if !expectedOK || !strictSidecarUnsigned(expected) {
		return Decision{Code: "sidecar_capability_schema_invalid"}
	}
	expectedCanonical, _ := canonicalizeValueImpl(expected)
	unsignedCanonical, _ := canonicalizeValueImpl(unsigned)
	if !bytes.Equal(expectedCanonical, unsignedCanonical) {
		return Decision{Code: "sidecar_signature_invalid"}
	}
	issued, _ := int64Value(unsigned["issued_at_ms"])
	deadline, _ := int64Value(unsigned["deadline_ms"])
	if issued > nowMS || deadline < nowMS || deadline < issued {
		return Decision{Code: "sidecar_capability_expired"}
	}
	keys, keyOK := sidecarKeys(keyring)
	if !keyOK {
		return Decision{Code: "sidecar_capability_schema_invalid"}
	}
	keyID, _ := stringValue(unsigned["key_id"])
	key, exists := keys[keyID]
	if !exists {
		return Decision{Code: "sidecar_key_not_found"}
	}
	if key.role != "sidecar_capability" {
		return Decision{Code: "sidecar_key_role_invalid"}
	}
	epoch, _ := int64Value(unsigned["key_epoch"])
	if key.epoch != epoch {
		return Decision{Code: "sidecar_key_epoch_mismatch"}
	}
	if key.revoked {
		return Decision{Code: "sidecar_key_revoked"}
	}
	fingerprint := sha256HexImpl(key.public)
	for candidateID, candidate := range keys {
		if candidateID != keyID && candidate.role != "sidecar_capability" && sha256HexImpl(candidate.public) == fingerprint {
			return Decision{Code: "sidecar_key_role_reuse"}
		}
	}
	signingBytes, signingErr := sidecarSigningBytes(unsigned)
	if signingErr != nil || !ed25519.Verify(key.public, signingBytes, signature) {
		return Decision{Code: "sidecar_signature_invalid"}
	}
	return Decision{Allowed: true, Code: "sidecar_capability_allow"}
}

func sidecarSigningBytes(unsigned map[string]any) ([]byte, error) {
	if !strictSidecarUnsigned(unsigned) {
		return nil, contractErr("sidecar_capability_schema_invalid")
	}
	wire := cloneObject(unsigned)
	for _, field := range sidecarDigestFields {
		digest, _ := stringValue(unsigned[field])
		decoded, err := decodeHex(digest)
		if err != nil || len(decoded) != 32 {
			return nil, contractErr("sidecar_capability_schema_invalid")
		}
		wire[field] = decoded
	}
	encoded, err := encodeDeterministicCBOR(wire)
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), sidecarCapabilityDomain...), encoded...), nil
}

func encodeSignedSidecar(unsigned map[string]any, signature []byte) ([]byte, error) {
	if len(signature) != ed25519.SignatureSize {
		return nil, contractErr("sidecar_capability_schema_invalid")
	}
	wire := cloneObject(unsigned)
	for _, field := range sidecarDigestFields {
		digest, _ := stringValue(unsigned[field])
		decoded, err := decodeHex(digest)
		if err != nil || len(decoded) != 32 {
			return nil, contractErr("sidecar_capability_schema_invalid")
		}
		wire[field] = decoded
	}
	wire["signature"] = append([]byte(nil), signature...)
	payload, err := encodeDeterministicCBOR(wire)
	if err != nil {
		return nil, err
	}
	return frameCBOR(payload)
}

func strictWireSidecarUnsigned(wire map[string]any) (map[string]any, bool) {
	application := cloneObject(wire)
	for _, field := range sidecarDigestFields {
		digest, ok := application[field].([]byte)
		if !ok || len(digest) != 32 {
			return nil, false
		}
		application[field] = encodeHex(digest)
	}
	return application, strictSidecarUnsigned(application)
}

func strictSidecarUnsigned(value map[string]any) bool {
	if !exactKeys(value, sidecarUnsignedFields...) {
		return false
	}
	schemaID, _ := stringValue(value["schema_id"])
	major, majorOK := int64Value(value["schema_major"])
	revision, revisionOK := int64Value(value["schema_revision"])
	method, _ := stringValue(value["method"])
	role, _ := stringValue(value["key_role"])
	if schemaID != "oracle.sidecar.capability" || !majorOK || major != 1 || !revisionOK || revision != 0 || method != "POST" || role != "sidecar_capability" {
		return false
	}
	for _, field := range []string{"key_epoch", "issued_at_ms", "deadline_ms", "content_length", "profile_generation", "proxy_generation", "credential_generation", "transport_cell_generation", "destination_policy_generation"} {
		if !generation(value[field]) {
			return false
		}
	}
	for _, field := range []string{"capability_id", "attempt_id", "nonce", "response_policy_ref", "key_id"} {
		if !safeRef(value[field]) {
			return false
		}
	}
	authority, authorityOK := stringValue(value["authority"])
	pathQuery, pathOK := stringValue(value["normalized_path_query"])
	if !authorityOK || authority == "" || len(authority) > 512 || !pathOK || pathQuery == "" || len(pathQuery) > 8_192 || pathQuery[0] != '/' {
		return false
	}
	for _, field := range sidecarDigestFields {
		digest, ok := stringValue(value[field])
		if !ok || !isSHA256(digest) {
			return false
		}
	}
	encoding, _ := stringValue(value["content_encoding"])
	destinationClass, _ := stringValue(value["destination_class"])
	retryOwner, _ := stringValue(value["retry_owner"])
	if !containsString([]string{"identity", "gzip", "br", "zstd"}, encoding) || !containsString([]string{"public_provider", "approved_proxy"}, destinationClass) || !containsString([]string{"none", "cc_gateway", "sub2api"}, retryOwner) {
		return false
	}
	destinations, ok := arrayValue(value["allowed_destinations"])
	if !ok || len(destinations) < 1 || len(destinations) > 16 {
		return false
	}
	for _, rawDestination := range destinations {
		destination, ok := objectValue(rawDestination)
		if !ok || !exactKeys(destination, "host", "port") {
			return false
		}
		host, hostOK := stringValue(destination["host"])
		port, portOK := int64Value(destination["port"])
		if !hostOK || host == "" || !portOK || port < 1 || port > 65_535 {
			return false
		}
	}
	return true
}

func sidecarKeys(input []byte) (map[string]authorityKey, bool) {
	keyring, ok := strictObject(input)
	if !ok || !exactKeys(keyring, "keys") {
		return nil, false
	}
	rawKeys, ok := objectValue(keyring["keys"])
	if !ok || len(rawKeys) > 64 {
		return nil, false
	}
	result := make(map[string]authorityKey, len(rawKeys))
	for mapKey, rawValue := range rawKeys {
		value, ok := objectValue(rawValue)
		if !ok || !exactKeys(value, "keyId", "role", "epoch", "revoked", "publicKeySpkiBase64url") {
			return nil, false
		}
		keyID, _ := stringValue(value["keyId"])
		role, _ := stringValue(value["role"])
		epoch, epochOK := int64Value(value["epoch"])
		revoked, revokedOK := boolValue(value["revoked"])
		spki, spkiOK := stringValue(value["publicKeySpkiBase64url"])
		if keyID != mapKey || !containsString([]string{"root", "manifest", "checkpoint", "revocation", "sidecar_capability"}, role) || !epochOK || !revokedOK || !spkiOK {
			return nil, false
		}
		der, decodeErr := base64.RawURLEncoding.Strict().DecodeString(spki)
		parsed, parseErr := x509.ParsePKIXPublicKey(der)
		public, publicOK := parsed.(ed25519.PublicKey)
		if decodeErr != nil || parseErr != nil || !publicOK {
			return nil, false
		}
		result[keyID] = authorityKey{id: keyID, role: role, epoch: epoch, revoked: revoked, public: public, spkiText: spki}
	}
	return result, true
}

func decodeHex(value string) ([]byte, error) {
	if len(value)%2 != 0 {
		return nil, contractErr("sidecar_capability_schema_invalid")
	}
	result := make([]byte, len(value)/2)
	for index := 0; index < len(result); index++ {
		high, highOK := hexNibble(value[index*2])
		low, lowOK := hexNibble(value[index*2+1])
		if !highOK || !lowOK {
			return nil, contractErr("sidecar_capability_schema_invalid")
		}
		result[index] = high<<4 | low
	}
	return result, nil
}

func encodeHex(value []byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, current := range value {
		result[index*2] = digits[current>>4]
		result[index*2+1] = digits[current&0x0f]
	}
	return string(result)
}

func hexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	default:
		return 0, false
	}
}
