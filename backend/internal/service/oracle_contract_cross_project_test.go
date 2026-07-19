package service

import (
	"encoding/json"
	"os"
	"testing"
)

type oracleCrossProjectCorpus struct {
	ExpectedStateDigests map[string]string `json:"expected_state_digests"`
	Fixtures             struct {
		ReadinessExpected  OracleReadinessExpected  `json:"readiness_expected"`
		Readiness          OracleReadinessHandshake `json:"readiness"`
		LifecycleState     OracleLifecycleState     `json:"lifecycle_state"`
		LifecycleOperation OracleLifecycleOperation `json:"lifecycle_operation"`
		LineageState       OracleTaskLineageState   `json:"lineage_state"`
		LineageCandidate   OracleTaskLineage        `json:"lineage_candidate"`
		OutcomePartial     OracleOutcomeEnvelope    `json:"outcome_partial"`
		OutcomeRateLimit   OracleOutcomeEnvelope    `json:"outcome_rate_limit"`
	} `json:"fixtures"`
	Cases []struct {
		ID           string `json:"id"`
		Kind         string `json:"kind"`
		ExpectedCode string `json:"expected_code"`
	} `json:"cases"`
}

func TestOracleContractCrossProject(t *testing.T) {
	raw, err := os.ReadFile("testdata/oracle_lab_contract/v1/interface-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus oracleCrossProjectCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range corpus.Cases {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			code := ""
			stateDigest := ""
			switch fixture.Kind {
			case "readiness":
				expected, handshake := corpus.Fixtures.ReadinessExpected, corpus.Fixtures.Readiness
				if fixture.ID == "readiness-live-not-ready" {
					handshake.Readiness = false
				}
				if fixture.ID == "readiness-contract-mismatch" {
					handshake.ContractDigest = repeatOracleHex("0")
				}
				if fixture.ID == "readiness-revision-unsupported" {
					handshake.SupportedContracts = []OracleSupportedContractRange{{SchemaMajor: 1, MinimumRevision: 1, MaximumRevision: 1}}
				}
				boundaryCalls := 0
				decision := DecideOracleReadiness(handshake, expected, func() { boundaryCalls++ })
				code = decision.Code
				if boundaryCalls != map[bool]int{true: 1, false: 0}[decision.Allowed] {
					t.Fatalf("readiness boundary calls=%d", boundaryCalls)
				}
			case "lifecycle":
				state, operation := corpus.Fixtures.LifecycleState, corpus.Fixtures.LifecycleOperation
				if fixture.ID == "lifecycle-register" {
					state.AccountGeneration, state.CredentialGeneration, state.ProxyGeneration, state.ProfileGeneration, state.StateVersion, state.Status = 0, 0, 0, 0, 0, "absent"
					operation.Operation, operation.AccountGeneration, operation.CredentialGeneration, operation.ProxyGeneration, operation.ProfileGeneration, operation.ExpectedStateVersion, operation.NextStateVersion = "register", 1, 1, 1, 1, 0, 1
				}
				if fixture.ID == "lifecycle-stale-cas" {
					operation.ExpectedStateVersion = 0
				}
				if fixture.ID == "lifecycle-generation-regression" {
					operation.ProxyGeneration = 0
				}
				decision := TransitionOracleLifecycle(state, operation)
				code, stateDigest = decision.Code, decision.NextStateDigest
			case "lineage":
				state, candidate := corpus.Fixtures.LineageState, corpus.Fixtures.LineageCandidate
				if fixture.ID == "lineage-root-mismatch" {
					candidate.RootTaskRef = "task:root:other"
				}
				if fixture.ID == "migration-sequence-stale" {
					candidate.MigrationSequence = state.MigrationSequence
				}
				decision := DecideOracleTaskLineage(state, candidate, corpus.Fixtures.ReadinessExpected.NowMS)
				code, stateDigest = decision.Code, decision.NextStateDigest
			case "outcome":
				outcome := corpus.Fixtures.OutcomeRateLimit
				if fixture.ID == "outcome-partial-tool-side-effect" {
					outcome = corpus.Fixtures.OutcomePartial
				}
				code = DecideOracleOutcome(outcome).Code
			case "replay":
				identity := OracleInterfaceReplayCommand{KeyEpoch: 11, CapabilityID: "capability:fixture:1", AttemptID: "attempt:fixture:1", Nonce: "nonce:fixture:1", Operation: "reserve", ExpectedGeneration: 0, NowMS: 1800000000000, ExpiresAtMS: 1800000060000}
				initial := OracleInterfaceReplayState{Entries: map[string]OracleInterfaceReplayEntry{}}
				reserved := TransitionOracleInterfaceReplay(initial, identity)
				switch fixture.ID {
				case "replay-reserve":
					code = reserved.Code
				case "replay-commit":
					identity.Operation, identity.ExpectedGeneration, identity.NowMS = "commit", 1, 1800000000100
					code = TransitionOracleInterfaceReplay(reserved.NextState.(OracleInterfaceReplayState), identity).Code
				case "replay-reuse":
					identity.ExpectedGeneration, identity.NowMS = 1, 1800000000100
					code = TransitionOracleInterfaceReplay(reserved.NextState.(OracleInterfaceReplayState), identity).Code
				case "replay-stale-replica":
					identity.Operation, identity.ExpectedGeneration, identity.NowMS = "commit", 0, 1800000000100
					code = TransitionOracleInterfaceReplay(reserved.NextState.(OracleInterfaceReplayState), identity).Code
				}
			}
			if code != fixture.ExpectedCode {
				t.Fatalf("expected %s, got %s", fixture.ExpectedCode, code)
			}
			if stateDigest != "" && stateDigest != corpus.ExpectedStateDigests[fixture.ID] {
				t.Fatalf("state digest differs: %s", stateDigest)
			}
			if stateDigest != "" && os.Getenv("ORACLE_PHASE2_DEBUG_DIGESTS") == "1" {
				t.Logf("interface-digest %s %s", fixture.ID, stateDigest)
			}
		})
	}
}
