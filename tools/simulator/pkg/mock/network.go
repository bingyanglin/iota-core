package mock

import (
	"fmt"
	"math/rand"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/google/martian/log"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/iotaledger/hive.go/crypto"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/iota-core/pkg/network"
)

// region network ////////////////////////////////////////////////////////////////////////////////////////////////

type PeeringStrategy func(network *Network)

type Configuration struct {
	minDelay        time.Duration
	maxDelay        time.Duration
	minPacketLoss   float64
	maxPacketLoss   float64
	peeringStrategy PeeringStrategy
}

func NewConfiguration(opts ...options.Option[Configuration]) *Configuration {
	return options.Apply(&Configuration{}, opts)
}

func (c *Configuration) RandomNetworkDelay() time.Duration {
	return c.minDelay + time.Duration(crypto.Randomness.Float64()*float64(c.maxDelay-c.minDelay))
}

func (c *Configuration) ExpRandomNetworkDelay() time.Duration {
	return time.Duration(rand.ExpFloat64() * (float64(c.maxDelay+c.minDelay) / 2))
}

func (c *Configuration) RandomPacketLoss() float64 {
	return c.minPacketLoss + crypto.Randomness.Float64()*(c.maxPacketLoss-c.minPacketLoss)
}

type Network struct {
	totalNodes int
	validators int
	configs    *Configuration

	dispatchers      map[peer.ID]*Endpoint
	dispatchersMutex syncutils.RWMutex
}

func NewNetwork(opts ...options.Option[Network]) *Network {
	return options.Apply(&Network{
		dispatchers: make(map[peer.ID]*Endpoint),
	}, opts)
}

func (n *Network) JoinWithEndpointID(endpointID peer.ID) *Endpoint {
	return n.JoinWithEndpoint(newMockedEndpoint(endpointID, n))
}

func (n *Network) JoinWithEndpoint(endpoint *Endpoint) *Endpoint {
	n.dispatchersMutex.Lock()
	defer n.dispatchersMutex.Unlock()

	n.dispatchers[endpoint.id] = endpoint

	return endpoint
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Endpoint ///////////////////////////////////////////////////////////////////////////////////////////////

type Endpoint struct {
	id        peer.ID
	network   *Network
	neighbors map[peer.ID]struct{}
	handler   func(peer.ID, proto.Message) error
}

func newMockedEndpoint(id peer.ID, n *Network) *Endpoint {
	return &Endpoint{
		id:        id,
		network:   n,
		neighbors: make(map[peer.ID]struct{}),
	}
}

func (e *Endpoint) LocalPeerID() peer.ID {
	return e.id
}

func (e *Endpoint) RegisterProtocol(_ func() proto.Message, handler func(peer.ID, proto.Message) error) {
	e.network.dispatchersMutex.Lock()
	defer e.network.dispatchersMutex.Unlock()

	e.handler = handler
}

func (e *Endpoint) UnregisterProtocol() {
	e.network.dispatchersMutex.Lock()
	defer e.network.dispatchersMutex.Unlock()

	e.handler = nil
	delete(e.network.dispatchers, e.id)
}

func (e *Endpoint) Shutdown() {
	e.UnregisterProtocol()
}

func (e *Endpoint) Send(packet proto.Message, to ...peer.ID) {
	e.network.dispatchersMutex.RLock()
	defer e.network.dispatchersMutex.RUnlock()

	if len(to) == 0 {
		to = lo.Keys(e.network.dispatchers)
	}

	for _, id := range to {
		if id == e.id {
			continue
		}

		dispatcher, exists := e.network.dispatchers[id]
		if !exists {
			fmt.Println(e.id, "ERROR: no dispatcher for ", id)
			continue
		}

		go func() {
			e.network.dispatchersMutex.RLock()
			defer e.network.dispatchersMutex.RUnlock()

			// Add network delay
			time.Sleep(e.network.configs.RandomNetworkDelay())

			if dispatcher.handler != nil {
				if err := dispatcher.handler(e.id, packet); err != nil {
					fmt.Println(e.id, "ERROR: ", err)
				}
			}
		}()
	}
}

var _ network.Endpoint = &Endpoint{}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

func WattsStrogatz(meanDegree int, randomness float64) PeeringStrategy {
	if meanDegree%2 != 0 {
		panic("Invalid argument: meanDegree needs to be even")
	}

	return func(network *Network) {
		nodeSlice := lo.Keys(network.dispatchers)
		nodeCount := len(nodeSlice)
		graph := make(map[int]map[int]bool)

		for nodeID := 0; nodeID < nodeCount; nodeID++ {
			graph[nodeID] = make(map[int]bool)

			for j := nodeID + 1; j <= nodeID+meanDegree/2; j++ {
				graph[nodeID][j%nodeCount] = true
			}
		}

		for tail, edges := range graph {
			for head := range edges {
				if crypto.Randomness.Float64() < randomness {
					newHead := crypto.Randomness.Intn(nodeCount)
					for newHead == tail || graph[newHead][tail] || edges[newHead] {
						newHead = crypto.Randomness.Intn(nodeCount)
					}

					delete(edges, head)
					edges[newHead] = true
				}
			}
		}
		for sourceNodeID, targetNodeIDs := range graph {
			for targetNodeID := range targetNodeIDs {
				randomNetworkDelay := network.configs.RandomNetworkDelay()
				randomPacketLoss := network.configs.RandomPacketLoss()

				network.dispatchers[nodeSlice[sourceNodeID]].neighbors[nodeSlice[targetNodeID]] = struct{}{}
				network.dispatchers[nodeSlice[targetNodeID]].neighbors[nodeSlice[sourceNodeID]] = struct{}{}

				log.Debugf("Connecting %s <-> %s [network delay (%s), packet loss (%0.4f%%)] ... [DONE]", nodeSlice[sourceNodeID], nodeSlice[targetNodeID], randomNetworkDelay, randomPacketLoss*100)
			}
		}
		totalNeighborCount := 0
		for id, peer := range network.dispatchers {
			log.Debugf("%s %d", id.String(), len(peer.neighbors))
			totalNeighborCount += len(peer.neighbors)
		}
		log.Infof("Average number of neighbors: %.1f", float64(totalNeighborCount)/float64(nodeCount))
	}
}

func WithTotalNodes(total int) options.Option[Network] {
	return func(n *Network) {
		n.totalNodes = total
	}
}

func WithValidators(total int) options.Option[Network] {
	return func(n *Network) {
		n.validators = total
	}
}

func WithNetworkConfig(config *Configuration) options.Option[Network] {
	return func(n *Network) {
		n.configs = config
	}
}

func WithNetworkDelay(minDelay time.Duration, maxDelay time.Duration) options.Option[Configuration] {
	return func(c *Configuration) {
		c.minDelay = minDelay
		c.maxDelay = maxDelay
	}
}

func WithPacketLoss(minPacketLoss float64, maxPacketLoss float64) options.Option[Configuration] {
	return func(c *Configuration) {
		c.minPacketLoss = minPacketLoss
		c.maxPacketLoss = maxPacketLoss
	}
}

func WithTopology(strategy PeeringStrategy) options.Option[Configuration] {
	return func(c *Configuration) {
		c.peeringStrategy = strategy
	}
}
