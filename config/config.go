     1|package config
     2|
     3|import (
     4|	"os"
     5|
     6|	"gopkg.in/yaml.v3"
     7|)
     8|
     9|type Config struct {
    10|	Provider    string `yaml:"provider"`
    11|	APIKey      string `yaml:"api_key"`
    12|	Model       string `yaml:"model"`
    13|	DaemonModel string `yaml:"daemon_model"`
    14|
    15|	Checks   CheckConfig   `yaml:"checks"`
    16|	Alerts   AlertConfig   `yaml:"alerts"`
    17|	Executor ExecConfig    `yaml:"executor"`
    18|	Agent    AgentConfig   `yaml:"agent"`
    19|}
    20|
    21|type CheckConfig struct {
    22|	Interval             string   `yaml:"interval"`
    23|	DiskThreshold        int      `yaml:"disk_threshold"`
    24|	MemWarningThreshold  int      `yaml:"mem_warning_threshold"`
    25|	MemCriticalThreshold int      `yaml:"mem_critical_threshold"`
    26|	Services             []string `yaml:"services"`
    27|}
    28|
    29|type AlertConfig struct {
    30|	Enabled     bool   `yaml:"enabled"`
    31|	WebhookURL  string `yaml:"webhook_url"`
    32|	MinSeverity string `yaml:"min_severity"`
    33|	RetryCount  int    `yaml:"retry_count"`
    34|	RetryDelay  string `yaml:"retry_delay"`
    35|}
    36|
    37|type ExecConfig struct {
    38|	Timeout         int      `yaml:"timeout"`
    39|	MaxOutputLines  int      `yaml:"max_output_lines"`
    40|	AllowedCommands []string `yaml:"allowed_commands"`
    41|