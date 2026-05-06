package service

import (
	"errors"
	"fmt"
	"strings"
)

const AugmentGatewayDefaultModelID = "gpt-5.4"

type AugmentGatewayModelUnavailableKind string

const (
	AugmentGatewayModelUnavailableUnknown  AugmentGatewayModelUnavailableKind = "unknown"
	AugmentGatewayModelUnavailableDisabled AugmentGatewayModelUnavailableKind = "disabled"
)

type AugmentGatewayRoutedModel struct {
	RequestedModelID string
	Model            AugmentGatewayModel
	Provider         AugmentGatewayProvider
	UpstreamModel    string
}

type AugmentGatewayModelUnavailableError struct {
	ModelID string
	Kind    AugmentGatewayModelUnavailableKind
}

func (e *AugmentGatewayModelUnavailableError) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch e.Kind {
	case AugmentGatewayModelUnavailableDisabled:
		return fmt.Sprintf("augment gateway model %q is not enabled", e.ModelID)
	case AugmentGatewayModelUnavailableUnknown:
		fallthrough
	default:
		return fmt.Sprintf("augment gateway model %q is not supported", e.ModelID)
	}
}

type AugmentGatewayRouter struct {
	registry       *AugmentGatewayModelRegistry
	defaultModelID string
}

func NewAugmentGatewayRouter(registry *AugmentGatewayModelRegistry, defaultModelID ...string) *AugmentGatewayRouter {
	modelID := AugmentGatewayDefaultModelID
	if len(defaultModelID) > 0 {
		if trimmed := strings.TrimSpace(defaultModelID[0]); trimmed != "" {
			modelID = trimmed
		}
	}
	return &AugmentGatewayRouter{
		registry:       registry,
		defaultModelID: modelID,
	}
}

func (r *AugmentGatewayRouter) Resolve(modelID string) (AugmentGatewayRoutedModel, error) {
	if r == nil || r.registry == nil {
		return AugmentGatewayRoutedModel{}, &AugmentGatewayModelUnavailableError{
			ModelID: strings.TrimSpace(modelID),
			Kind:    AugmentGatewayModelUnavailableUnknown,
		}
	}

	requestedModelID := strings.TrimSpace(modelID)
	if requestedModelID == "" {
		requestedModelID = r.defaultModelID
	}

	model, ok := r.registry.Resolve(requestedModelID)
	if !ok {
		return AugmentGatewayRoutedModel{}, &AugmentGatewayModelUnavailableError{
			ModelID: requestedModelID,
			Kind:    AugmentGatewayModelUnavailableUnknown,
		}
	}
	if !r.registry.IsVisible(requestedModelID) {
		return AugmentGatewayRoutedModel{}, &AugmentGatewayModelUnavailableError{
			ModelID: requestedModelID,
			Kind:    AugmentGatewayModelUnavailableDisabled,
		}
	}

	return AugmentGatewayRoutedModel{
		RequestedModelID: requestedModelID,
		Model:            model,
		Provider:         model.Provider,
		UpstreamModel:    model.UpstreamModel,
	}, nil
}

func IsAugmentGatewayModelUnavailable(err error) (*AugmentGatewayModelUnavailableError, bool) {
	var unavailable *AugmentGatewayModelUnavailableError
	if errors.As(err, &unavailable) {
		return unavailable, true
	}
	return nil, false
}
