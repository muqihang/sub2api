package service

import "github.com/Wei-Shaw/sub2api/internal/config"

type AugmentGatewayService struct {
	cfg       *config.Config
	registry  *AugmentGatewayModelRegistry
	router    *AugmentGatewayRouter
	executor  AugmentGatewayProviderExecutor
	turnStore *AugmentGatewayReasoningTurnStore
}

func NewAugmentGatewayService(
	cfg *config.Config,
	registry *AugmentGatewayModelRegistry,
	router *AugmentGatewayRouter,
	executor AugmentGatewayProviderExecutor,
	turnStore *AugmentGatewayReasoningTurnStore,
) *AugmentGatewayService {
	return &AugmentGatewayService{
		cfg:       cfg,
		registry:  registry,
		router:    router,
		executor:  executor,
		turnStore: turnStore,
	}
}

func (s *AugmentGatewayService) Registry() *AugmentGatewayModelRegistry {
	if s == nil {
		return nil
	}
	return s.registry
}

func (s *AugmentGatewayService) Router() *AugmentGatewayRouter {
	if s == nil {
		return nil
	}
	return s.router
}

func (s *AugmentGatewayService) Executor() AugmentGatewayProviderExecutor {
	if s == nil {
		return nil
	}
	return s.executor
}

func (s *AugmentGatewayService) TurnStore() *AugmentGatewayReasoningTurnStore {
	if s == nil {
		return nil
	}
	return s.turnStore
}
