package oracleevidence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const mirrorRoot = "testdata/oracle_lab_contract/v1"

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

func r1OracleContractScaffold(t *testing.T) {
	if len(StableCodes) != 119 {
		t.Fatalf("stable code count = %d, want 119", len(StableCodes))
	}
	if got := stableCodeDigest(); got != "f6f89d48519aaa46b362a474cc6bd8e470b638e1c7f4c3c0a7ac99413a85fa5c" {
		t.Fatalf("stable code digest = %s", got)
	}
	if got := (notImplementedDecision()); got.Allowed || got.Code != CodeOracleNotImplemented {
		t.Fatalf("scaffold does not fail closed: %+v", got)
	}
}

func r1OracleContractStrictJSON(t *testing.T) {
	t.Run("invalid_json_fails_closed", func(t *testing.T) {
		_, err := ParseStrictJSON([]byte(`{"broken":`))
		requireCode(t, err, CodeJSONInvalid)
	})
	t.Run("valid_json_is_accepted", func(t *testing.T) {
		value, err := ParseStrictJSON([]byte(`{"ok":true}`))
		if err != nil {
			t.Fatalf("valid strict JSON rejected: %v", err)
		}
		if value == nil {
			t.Fatal("valid strict JSON returned nil")
		}
	})
}

func r1OracleContractJCS(t *testing.T) {
	t.Run("invalid_jcs_source_fails_closed", func(t *testing.T) {
		_, err := CanonicalizeJSON(nil)
		requireCode(t, err, CodeJSONInvalid)
	})
	t.Run("valid_jcs_control", func(t *testing.T) {
		got, err := CanonicalizeJSON([]byte(`{"b":1,"a":2}`))
		if err != nil {
			t.Fatalf("valid JCS control rejected: %v", err)
		}
		if string(got) != `{"a":2,"b":1}` {
			t.Fatalf("canonical bytes = %q", got)
		}
	})
}

func r1OracleContractNormalization(t *testing.T) {
	t.Run("invalid_raw_port_fails_closed", func(t *testing.T) {
		_, err := ParseAuthorityPort(RawPort("65536"))
		requireCode(t, err, CodeURLPortInvalid)
	})
	t.Run("valid_authority_control", func(t *testing.T) {
		got, err := FormatAuthority("api.example.com", RawPort("443"))
		if err != nil {
			t.Fatalf("valid authority rejected: %v", err)
		}
		if got != "api.example.com:443" {
			t.Fatalf("authority = %q", got)
		}
	})
}

func r1OracleContractCBOR(t *testing.T) {
	t.Run("empty_cbor_fails_closed", func(t *testing.T) {
		_, err := CanonicalizeCBOR(nil)
		requireCode(t, err, CodeCBORInvalid)
	})
	t.Run("valid_cbor_control", func(t *testing.T) {
		got, err := CanonicalizeCBOR([]byte{0xa1, 0x61, 0x61, 0x01})
		if err != nil {
			t.Fatalf("valid CBOR rejected: %v", err)
		}
		if len(got) == 0 {
			t.Fatal("valid CBOR returned empty bytes")
		}
	})
}

func r1OracleContractSchema(t *testing.T) {
	t.Run("missing_schema_fails_closed", func(t *testing.T) {
		_, err := LoadContractSchema(filepath.Join(t.TempDir(), "missing"))
		requireCode(t, err, CodeContractBundle)
	})
	t.Run("valid_schema_control", func(t *testing.T) {
		schemas, err := LoadContractSchema(mirrorRoot)
		if err != nil {
			t.Fatalf("valid schema rejected: %v", err)
		}
		if schemas == nil {
			t.Fatal("valid schema returned nil")
		}
	})
}

func r1OracleContractAdmission(t *testing.T) {
	t.Run("malformed_certificate_fails_closed", func(t *testing.T) {
		decision := DecideBehaviorAdmission(nil, []byte(`{}`))
		if decision.Allowed {
			t.Fatal("malformed certificate allowed")
		}
	})
	t.Run("valid_certificate_admitted", func(t *testing.T) {
		decision := DecideBehaviorAdmission([]byte(`{"schema":"oracle.compatibility"}`), []byte(`{"now_ms":1}`))
		requireAllowed(t, decision, "admission_allow")
	})
}

func r1OracleContractManifestAuthority(t *testing.T) {
	t.Run("missing_candidate_fails_closed", func(t *testing.T) {
		if VerifyManifestAuthorityUpdate(AuthorityInput{}).Allowed {
			t.Fatal("missing candidate allowed")
		}
	})
	t.Run("valid_authority_update_allowed", func(t *testing.T) {
		decision := VerifyManifestAuthorityUpdate(AuthorityInput{State: []byte(`{}`), Candidate: []byte(`{"manifest":"valid"}`), Context: []byte(`{}`)})
		requireAllowed(t, decision, "authority_allow")
	})
}

func r1OracleContractInterface(t *testing.T) {
	t.Run("missing_handshake_fails_closed", func(t *testing.T) {
		if DecideReadiness(nil, []byte(`{}`)).Allowed {
			t.Fatal("missing handshake allowed")
		}
	})
	t.Run("valid_readiness_allowed", func(t *testing.T) {
		requireAllowed(t, DecideReadiness([]byte(`{"ready":true}`), []byte(`{"ready":true}`)), "interface_allow")
	})
}

func r1OracleContractReplay(t *testing.T) {
	t.Run("missing_command_fails_closed", func(t *testing.T) {
		if ExecuteReplay([]byte(`{}`), nil).Allowed {
			t.Fatal("missing replay command allowed")
		}
	})
	t.Run("valid_replay_reserve_allowed", func(t *testing.T) {
		requireAllowed(t, ExecuteReplay([]byte(`{"state":"fresh"}`), []byte(`{"operation":"reserve"}`)), "replay_reserved")
	})
}

func r1OracleContractSidecar(t *testing.T) {
	t.Run("empty_envelope_fails_closed", func(t *testing.T) {
		if ValidateSidecarEnvelope(nil, nil).Allowed {
			t.Fatal("empty sidecar allowed")
		}
	})
	t.Run("valid_sidecar_control", func(t *testing.T) {
		requireAllowed(t, ValidateSidecarEnvelope([]byte{0xa1, 0x01, 0x01}, &SchemaSet{bundleRoot: mirrorRoot}), "sidecar_capability_allow")
	})
}

func r1OracleContractMutation(t *testing.T) {
	t.Run("invalid_pointer_fails_closed", func(t *testing.T) {
		_, err := ParseBoundedPointerIndex("01", 4, false)
		requireCode(t, err, CodeMutationPointer)
	})
	t.Run("positive_mutation_noop", func(t *testing.T) {
		source := []byte(`{"certificate":"valid-control"}`)
		got, err := ApplyMutation(source, MutationOperation{Kind: "replace_bytes", Offset: 0, DeleteCount: 0})
		if err != nil {
			t.Fatalf("positive mutation no-op rejected: %v", err)
		}
		if string(got) != string(source) {
			t.Fatalf("positive no-op changed source: %q", got)
		}
	})
}

func r1OracleContractCrossRepo(t *testing.T) {
	t.Run("missing_mirror_fails_closed", func(t *testing.T) {
		decision := InspectMirror(filepath.Join(t.TempDir(), "missing"), mirrorRoot, "predecessor")
		if decision.Allowed {
			t.Fatal("missing mirror allowed")
		}
	})
	t.Run("valid_mirror_accepted", func(t *testing.T) {
		decision := InspectMirror(mirrorRoot, mirrorRoot, "70c26db06e9135db31d08f097573e3fd55bd9a8894614832eefeecabf6b1a3d1")
		requireAllowed(t, decision, "")
	})
	t.Run("committed_rebaseline_fixtures_reached", func(t *testing.T) {
		for _, name := range []string{"mutation-corpus.json", "source-manifest.json", filepath.Join("synthetic", "control.json")} {
			if _, err := os.Stat(filepath.Join("testdata/rebaseline/v1", name)); err != nil {
				t.Fatalf("fixture %s unavailable: %v", name, err)
			}
		}
	})
}
