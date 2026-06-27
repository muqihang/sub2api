package service

import (
	"context"
	"strings"
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

type claudePlatformAWSUpdateRepoStub struct {
	AccountRepository
	account     *Account
	updateCalls int
}

func (r *claudePlatformAWSUpdateRepoStub) GetByID(_ context.Context, _ int64) (*Account, error) {
	return r.account, nil
}

func (r *claudePlatformAWSUpdateRepoStub) Update(_ context.Context, account *Account) error {
	r.updateCalls++
	r.account = account
	return nil
}

func TestAdminService_ClaudePlatformAWSUpdatePreservesAuthorityExtraAndAllowsTuning(t *testing.T) {
	accountID := int64(5904)
	existingExtra := claudePlatformAWSAuthorityExtraForAdminTest()
	existingExtra["base_rpm"] = 10
	repo := &claudePlatformAWSUpdateRepoStub{account: &Account{
		ID:       accountID,
		Platform: PlatformAnthropic,
		Type:     AccountTypeClaudePlatformAWS,
		Status:   StatusActive,
		Credentials: map[string]any{
			"auth_mode":              "apikey",
			"api_key":                syntheticAWSAPIKey(),
			"aws_region":             "us-east-1",
			"anthropic_workspace_id": syntheticAWSWorkspaceID(3),
			"base_url":               "https://aws-external-anthropic.us-east-1.api.aws",
		},
		Extra: existingExtra,
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	incomingExtra := claudePlatformAWSSpoofedAuthorityExtraForAdminTest()
	incomingExtra["base_rpm"] = 42

	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{Extra: incomingExtra})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 42, repo.account.Extra["base_rpm"])
	for _, key := range claudePlatformAWSProtectedAuthorityExtraKeysForAdminTest() {
		require.Equal(t, existingExtra[key], repo.account.Extra[key], key)
	}
}

func TestAdminService_ClaudePlatformAWSUpdateBlankCredentialDoesNotClearSensitiveTuple(t *testing.T) {
	accountID := int64(5905)
	existingAPIKey := syntheticAWSAPIKey()
	existingWorkspace := syntheticAWSWorkspaceID(3)
	repo := &claudePlatformAWSUpdateRepoStub{account: &Account{
		ID:       accountID,
		Platform: PlatformAnthropic,
		Type:     AccountTypeClaudePlatformAWS,
		Status:   StatusActive,
		Credentials: map[string]any{
			"auth_mode":              "apikey",
			"api_key":                existingAPIKey,
			"aws_region":             "us-east-1",
			"anthropic_workspace_id": existingWorkspace,
			"base_url":               "https://aws-external-anthropic.us-east-1.api.aws",
		},
		Extra: claudePlatformAWSAuthorityExtraForAdminTest(),
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{Credentials: map[string]any{
		"auth_mode":              "",
		"api_key":                "",
		"aws_region":             "",
		"anthropic_workspace_id": "",
		"base_url":               "",
	}})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, existingAPIKey, repo.account.Credentials["api_key"])
	require.Equal(t, existingWorkspace, repo.account.Credentials["anthropic_workspace_id"])
	require.Equal(t, "apikey", repo.account.Credentials["auth_mode"])
	require.Equal(t, "us-east-1", repo.account.Credentials["aws_region"])
	require.Equal(t, "https://aws-external-anthropic.us-east-1.api.aws", repo.account.Credentials["base_url"])
}

type claudePlatformAWSBulkUpdateRepoStub struct {
	AccountRepository
	bulkUpdateIDs   []int64
	lastBulkUpdates AccountBulkUpdate
}

func (r *claudePlatformAWSBulkUpdateRepoStub) BulkUpdate(_ context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	r.bulkUpdateIDs = append([]int64{}, ids...)
	r.lastBulkUpdates = updates
	return int64(len(ids)), nil
}

func TestAdminService_BulkUpdateAccounts_FiltersClaudePlatformAWSAuthorityExtra(t *testing.T) {
	repo := &claudePlatformAWSBulkUpdateRepoStub{}
	svc := &adminServiceImpl{accountRepo: repo}
	incomingExtra := claudePlatformAWSSpoofedAuthorityExtraForAdminTest()
	incomingExtra["base_rpm"] = 55

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{5904, 5905},
		Extra:      incomingExtra,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, []int64{5904, 5905}, repo.bulkUpdateIDs)
	require.Equal(t, 55, repo.lastBulkUpdates.Extra["base_rpm"])
	for _, key := range claudePlatformAWSProtectedAuthorityExtraKeysForAdminTest() {
		require.NotContains(t, repo.lastBulkUpdates.Extra, key, key)
	}
}

func claudePlatformAWSProtectedAuthorityExtraKeysForAdminTest() []string {
	return []string{
		ClaudePlatformAWSExtraWorkspaceRef,
		ClaudePlatformAWSExtraWorkspaceBindingHMAC,
		ClaudePlatformAWSExtraEndpointRef,
		ClaudePlatformAWSExtraRegion,
		ClaudePlatformAWSExtraAuthScheme,
		ClaudePlatformAWSExtraRequestShapeProfileRef,
		ClaudePlatformAWSExtraCacheParityProfileRef,
		ClaudePlatformAWSExtraBetaPolicyRef,
		ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus,
		ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus,
		ClaudePlatformAWSExtraProductionAdmitted,
		ccGatewayExtraAccountRef,
		ccGatewayExtraCredentialRef,
		ccGatewayExtraCredentialBindingHMAC,
		ccGatewayExtraProxyIdentityRef,
		ccGatewayExtraEgressBucket,
	}
}

func claudePlatformAWSAuthorityExtraForAdminTest() map[string]any {
	return map[string]any{
		ClaudePlatformAWSExtraWorkspaceRef:                     "workspace:existing",
		ClaudePlatformAWSExtraWorkspaceBindingHMAC:             "hmac-sha256:" + strings.Repeat("1", 64),
		ClaudePlatformAWSExtraEndpointRef:                      "endpoint:existing",
		ClaudePlatformAWSExtraRegion:                           "us-east-1",
		ClaudePlatformAWSExtraAuthScheme:                       ClaudePlatformAWSAuthProfileXAPIKey,
		ClaudePlatformAWSExtraRequestShapeProfileRef:           "request-shape:existing",
		ClaudePlatformAWSExtraCacheParityProfileRef:            "cache-profile:existing",
		ClaudePlatformAWSExtraBetaPolicyRef:                    "beta-policy:existing",
		ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus:     "pass",
		ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus: "pass",
		ClaudePlatformAWSExtraProductionAdmitted:               true,
		ccGatewayExtraAccountRef:                               "account:existing",
		ccGatewayExtraCredentialRef:                            "credential:existing",
		ccGatewayExtraCredentialBindingHMAC:                    "hmac-sha256:" + strings.Repeat("2", 64),
		ccGatewayExtraProxyIdentityRef:                         "proxy:existing",
		ccGatewayExtraEgressBucket:                             "egress:existing",
	}
}

func claudePlatformAWSSpoofedAuthorityExtraForAdminTest() map[string]any {
	return map[string]any{
		ClaudePlatformAWSExtraWorkspaceRef:                     "workspace:spoofed",
		ClaudePlatformAWSExtraWorkspaceBindingHMAC:             "hmac-sha256:" + strings.Repeat("3", 64),
		ClaudePlatformAWSExtraEndpointRef:                      "endpoint:spoofed",
		ClaudePlatformAWSExtraRegion:                           "eu-west-1",
		ClaudePlatformAWSExtraAuthScheme:                       ClaudePlatformAWSAuthProfileBearerAPIKey,
		ClaudePlatformAWSExtraRequestShapeProfileRef:           "request-shape:spoofed",
		ClaudePlatformAWSExtraCacheParityProfileRef:            "cache-profile:spoofed",
		ClaudePlatformAWSExtraBetaPolicyRef:                    "beta-policy:spoofed",
		ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus:     "pass",
		ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus: "pass",
		ClaudePlatformAWSExtraProductionAdmitted:               false,
		ccGatewayExtraAccountRef:                               "account:spoofed",
		ccGatewayExtraCredentialRef:                            "credential:spoofed",
		ccGatewayExtraCredentialBindingHMAC:                    "hmac-sha256:" + strings.Repeat("4", 64),
		ccGatewayExtraProxyIdentityRef:                         "proxy:spoofed",
		ccGatewayExtraEgressBucket:                             "egress:spoofed",
	}
}
