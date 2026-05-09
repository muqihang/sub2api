package service

import (
	"errors"
	"fmt"
	"strings"
)

type GeminiAccountFamily string

const (
	GeminiAccountFamilyCodeAssist           GeminiAccountFamily = "code_assist"
	GeminiAccountFamilyGoogleOne            GeminiAccountFamily = "google_one"
	GeminiAccountFamilyAIStudioOAuth        GeminiAccountFamily = "ai_studio_oauth"
	GeminiAccountFamilyAIStudioAPIKey       GeminiAccountFamily = "ai_studio_apikey"
	GeminiAccountFamilyVertexServiceAccount GeminiAccountFamily = "vertex_service_account"
)

type GeminiUpstreamFamily string

const (
	GeminiUpstreamFamilyAIStudio   GeminiUpstreamFamily = "ai_studio"
	GeminiUpstreamFamilyCodeAssist GeminiUpstreamFamily = "code_assist"
	GeminiUpstreamFamilyVertex     GeminiUpstreamFamily = "vertex"
)

type GeminiTokenSource string

const (
	GeminiTokenSourceOAuth          GeminiTokenSource = "oauth"
	GeminiTokenSourceAPIKey         GeminiTokenSource = "api_key"
	GeminiTokenSourceServiceAccount GeminiTokenSource = "service_account"
)

type GeminiRuntimeContract struct {
	AccountFamily            GeminiAccountFamily
	UpstreamFamily           GeminiUpstreamFamily
	RequiresProjectID        bool
	SupportsLiveAPI          bool
	SupportsThoughtSignature bool
	TokenSource              GeminiTokenSource
	HasTierSemantics         bool
}

func ResolveGeminiRuntimeContract(account *Account) (*GeminiRuntimeContract, error) {
	if account == nil {
		return nil, ErrAccountNilInput
	}
	if account.Platform != PlatformGemini {
		return nil, errors.New("account is not a gemini account")
	}

	switch account.Type {
	case AccountTypeOAuth:
		oauthType := strings.ToLower(strings.TrimSpace(account.GeminiOAuthType()))
		if oauthType == "" {
			oauthType = "ai_studio"
		}
		switch oauthType {
		case "code_assist":
			return &GeminiRuntimeContract{
				AccountFamily:            GeminiAccountFamilyCodeAssist,
				UpstreamFamily:           GeminiUpstreamFamilyCodeAssist,
				RequiresProjectID:        true,
				SupportsLiveAPI:          false,
				SupportsThoughtSignature: true,
				TokenSource:              GeminiTokenSourceOAuth,
				HasTierSemantics:         true,
			}, nil
		case "google_one":
			return &GeminiRuntimeContract{
				AccountFamily:            GeminiAccountFamilyGoogleOne,
				UpstreamFamily:           GeminiUpstreamFamilyCodeAssist,
				RequiresProjectID:        false,
				SupportsLiveAPI:          false,
				SupportsThoughtSignature: true,
				TokenSource:              GeminiTokenSourceOAuth,
				HasTierSemantics:         true,
			}, nil
		case "ai_studio":
			return &GeminiRuntimeContract{
				AccountFamily:            GeminiAccountFamilyAIStudioOAuth,
				UpstreamFamily:           GeminiUpstreamFamilyAIStudio,
				RequiresProjectID:        false,
				SupportsLiveAPI:          false,
				SupportsThoughtSignature: true,
				TokenSource:              GeminiTokenSourceOAuth,
				HasTierSemantics:         true,
			}, nil
		default:
			return nil, fmt.Errorf("unsupported gemini oauth_type: %s", oauthType)
		}
	case AccountTypeAPIKey:
		return &GeminiRuntimeContract{
			AccountFamily:            GeminiAccountFamilyAIStudioAPIKey,
			UpstreamFamily:           GeminiUpstreamFamilyAIStudio,
			RequiresProjectID:        false,
			SupportsLiveAPI:          false,
			SupportsThoughtSignature: true,
			TokenSource:              GeminiTokenSourceAPIKey,
			HasTierSemantics:         false,
		}, nil
	case AccountTypeServiceAccount:
		return &GeminiRuntimeContract{
			AccountFamily:            GeminiAccountFamilyVertexServiceAccount,
			UpstreamFamily:           GeminiUpstreamFamilyVertex,
			RequiresProjectID:        true,
			SupportsLiveAPI:          false,
			SupportsThoughtSignature: true,
			TokenSource:              GeminiTokenSourceServiceAccount,
			HasTierSemantics:         false,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported gemini account type: %s", account.Type)
	}
}
