package simulator

import (
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/tools/simulator/pkg/core"
)

func main() {
	sim := core.NewSimulator()
	defer sim.Shutdown()

	node1 := sim.AddValidatorNode("node1")
	wallet := sim.AddDefaultWallet(node1)
	sim.Run(true, map[string][]options.Option[protocol.Protocol]{})

	tx1 := wallet.CreateBasicOutputsEquallyFromInput("tx1", 1, "Genesis:0")

	tx2 := wallet.CreateBasicOutputsEquallyFromInput("tx2", 1, "tx1:0")

	sim.IssueBasicBlockWithOptions("block1", wallet, tx2)

	sim.IssueBasicBlockWithOptions("block2", wallet, tx1)

}
