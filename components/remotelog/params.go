package remotelog

import "github.com/iotaledger/hive.go/app"

// ParametersRemoteLog contains the definition of the parameters used by the remotelog plugin.
type ParametersRemoteLog struct {
	// Enabled defines whether the remote log plugin is enabled.
	Enabled bool `default:"false" usage:"whether the remote log component is enabled"`

	// ServerAddress defines the server address that will receive the logs.
	ServerAddress string `default:"metrics-01.devnet.shimmer.iota.cafe:5213" usage:"RemoteLog server address"`
}

// ParamsRemoteLog contains the configuration used by the remotelog plugin.
var ParamsRemoteLog = &ParametersRemoteLog{}

var params = &app.ComponentParams{
	Params: map[string]any{
		"remotelog": ParamsRemoteLog,
	},
	Masked: nil,
}
