package config

import (
	"time"
)

// Decision config keys
const (
	// Decision routing
	KeyDecisionRoutesDefault = "decision.routes.default"
	KeyDecisionRoutesUrgent  = "decision.routes.urgent"

	// Decision settings
	KeyDecisionDefaultTimeout    = "decision.settings.default-timeout"
	KeyDecisionRemindInterval    = "decision.settings.remind-interval"
	KeyDecisionMaxReminders      = "decision.settings.max-reminders"
	KeyDecisionMaxIterations     = "decision.settings.max-iterations"
	KeyDecisionAutoAcceptOnMax   = "decision.settings.auto-accept-on-max"
)

// DecisionSettings contains all decision point configuration.
// hq-946577.33: Decision config schema
type DecisionSettings struct {
	// Routes defines notification channels for different priority levels.
	// Each route is a list of delivery methods: "email:user", "webhook", "sms:user"
	Routes DecisionRoutes `json:"routes" yaml:"routes"`

	// Settings contains timing and behavior parameters.
	Settings DecisionBehavior `json:"settings" yaml:"settings"`
}

// DecisionRoutes maps priority levels to notification channels.
type DecisionRoutes struct {
	// Default routes for normal priority decisions
	Default []string `json:"default" yaml:"default"`

	// Urgent routes for high priority decisions (P0, P1)
	Urgent []string `json:"urgent" yaml:"urgent"`
}

// DecisionBehavior contains decision point timing and behavior settings.
type DecisionBehavior struct {
	// DefaultTimeout is how long to wait for a response before timeout (default: 24h)
	DefaultTimeout time.Duration `json:"default_timeout" yaml:"default-timeout"`

	// RemindInterval is how often to send reminders (default: 4h)
	RemindInterval time.Duration `json:"remind_interval" yaml:"remind-interval"`

	// MaxReminders is the maximum number of reminders to send (default: 3)
	MaxReminders int `json:"max_reminders" yaml:"max-reminders"`

	// MaxIterations is the maximum refinement iterations (default: 3)
	MaxIterations int `json:"max_iterations" yaml:"max-iterations"`

	// AutoAcceptOnMax controls whether to auto-accept text guidance on max iterations
	// If true, the last text guidance is accepted automatically when max iterations reached.
	// If false (default), the user must select an option.
	AutoAcceptOnMax bool `json:"auto_accept_on_max" yaml:"auto-accept-on-max"`
}

// RegisterDecisionDefaults registers default values for decision configuration.
// Called from Initialize() in config.go.
func RegisterDecisionDefaults() {
	if v == nil {
		return
	}

	// Default notification routes
	v.SetDefault(KeyDecisionRoutesDefault, []string{"email", "webhook"})
	v.SetDefault(KeyDecisionRoutesUrgent, []string{"email", "sms", "webhook"})

	// Default timing settings
	v.SetDefault(KeyDecisionDefaultTimeout, "24h")
	v.SetDefault(KeyDecisionRemindInterval, "4h")
	v.SetDefault(KeyDecisionMaxReminders, 3)
	v.SetDefault(KeyDecisionMaxIterations, 3)
	v.SetDefault(KeyDecisionAutoAcceptOnMax, false)
}

// GetDecisionSettings returns the current decision point configuration.
func GetDecisionSettings() DecisionSettings {
	return DecisionSettings{
		Routes: DecisionRoutes{
			Default: GetStringSlice(KeyDecisionRoutesDefault),
			Urgent:  GetStringSlice(KeyDecisionRoutesUrgent),
		},
		Settings: DecisionBehavior{
			DefaultTimeout:  GetDuration(KeyDecisionDefaultTimeout),
			RemindInterval:  GetDuration(KeyDecisionRemindInterval),
			MaxReminders:    GetInt(KeyDecisionMaxReminders),
			MaxIterations:   GetInt(KeyDecisionMaxIterations),
			AutoAcceptOnMax: GetBool(KeyDecisionAutoAcceptOnMax),
		},
	}
}

// GetDecisionDefaultTimeout returns the default timeout for decision points.
func GetDecisionDefaultTimeout() time.Duration {
	return GetDuration(KeyDecisionDefaultTimeout)
}

// GetDecisionMaxIterations returns the maximum number of refinement iterations.
func GetDecisionMaxIterations() int {
	return GetInt(KeyDecisionMaxIterations)
}

// GetDecisionRemindInterval returns the interval between reminders.
func GetDecisionRemindInterval() time.Duration {
	return GetDuration(KeyDecisionRemindInterval)
}

// GetDecisionMaxReminders returns the maximum number of reminders.
func GetDecisionMaxReminders() int {
	return GetInt(KeyDecisionMaxReminders)
}

// GetDecisionAutoAcceptOnMax returns whether to auto-accept on max iterations.
func GetDecisionAutoAcceptOnMax() bool {
	return GetBool(KeyDecisionAutoAcceptOnMax)
}

// GetDecisionRoutes returns notification routes for the given priority.
// Priority 0-1 uses "urgent" routes, others use "default" routes.
func GetDecisionRoutes(priority int) []string {
	if priority <= 1 {
		return GetStringSlice(KeyDecisionRoutesUrgent)
	}
	return GetStringSlice(KeyDecisionRoutesDefault)
}
