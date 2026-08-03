package service

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type oracleCanonicalCorpus struct {
	JSONCases []struct {
		ID                   string `json:"id"`
		InputJSON            string `json:"input_json"`
		InputHex             string `json:"input_hex"`
		Valid                bool   `json:"valid"`
		ExpectedCode         string `json:"expected_code"`
		ExpectedCanonicalHex string `json:"expected_canonical_hex"`
		ExpectedSHA256       string `json:"expected_sha256"`
	} `json:"json_cases"`
	NormalizationCases []struct {
		ID                string      `json:"id"`
		Path              string      `json:"path"`
		QueryPairs        [][2]string `json:"query_pairs"`
		ExpectedPathQuery string      `json:"expected_path_query"`
		Host              string      `json:"host"`
		Port              int         `json:"port"`
		ExpectedAuthority string      `json:"expected_authority"`
	} `json:"normalization_cases"`
}

func TestOracleContractCanonical(t *testing.T) {
	raw, err := os.ReadFile("testdata/oracle_lab_contract/v1/canonicalization-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus oracleCanonicalCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range corpus.JSONCases {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			input := []byte(fixture.InputJSON)
			if fixture.InputHex != "" {
				input, err = hex.DecodeString(fixture.InputHex)
				if err != nil {
					t.Fatal(err)
				}
			}
			result, err := CanonicalizeOracleJSON(input)
			if !fixture.Valid {
				if OracleContractErrorCode(err) != fixture.ExpectedCode {
					t.Fatalf("expected %s, got %v", fixture.ExpectedCode, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fixture.ExpectedCanonicalHex != "" && hex.EncodeToString(result.Canonical) != fixture.ExpectedCanonicalHex {
				t.Fatalf("canonical bytes differ: %x", result.Canonical)
			}
			if fixture.ExpectedSHA256 != "" && result.SHA256 != fixture.ExpectedSHA256 {
				t.Fatalf("sha256 differs: %s", result.SHA256)
			}
		})
	}
	for _, fixture := range corpus.NormalizationCases {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			if fixture.ExpectedPathQuery != "" && NormalizeOraclePathQuery(fixture.Path, fixture.QueryPairs) != fixture.ExpectedPathQuery {
				t.Fatalf("path query differs")
			}
			if fixture.ExpectedAuthority != "" && FormatOracleAuthority(fixture.Host, fixture.Port) != fixture.ExpectedAuthority {
				t.Fatalf("authority differs")
			}
		})
	}
}
