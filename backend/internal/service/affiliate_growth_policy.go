package service

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

type AffiliateGrowthMode string

const (
	AffiliateGrowthModeLegacy   AffiliateGrowthMode = "legacy"
	AffiliateGrowthModeTieredV1 AffiliateGrowthMode = "tiered_v1"
	affiliateTierRulesMaxCount                      = 32
)

type AffiliateTierRule struct {
	MinEffectiveInvitees int     `json:"min_effective_invitees"`
	RatePercent          float64 `json:"rate_percent"`
}

type AffiliateGrowthPolicy struct {
	rules []AffiliateTierRule
}

func DefaultAffiliateTierRules() []AffiliateTierRule {
	return []AffiliateTierRule{
		{MinEffectiveInvitees: 0, RatePercent: 8},
		{MinEffectiveInvitees: 3, RatePercent: 10},
		{MinEffectiveInvitees: 10, RatePercent: 12},
		{MinEffectiveInvitees: 25, RatePercent: 15},
	}
}

func DefaultAffiliateTierRulesJSON() string {
	raw, err := json.Marshal(DefaultAffiliateTierRules())
	if err != nil {
		panic("marshal static affiliate tier rules: " + err.Error())
	}
	return string(raw)
}

func IsValidAffiliateGrowthMode(mode AffiliateGrowthMode) bool {
	switch mode {
	case AffiliateGrowthModeLegacy, AffiliateGrowthModeTieredV1:
		return true
	default:
		return false
	}
}

func NewAffiliateGrowthPolicy(rules []AffiliateTierRule) (*AffiliateGrowthPolicy, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("affiliate tier rules must not be empty")
	}
	if len(rules) > affiliateTierRulesMaxCount {
		return nil, fmt.Errorf("affiliate tier rules exceed maximum count %d", affiliateTierRulesMaxCount)
	}

	normalized := append([]AffiliateTierRule(nil), rules...)
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].MinEffectiveInvitees < normalized[j].MinEffectiveInvitees
	})

	for i, rule := range normalized {
		if rule.MinEffectiveInvitees < 0 {
			return nil, fmt.Errorf("affiliate tier threshold must be non-negative")
		}
		if math.IsNaN(rule.RatePercent) || math.IsInf(rule.RatePercent, 0) ||
			rule.RatePercent < AffiliateRebateRateMin || rule.RatePercent > AffiliateRebateRateMax {
			return nil, fmt.Errorf("affiliate tier rate must be between %.0f and %.0f", AffiliateRebateRateMin, AffiliateRebateRateMax)
		}
		if i > 0 && normalized[i-1].MinEffectiveInvitees == rule.MinEffectiveInvitees {
			return nil, fmt.Errorf("affiliate tier thresholds must be unique")
		}
	}
	if normalized[0].MinEffectiveInvitees != 0 {
		return nil, fmt.Errorf("affiliate tier rules must start at zero")
	}

	return &AffiliateGrowthPolicy{rules: normalized}, nil
}

func ParseAffiliateTierRules(raw string) ([]AffiliateTierRule, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("affiliate tier rules must not be empty")
	}

	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var rules []AffiliateTierRule
	if err := decoder.Decode(&rules); err != nil {
		return nil, fmt.Errorf("decode affiliate tier rules: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("affiliate tier rules contain trailing JSON value")
		}
		return nil, fmt.Errorf("decode affiliate tier rules trailing data: %w", err)
	}

	policy, err := NewAffiliateGrowthPolicy(rules)
	if err != nil {
		return nil, err
	}
	return policy.Rules(), nil
}

func (p *AffiliateGrowthPolicy) Rules() []AffiliateTierRule {
	if p == nil {
		return nil
	}
	return append([]AffiliateTierRule(nil), p.rules...)
}

func (p *AffiliateGrowthPolicy) RatePercent(effectiveInvitees int) float64 {
	if p == nil || len(p.rules) == 0 {
		return 0
	}
	if effectiveInvitees < 0 {
		effectiveInvitees = 0
	}

	index := sort.Search(len(p.rules), func(i int) bool {
		return p.rules[i].MinEffectiveInvitees > effectiveInvitees
	})
	if index == 0 {
		return p.rules[0].RatePercent
	}
	return p.rules[index-1].RatePercent
}
