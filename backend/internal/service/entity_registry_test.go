package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCreateEntityBindingInputRejectsAccountIDUntilSupported(t *testing.T) {
	accountID := int64(9001)

	_, err := ValidateCreateEntityBindingInput(CreateEntityBindingInput{
		EntityID:  1,
		AccountID: &accountID,
	})

	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "account")
}
