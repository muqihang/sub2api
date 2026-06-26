package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type claudePlatformAWSCreateRepoStub struct {
	AccountRepository
	created *Account
}

func (r *claudePlatformAWSCreateRepoStub) Create(_ context.Context, account *Account) error {
	r.created = account
	account.ID = 5901
	return nil
}

func (r *claudePlatformAWSCreateRepoStub) BindGroups(_ context.Context, _ int64, _ []int64) error {
	return nil
}

func TestAdminServiceCreateAccount_ClaudePlatformAWSValidatesAndStartsUnschedulable(t *testing.T) {
	repo := &claudePlatformAWSCreateRepoStub{}
	svc := &adminServiceImpl{accountRepo: repo}
	proxyID := int64(42)
	reqSchedulable := true

	got, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "cpaws",
		Platform:             PlatformAnthropic,
		Type:                 AccountTypeClaudePlatformAWS,
		ProxyID:              &proxyID,
		Schedulable:          &reqSchedulable,
		SkipDefaultGroupBind: true,
		Credentials: map[string]any{
			"auth_mode":              "apikey",
			"api_key":                syntheticAWSAPIKey(),
			"aws_region":             "us-east-1",
			"anthropic_workspace_id": syntheticAWSWorkspaceID(3),
			"base_url":               "https://aws-external-anthropic.us-east-1.api.aws",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	require.False(t, repo.created.Schedulable, "new AWS Platform accounts must be non-production until CP0/CC Gateway gates pass")
	require.True(t, isSafeLedgerRef(repo.created.Extra[ClaudePlatformAWSExtraWorkspaceRef].(string)))
	require.True(t, isSafeLedgerRef(repo.created.Extra[ClaudePlatformAWSExtraEndpointRef].(string)))
	require.True(t, isSafeLedgerRef(repo.created.Extra[ccGatewayExtraAccountRef].(string)))
	require.True(t, isSafeLedgerRef(repo.created.Extra[ccGatewayExtraCredentialRef].(string)))
	require.True(t, ledgerGeneratedHMACRefRe.MatchString(repo.created.Extra[ccGatewayExtraCredentialBindingHMAC].(string)))
	require.True(t, isSafeLedgerRef(repo.created.Extra[ccGatewayExtraProxyIdentityRef].(string)))
	require.True(t, ledgerGeneratedHMACRefRe.MatchString(repo.created.Extra[ClaudePlatformAWSExtraWorkspaceBindingHMAC].(string)))
	require.Equal(t, "us-east-1", repo.created.Extra[ClaudePlatformAWSExtraRegion])
	require.Equal(t, ClaudePlatformAWSAuthProfileBlocked, repo.created.Extra[ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus])
}

func TestAdminServiceCreateAccount_ClaudePlatformAWSRejectsInvalidWithoutLeakingWorkspace(t *testing.T) {
	repo := &claudePlatformAWSCreateRepoStub{}
	svc := &adminServiceImpl{accountRepo: repo}
	proxyID := int64(42)

	_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "cpaws",
		Platform:             PlatformAnthropic,
		Type:                 AccountTypeClaudePlatformAWS,
		ProxyID:              &proxyID,
		SkipDefaultGroupBind: true,
		Credentials: map[string]any{
			"auth_mode":              "apikey",
			"api_key":                syntheticAWSAPIKey(),
			"aws_region":             "us-east-1",
			"anthropic_workspace_id": "not-a-workspace-secret",
			"base_url":               "https://aws-external-anthropic.us-east-1.api.aws",
		},
	})

	require.Error(t, err)
	require.Nil(t, repo.created)
	require.NotContains(t, err.Error(), "not-a-workspace-secret")
	require.NotContains(t, err.Error(), syntheticAWSAPIKey())
}
