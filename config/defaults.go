// Package config provides configuration loading and defaults.
package config

const (
	DefaultProvider           = "openrouter"
	DefaultModel              = "google/gemini-2.0-flash"
	DefaultDaemonModel        = "google/gemini-2.0-flash"
	DefaultCheckInterval      = "1m"
	DefaultAnomalyWindow      = "5m"
	DefaultDiskThreshold      = 85
	DefaultDiskGrowth         = 5
	DefaultMemWarning         = 85
	DefaultMemCritical        = 95
	DefaultMemGrowth          = 10
	DefaultAuthFailThreshold  = 10
	DefaultProcGrowthMult     = 2.0
	DefaultExecTimeout        = 120
	DefaultMaxOutput          = 200
	DefaultMaxIterations      = 100
	DefaultTemperature        = 0.3
	DefaultMaxTokens          = 4096
)
