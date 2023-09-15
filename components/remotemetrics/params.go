package remotemetrics

import "github.com/iotaledger/hive.go/app"

// ParametersDefinition contains the definition of the parameters used by the remotelog plugin.
type ParametersRemoteMetrics struct {
	// Enabled defines whether the remote metrics plugin is enabled.
	Enabled bool `default:"false" usage:"whether the remote metrics plugin is enabled"`
	// MetricsLevelMetricsLevel used limit the amount of metrics sent to metrics collection service. The higher the value, the less logs is sent
	MetricsLevel uint8 `default:"1" usage:"Numeric value to limit the amount of metrics sent to metrics collection service. The higher the value, the less logs is sent"`
}

// Parameters contains the configuration used by the remotelog plugin.

var ParamsRemoteMetrics = &ParametersRemoteMetrics{}

var params = &app.ComponentParams{
	Params: map[string]any{
		"remotemetrics": ParamsRemoteMetrics,
	},
	Masked: nil,
}
