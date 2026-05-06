package service

import "context"

type augmentGatewayOpenAIAdapter struct {
	provider AugmentGatewayProvider
}

func (a *augmentGatewayOpenAIAdapter) Complete(ctx context.Context, req AugmentGatewayProviderRequest) (AugmentGatewayProviderResult, error) {
	provider := a.provider
	if provider == "" {
		provider = AugmentGatewayProviderOpenAI
	}
	return AugmentGatewayProviderResult{}, &AugmentGatewayProviderNotImplementedError{
		Provider:  provider,
		Operation: "complete",
	}
}

func (a *augmentGatewayOpenAIAdapter) Stream(ctx context.Context, req AugmentGatewayProviderRequest, emit func(AugmentGatewayProviderChunk) error) error {
	provider := a.provider
	if provider == "" {
		provider = AugmentGatewayProviderOpenAI
	}
	return &AugmentGatewayProviderNotImplementedError{
		Provider:  provider,
		Operation: "stream",
	}
}
