package service

import "time"

const (
	PoolUtilizationStatusBehind   = "behind"
	PoolUtilizationStatusOnTrack  = "on_track"
	PoolUtilizationStatusAhead    = "ahead"
	PoolUtilizationActionCatchUp  = "catch_up"
	PoolUtilizationActionSlowDown = "slow_down"
	PoolUtilizationActionMaintain = "maintain"
	PoolUtilizationActionCooldown = "cooldown"
)

type PoolUtilizationBudgetInput struct {
	Profile        string
	Utilization7d  float64
	WindowAge      time.Duration
	CooldownActive bool
}

type PoolUtilizationBudgetLedger struct {
	Profile                 string  `json:"profile"`
	TargetDays              int     `json:"target_days"`
	TargetUtilizationBucket string  `json:"target_utilization_bucket"`
	Status                  string  `json:"status"`
	Action                  string  `json:"action"`
	RecommendedWeight       float64 `json:"recommended_weight"`
	QueuePriority           int     `json:"queue_priority"`
	HardBlock               bool    `json:"hard_block"`
}

func BuildPoolUtilizationBudgetLedger(input PoolUtilizationBudgetInput) PoolUtilizationBudgetLedger {
	profile := normalizePoolProfile(input.Profile)
	days, target := 7, 0.95
	if profile == PoolProfileAggressive {
		days, target = 3, 0.975
	}
	if input.CooldownActive {
		return PoolUtilizationBudgetLedger{Profile: profile, TargetDays: days, TargetUtilizationBucket: targetBucket(profile), Status: PoolUtilizationStatusBehind, Action: PoolUtilizationActionCooldown, RecommendedWeight: 0, QueuePriority: -10, HardBlock: false}
	}
	elapsedDays := input.WindowAge.Hours() / 24
	if elapsedDays < 0 {
		elapsedDays = 0
	}
	if elapsedDays > float64(days) {
		elapsedDays = float64(days)
	}
	expected := target * (elapsedDays / float64(days))
	actual := budgetClamp01(input.Utilization7d)
	tolerance := 0.05
	if profile == PoolProfileAggressive {
		tolerance = 0.025
	}
	out := PoolUtilizationBudgetLedger{Profile: profile, TargetDays: days, TargetUtilizationBucket: targetBucket(profile), RecommendedWeight: 1, QueuePriority: 0, HardBlock: false}
	if actual+tolerance < expected {
		out.Status = PoolUtilizationStatusBehind
		out.Action = PoolUtilizationActionCatchUp
		if profile == PoolProfileAggressive {
			out.RecommendedWeight = 1.8
			out.QueuePriority = 20
		} else {
			out.RecommendedWeight = 1.2
			out.QueuePriority = 5
		}
	} else if actual > expected+tolerance {
		out.Status = PoolUtilizationStatusAhead
		out.Action = PoolUtilizationActionSlowDown
		if profile == PoolProfileAggressive {
			out.RecommendedWeight = 0.65
			out.QueuePriority = -5
		} else {
			out.RecommendedWeight = 0.8
			out.QueuePriority = -3
		}
	} else {
		out.Status = PoolUtilizationStatusOnTrack
		out.Action = PoolUtilizationActionMaintain
	}
	return out
}

func targetBucket(profile string) string {
	if normalizePoolProfile(profile) == PoolProfileAggressive {
		return "pct_95_100_in_3d"
	}
	return "pct_90_100_in_7d"
}
