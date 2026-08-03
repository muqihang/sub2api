package service

import "context"

type augmentGatewayAnthropicAdapter struct{}

func (a *augmentGatewayAnthropicAdapter) Complete(ctx context.Context, req AugmentGatewayProviderRequest) (AugmentGatewayProviderResult, error) {
	return AugmentGatewayProviderResult{}, &AugmentGatewayProviderNotImplementedError{
		Provider:  AugmentGatewayProviderAnthropic,
		Operation: "complete",
	}
}

func (a *augmentGatewayAnthropicAdapter) Stream(ctx context.Context, req AugmentGatewayProviderRequest, emit func(AugmentGatewayProviderChunk) error) error {
	return &AugmentGatewayProviderNotImplementedError{
		Provider:  AugmentGatewayProviderAnthropic,
		Operation: "stream",
	}
}
