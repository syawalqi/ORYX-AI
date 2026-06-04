package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Provider    string `yaml:"provider"`
	APIKey      string `yaml:"api_key"`
	Model       string `yaml:"model"`
	DaemonModel string `yaml:"daemon_model"`

	Checks   CheckConfig   `yaml:"checks"`
	Alerts   AlertConfig   `yaml:"alerts"`
	Executor ExecConfig    `yaml:"executor"`
	Agent    AgentConfig   `yaml:"agent"`
}

type CheckConfig struct {
	Interval             string   `yaml:"interval"`
	AnomalyWindow        string   `yaml:"anomaly_window"`
	DiskThreshold        int      `yaml:"disk_threshold"`
	MemWarningThreshold  int      `yaml:"mem_warning_threshold"`
	MemCriticalThreshold int      `yaml:"mem_critical_threshold"`
	DiskGrowthThreshold  int      `yaml:"disk_growth_threshold"`
	MemGrowthThreshold   int      `yaml:"mem_growth_threshold"`
	AuthFailThreshold    int      `yaml:"auth_fail_threshold"`
	ProcGrowthMultiplier float64  `yaml:"proc_growth_multiplier"`
	Services             []string `yaml:"services"`
}

type AlertConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Delivery    string `yaml:"delivery"`
	WebhookURL  string `yaml:"webhook_url"`
	TelegramToken string `yaml:"telegram_token"`
	TelegramChat  string `yaml:"telegram_chat"`
	MinSeverity string `yaml:"min_severity"`
	RetryCount  int    `yaml:"retry_count"`
	RetryDelay  string `yaml:"retry_delay"`
}

type ExecConfig struct {
	Timeout         int      `yaml:"timeout"`
	MaxOutputLines  int      `yaml:"max_output_lines"`
	AllowedCommands []string `yaml:"allowed_commands"`
	BlockedCommands []string `yaml:"blocked_commands"`
}

type AgentConfig struct {
	MaxIterations int     `yaml:"max_iterations"`
	Temperature   float64 `yaml:"temperature"`
	MaxTokens     int     `yaml:"max_tokens"`
}

func Default() *Config {
	return &Config{
		Provider:    DefaultProvider,
		Model:       DefaultModel,
		DaemonModel: DefaultDaemonModel,
		Checks: CheckConfig{
			Interval:             DefaultCheckInterval,
			AnomalyWindow:        DefaultAnomalyWindow,
			DiskThreshold:        DefaultDiskThreshold,
			DiskGrowthThreshold:  DefaultDiskGrowth,
			MemWarningThreshold:  DefaultMemWarning,
			MemCriticalThreshold: DefaultMemCritical,
			MemGrowthThreshold:   DefaultMemGrowth,
			AuthFailThreshold:    DefaultAuthFailThreshold,
			ProcGrowthMultiplier: DefaultProcGrowthMult,
			Services:             []string{},
		},
		Alerts: AlertConfig{
			Enabled:     false,
			Delivery:    "stdout",
			MinSeverity: "warning",
			RetryCount:  3,
			RetryDelay:  "30s",
		},
		Executor: ExecConfig{
			Timeout:        DefaultExecTimeout,
			MaxOutputLines: DefaultMaxOutput,
			BlockedCommands: []string{
				"rm -rf /",
				"mkfs",
				"dd if=",
			},
		},
		Agent: AgentConfig{
			MaxIterations: DefaultMaxIterations,
			Temperature:   DefaultTemperature,
			MaxTokens:     DefaultMaxTokens,
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = []byte(os.ExpandEnv(string(data)))

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
