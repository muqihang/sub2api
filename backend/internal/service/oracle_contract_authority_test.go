package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

type oracleAuthorityCorpus struct {
	ExpectedNextStateDigests map[string]string `json:"expected_next_state_digests"`
	Cases                    []struct {
		ID           string `json:"id"`
		ExpectedCode string `json:"expected_code"`
	} `json:"cases"`
}

type oracleRuntimeAuthorityKey struct {
	OracleTrustKey
	PrivateKey ed25519.PrivateKey
}

type oracleAuthorityFixture struct {
	Keys       map[string]oracleRuntimeAuthorityKey
	State      OracleTrustState
	Context    OracleManifestAuthorityContext
	Manifest   OracleAuthorityManifest
	Checkpoint OracleAuthorityCheckpoint
}

func newOracleRuntimeAuthorityKey(t *testing.T, keyID, role string, epoch int64) oracleRuntimeAuthorityKey {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return oracleRuntimeAuthorityKey{OracleTrustKey: OracleTrustKey{KeyID: keyID, Role: role, Epoch: epoch, PublicKey: publicKey}, PrivateKey: privateKey}
}

func oracleAuthoritySignature(t *testing.T, key oracleRuntimeAuthorityKey, domain []byte, signed any) OracleAuthoritySignature {
	t.Helper()
	bytes, err := OracleDomainSeparatedJCS(domain, signed)
	if err != nil {
		t.Fatal(err)
	}
	return OracleAuthoritySignature{
		Algorithm:          "Ed25519",
		KeyID:              key.KeyID,
		KeyEpoch:           key.Epoch,
		Role:               key.Role,
		SignatureBase64URL: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key.PrivateKey, bytes)),
	}
}

func newOracleAuthorityFixture(t *testing.T) oracleAuthorityFixture {
	t.Helper()
	keys := make(map[string]oracleRuntimeAuthorityKey)
	for _, specification := range []struct {
		ID, Role string
	}{
		{"root-old-1", "root"}, {"root-old-2", "root"}, {"root-old-3", "root"},
		{"manifest-1", "manifest"}, {"manifest-2", "manifest"}, {"manifest-3", "manifest"},
		{"checkpoint-1", "checkpoint"}, {"revocation-1", "revocation"},
	} {
		keys[specification.ID] = newOracleRuntimeAuthorityKey(t, specification.ID, specification.Role, 1)
	}
	trustKeys := make(map[string]OracleTrustKey, len(keys))
	for id, key := range keys {
		trustKeys[id] = key.OracleTrustKey
	}
	state := OracleTrustState{
		RootEpoch: 1, PolicyVersion: 10, RollbackFloor: 10, RevocationVersion: 1,
		ManifestDigest: repeatOracleHex("a"), CheckpointVersion: 5, CheckpointDigest: repeatOracleHex("b"),
		ReplicaGeneration: 7, LastWallClockMS: 1799999999000, Keys: trustKeys,
		Thresholds: OracleAuthorityThresholds{Root: 2, Manifest: 2, Checkpoint: 1, Revocation: 1},
	}
	manifest := OracleAuthorityManifest{
		SchemaID: "oracle.compatibility", SchemaMajor: 1, SchemaRevision: 0, Kind: "manifest_authority",
		ManifestID: "manifest:fixture:11", PolicyVersion: 11, ParentDigest: state.ManifestDigest,
		RollbackDigest: state.ManifestDigest, ContractDigest: repeatOracleHex("1"), IssuedAtMS: 1799999999500,
		ExpiresAtMS: 1800003600000, SourcePackageDigests: []string{repeatOracleHex("2")},
		PromotionRefs: []string{}, WitnessCheckpointDigest: repeatOracleHex("c"),
		InvalidatingDependencyDigests: []string{repeatOracleHex("3")},
	}
	manifestDigest, err := OracleAuthorityObjectDigest(OracleManifestAuthorityDomain, manifest)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := OracleAuthorityCheckpoint{
		SchemaID: "oracle.compatibility", SchemaMajor: 1, SchemaRevision: 0, Kind: "checkpoint", Version: 6,
		ManifestDigest: manifestDigest, PreviousCheckpointDigest: state.CheckpointDigest,
		WitnessCheckpointDigest: manifest.WitnessCheckpointDigest, IssuedAtMS: 1799999999500, ExpiresAtMS: 1800003600000,
	}
	context := OracleManifestAuthorityContext{
		NowWallClockMS: 1800000000000, MonotonicElapsedMS: 1000, MaximumClockRollbackMS: 300000,
		MaximumCheckpointAgeMS: 3600000, ExpectedReplicaGeneration: 7,
		InvalidatedDependencyDigests: []string{}, WitnessedCheckpoints: map[int64]string{},
	}
	return oracleAuthorityFixture{Keys: keys, State: state, Context: context, Manifest: manifest, Checkpoint: checkpoint}
}

func repeatOracleHex(value string) string {
	result := ""
	for i := 0; i < 64; i++ {
		result += value
	}
	return result
}

func oracleSignedAuthorityUpdate(t *testing.T, fixture oracleAuthorityFixture, manifestSigners ...string) OracleManifestAuthorityUpdate {
	t.Helper()
	if manifestSigners == nil {
		manifestSigners = []string{"manifest-1", "manifest-2"}
	}
	manifestSignatures := make([]OracleAuthoritySignature, len(manifestSigners))
	for index, id := range manifestSigners {
		manifestSignatures[index] = oracleAuthoritySignature(t, fixture.Keys[id], OracleManifestAuthorityDomain, fixture.Manifest)
	}
	return OracleManifestAuthorityUpdate{
		Manifest: fixture.Manifest, ManifestSignatures: manifestSignatures, Checkpoint: fixture.Checkpoint,
		CheckpointSignatures: []OracleAuthoritySignature{oracleAuthoritySignature(t, fixture.Keys["checkpoint-1"], OracleCheckpointAuthorityDomain, fixture.Checkpoint)},
	}
}

func oracleRootRotation(t *testing.T, fixture oracleAuthorityFixture) (OracleRootRotation, []OracleAuthoritySignature, []OracleAuthoritySignature) {
	t.Helper()
	newKeys := []oracleRuntimeAuthorityKey{
		newOracleRuntimeAuthorityKey(t, "root-new-1", "root", 2),
		newOracleRuntimeAuthorityKey(t, "root-new-2", "root", 2),
		newOracleRuntimeAuthorityKey(t, "root-new-3", "root", 2),
	}
	rotation := OracleRootRotation{SchemaID: "oracle.compatibility", SchemaMajor: 1, SchemaRevision: 0, Kind: "root_rotation", OldEpoch: 1, NewEpoch: 2, NewRootThreshold: 2}
	for _, key := range newKeys {
		der, err := x509.MarshalPKIXPublicKey(key.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		rotation.NewKeys = append(rotation.NewKeys, OracleRootRotationKey{KeyID: key.KeyID, Role: "root", Epoch: 2, PublicKeySPKIBase64URL: base64.RawURLEncoding.EncodeToString(der)})
	}
	oldSignatures := []OracleAuthoritySignature{
		oracleAuthoritySignature(t, fixture.Keys["root-old-1"], OracleRootRotationAuthorityDomain, rotation),
		oracleAuthoritySignature(t, fixture.Keys["root-old-2"], OracleRootRotationAuthorityDomain, rotation),
	}
	newSignatures := []OracleAuthoritySignature{
		oracleAuthoritySignature(t, newKeys[0], OracleRootRotationAuthorityDomain, rotation),
		oracleAuthoritySignature(t, newKeys[1], OracleRootRotationAuthorityDomain, rotation),
	}
	return rotation, oldSignatures, newSignatures
}

func TestOracleContractAuthority(t *testing.T) {
	raw, err := os.ReadFile("testdata/oracle_lab_contract/v1/authority-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus oracleAuthorityCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, testcase := range corpus.Cases {
		testcase := testcase
		t.Run(testcase.ID, func(t *testing.T) {
			fixture := newOracleAuthorityFixture(t)
			if testcase.ID == "authority-root-rotation-dual-threshold" || testcase.ID == "authority-root-rotation-old-only" || testcase.ID == "authority-root-rotation-new-only" {
				rotation, oldSignatures, newSignatures := oracleRootRotation(t, fixture)
				if testcase.ID == "authority-root-rotation-old-only" {
					newSignatures = nil
				}
				if testcase.ID == "authority-root-rotation-new-only" {
					oldSignatures = nil
				}
				decision := VerifyOracleRootRotation(fixture.State, rotation, oldSignatures, newSignatures)
				if decision.Code != testcase.ExpectedCode {
					t.Fatalf("expected %s, got %+v", testcase.ExpectedCode, decision)
				}
				if decision.Allowed && decision.NextStateDigest != corpus.ExpectedNextStateDigests[testcase.ID] {
					t.Fatalf("root rotation digest mismatch: %s", decision.NextStateDigest)
				}
				if decision.Allowed && os.Getenv("ORACLE_PHASE2_DEBUG_DIGESTS") == "1" {
					t.Logf("authority-digest %s %s", testcase.ID, decision.NextStateDigest)
				}
				return
			}
			if testcase.ID == "authority-emergency-revocation" {
				revocation := OracleAuthorityRevocation{
					SchemaID: "oracle.compatibility", SchemaMajor: 1, SchemaRevision: 0, Kind: "emergency_revocation",
					Version: 2, KeyEpoch: 1, IssuedAtMS: fixture.Context.NowWallClockMS,
					ExpiresAtMS: fixture.Context.NowWallClockMS + 60000, RevokedKeyIDs: []string{"manifest-3"}, ReasonRef: "reason:key-compromise-fixture",
				}
				decision := VerifyOracleEmergencyRevocation(fixture.State, revocation, []OracleAuthoritySignature{oracleAuthoritySignature(t, fixture.Keys["revocation-1"], OracleRevocationAuthorityDomain, revocation)}, fixture.Context.NowWallClockMS)
				if decision.Code != testcase.ExpectedCode || decision.NextState == nil || !decision.NextState.Keys["manifest-3"].Revoked {
					t.Fatalf("unexpected revocation decision: %+v", decision)
				}
				if decision.NextStateDigest != corpus.ExpectedNextStateDigests[testcase.ID] {
					t.Fatalf("revocation digest mismatch: %s", decision.NextStateDigest)
				}
				if os.Getenv("ORACLE_PHASE2_DEBUG_DIGESTS") == "1" {
					t.Logf("authority-digest %s %s", testcase.ID, decision.NextStateDigest)
				}
				return
			}
			if testcase.ID == "authority-expired" {
				fixture.Manifest.ExpiresAtMS = fixture.Context.NowWallClockMS - 1
			}
			if testcase.ID == "authority-parent-mismatch" {
				fixture.Manifest.ParentDigest = repeatOracleHex("0")
			}
			if testcase.ID == "authority-policy-rollback" {
				fixture.Manifest.PolicyVersion = 9
			}
			if testcase.ID == "authority-revoked-key" {
				key := fixture.State.Keys["manifest-1"]
				key.Revoked = true
				fixture.State.Keys["manifest-1"] = key
			}
			if testcase.ID == "authority-stale-checkpoint" {
				fixture.Checkpoint.Version = fixture.State.CheckpointVersion
			}
			if testcase.ID == "authority-freeze" {
				fixture.Checkpoint.IssuedAtMS = fixture.Context.NowWallClockMS - fixture.Context.MaximumCheckpointAgeMS - 1
			}
			if testcase.ID == "authority-mix-and-match" {
				fixture.Checkpoint.ManifestDigest = repeatOracleHex("0")
			}
			if testcase.ID == "authority-split-view" {
				fixture.Context.WitnessedCheckpoints[fixture.Checkpoint.Version] = repeatOracleHex("0")
			}
			if testcase.ID == "authority-witness-mismatch" {
				fixture.Manifest.WitnessCheckpointDigest = repeatOracleHex("0")
			}
			if testcase.ID == "authority-clock-rollback" {
				fixture.Context.NowWallClockMS = fixture.State.LastWallClockMS - fixture.Context.MaximumClockRollbackMS - 1
			}
			if testcase.ID == "authority-dependency-invalidated" {
				fixture.Context.InvalidatedDependencyDigests = []string{fixture.Manifest.InvalidatingDependencyDigests[0]}
			}
			if testcase.ID == "authority-replica-generation-conflict" {
				fixture.Context.ExpectedReplicaGeneration++
			}
			if testcase.ID != "authority-mix-and-match" {
				fixture.Checkpoint.ManifestDigest, err = OracleAuthorityObjectDigest(OracleManifestAuthorityDomain, fixture.Manifest)
				if err != nil {
					t.Fatal(err)
				}
			}
			var update OracleManifestAuthorityUpdate
			switch testcase.ID {
			case "authority-insufficient-threshold":
				update = oracleSignedAuthorityUpdate(t, fixture, "manifest-1")
			case "authority-duplicate-signer":
				update = oracleSignedAuthorityUpdate(t, fixture, "manifest-1")
				update.ManifestSignatures = append(update.ManifestSignatures, update.ManifestSignatures[0])
			case "authority-wrong-role":
				update = oracleSignedAuthorityUpdate(t, fixture, []string{}...)
				update.ManifestSignatures = []OracleAuthoritySignature{oracleAuthoritySignature(t, fixture.Keys["root-old-1"], OracleManifestAuthorityDomain, fixture.Manifest)}
			default:
				update = oracleSignedAuthorityUpdate(t, fixture)
			}
			decision := VerifyOracleManifestAuthorityUpdate(fixture.State, update, fixture.Context)
			if decision.Code != testcase.ExpectedCode {
				t.Fatalf("expected %s, got %+v", testcase.ExpectedCode, decision)
			}
			if decision.Allowed {
				if decision.NextStateDigest != corpus.ExpectedNextStateDigests[testcase.ID] {
					t.Fatalf("fixture digest mismatch: %s", decision.NextStateDigest)
				}
				if os.Getenv("ORACLE_PHASE2_DEBUG_DIGESTS") == "1" {
					t.Logf("authority-digest %s %s", testcase.ID, decision.NextStateDigest)
				}
				digest, err := OracleTrustStateDigest(*decision.NextState)
				if err != nil || digest != decision.NextStateDigest {
					t.Fatalf("next state digest mismatch: %s %v", digest, err)
				}
				if testcase.ID == "authority-restart-snapshot" {
					restarted := fixture.State
					restarted.Keys = make(map[string]OracleTrustKey, len(fixture.State.Keys))
					for id, key := range fixture.State.Keys {
						restarted.Keys[id] = key
					}
					if again := VerifyOracleManifestAuthorityUpdate(restarted, update, fixture.Context); again.NextStateDigest != decision.NextStateDigest {
						t.Fatalf("restart digest differs")
					}
				}
			}
		})
	}
}
