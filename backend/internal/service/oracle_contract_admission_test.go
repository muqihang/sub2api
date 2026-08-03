package service

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

type oracleAdmissionCorpus struct {
	BaseCertificate      map[string]any             `json:"base_certificate"`
	BaseContext          map[string]any             `json:"base_context"`
	NegativeCapabilities OracleNegativeCapabilities `json:"negative_capabilities"`
	Cases                []struct {
		ID       string `json:"id"`
		Mutation *struct {
			Target string `json:"target"`
			Set    string `json:"set"`
			Remove string `json:"remove"`
			Add    string `json:"add"`
			Value  any    `json:"value"`
		} `json:"mutation"`
		ExpectedCode string `json:"expected_code"`
	} `json:"cases"`
}

func oracleCloneMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func oracleMutationParent(t *testing.T, root map[string]any, dotted string) (map[string]any, string) {
	t.Helper()
	parts := strings.Split(dotted, ".")
	key := parts[len(parts)-1]
	var current any = root
	for _, part := range parts[:len(parts)-1] {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil {
				t.Fatal(err)
			}
			current = typed[index]
		default:
			t.Fatalf("invalid mutation path %s", dotted)
		}
	}
	parent, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("mutation path %s does not resolve to an object", dotted)
	}
	return parent, key
}

func applyOracleMutation(t *testing.T, root map[string]any, mutation struct {
	Target string `json:"target"`
	Set    string `json:"set"`
	Remove string `json:"remove"`
	Add    string `json:"add"`
	Value  any    `json:"value"`
}) {
	t.Helper()
	if mutation.Remove != "" {
		parent, key := oracleMutationParent(t, root, mutation.Remove)
		delete(parent, key)
	} else if mutation.Add != "" {
		root[mutation.Add] = true
	} else if mutation.Set != "" {
		parent, key := oracleMutationParent(t, root, mutation.Set)
		parent[key] = mutation.Value
	}
}

func TestOracleContractAdmission(t *testing.T) {
	raw, err := os.ReadFile("testdata/oracle_lab_contract/v1/coherence-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus oracleAdmissionCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range corpus.Cases {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			certificate := oracleCloneMap(t, corpus.BaseCertificate)
			contextMap := oracleCloneMap(t, corpus.BaseContext)
			if fixture.Mutation != nil {
				if fixture.Mutation.Target == "certificate" {
					applyOracleMutation(t, certificate, *fixture.Mutation)
				} else {
					applyOracleMutation(t, contextMap, *fixture.Mutation)
				}
			}
			certificateRaw, _ := json.Marshal(certificate)
			contextRaw, _ := json.Marshal(contextMap)
			var context OracleAdmissionContext
			if err := json.Unmarshal(contextRaw, &context); err != nil {
				t.Fatal(err)
			}
			context.NegativeCapabilities = corpus.NegativeCapabilities
			payloadDigest, payloadErr := OracleAdmissionPayloadDigest(certificateRaw, context.Signals, context.NegativeCapabilities)
			if payloadErr == nil {
				context.Expected.ManifestPayloadDigest = payloadDigest
			} else if fixture.ExpectedCode != "admission_schema_invalid" {
				t.Fatal(payloadErr)
			}
			boundaryCalls := 0
			decision := DecideOracleBehaviorAdmission(certificateRaw, context, func() { boundaryCalls++ })
			if decision.Code != fixture.ExpectedCode {
				t.Fatalf("expected %s, got %+v", fixture.ExpectedCode, decision)
			}
			expectedAllowed := fixture.ExpectedCode == "admission_allow"
			if decision.Allowed != expectedAllowed || boundaryCalls != map[bool]int{true: 1, false: 0}[expectedAllowed] {
				t.Fatalf("unexpected boundary decision: %+v calls=%d", decision, boundaryCalls)
			}
		})
	}
}

func TestOracleAdmissionRejectsUnboundManifestPayload(t *testing.T) {
	raw, err := os.ReadFile("testdata/oracle_lab_contract/v1/coherence-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus oracleAdmissionCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	certificate := oracleCloneMap(t, corpus.BaseCertificate)
	contextMap := oracleCloneMap(t, corpus.BaseContext)
	contextRaw, _ := json.Marshal(contextMap)
	var context OracleAdmissionContext
	if err := json.Unmarshal(contextRaw, &context); err != nil {
		t.Fatal(err)
	}
	context.NegativeCapabilities = corpus.NegativeCapabilities
	certificateRaw, _ := json.Marshal(certificate)
	context.Expected.ManifestPayloadDigest, err = OracleAdmissionPayloadDigest(certificateRaw, context.Signals, context.NegativeCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	certificate["persona_ref"] = "persona:attacker-selected"
	certificateRaw, _ = json.Marshal(certificate)
	decision := DecideOracleBehaviorAdmission(certificateRaw, context, nil)
	if decision.Allowed || decision.Code != "admission_manifest_payload_mismatch" {
		t.Fatalf("unbound payload returned %+v", decision)
	}
}
