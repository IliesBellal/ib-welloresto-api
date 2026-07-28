package config

type PlanningConfig struct {
	PublicBaseURL string
}

func loadPlanningConfig() PlanningConfig {
	return PlanningConfig{
		PublicBaseURL: getEnv("PUBLIC_PLANNING_BASE_URL", ""),
	}
}
