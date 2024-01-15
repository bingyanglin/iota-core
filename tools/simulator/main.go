package simulator

import (
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/tools/simulator/pkg/core"
	"github.com/iotaledger/iota-core/tools/simulator/pkg/mock"
)

func main() {
	networkConfigs := mock.NewConfiguration(
		mock.WithNetworkDelay(10, 200),
		mock.WithPacketLoss(0.1, 0.2),
		mock.WithTopology(mock.WattsStrogatz(1, 0.1)),
	)

	sim := core.NewSimulator(
		core.WithNetworkOptions(
			mock.WithTotalNodes(100),
			mock.WithValidators(4),
			mock.WithNetworkConfig(networkConfigs),
		),
	)
	defer sim.Shutdown()

	node1 := sim.AddValidatorNode("node1")
	sim.AddDefaultWallet(node1)
	sim.Run(true, map[string][]options.Option[protocol.Protocol]{})

}
