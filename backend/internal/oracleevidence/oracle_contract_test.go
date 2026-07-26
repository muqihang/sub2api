package oracleevidence

import (
	"errors"
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
