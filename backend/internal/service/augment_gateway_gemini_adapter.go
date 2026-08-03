package service

import "context"

type augmentGatewayGeminiAdapter struct{}

func (a *augmentGatewayGeminiAdapter) Complete(ctx context.Context, req AugmentGatewayProviderRequest) (AugmentGatewayProviderResult, error) {
	return AugmentGatewayProviderResult{}, &AugmentGatewayProviderNotImplementedError{
		Provider:  AugmentGatewayProviderGemini,
		Operation: "complete",
	}
}

func (a *augmentGatewayGeminiAdapter) Stream(ctx context.Context, req AugmentGatewayProviderRequest, emit func(AugmentGatewayProviderChunk) error) error {
	return &AugmentGatewayProviderNotImplementedError{
		Provider:  AugmentGatewayProviderGemini,
		Operation: "stream",
	}
}
