package p2p

import (
	"context"

	"github.com/libp2p/go-libp2p/core/host"
	p2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"google.golang.org/protobuf/proto"

	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/log"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/iota-core/pkg/network"
	"github.com/iotaledger/iota-core/pkg/network/p2p/autopeering"
	"github.com/iotaledger/iota-core/pkg/network/p2p/manualpeering"
)

// ProtocolHandler holds callbacks to handle a protocol.
type ProtocolHandler struct {
	PacketFactory func() proto.Message
	PacketHandler func(peer.ID, proto.Message) error
}

// The Manager handles the connected neighbors.
type Manager struct {
	logger log.Logger

	// Fired when a neighbor connection has been established.
	neighborAdded *event.Event1[network.Neighbor]
	// Fired when a neighbor has been removed.
	neighborRemoved *event.Event1[network.Neighbor]

	libp2pHost host.Host

	ctx context.Context

	shutdownMutex syncutils.RWMutex
	isShutdown    bool

	neighbors *shrinkingmap.ShrinkingMap[peer.ID, *neighbor]

	protocolHandler      *ProtocolHandler
	protocolHandlerMutex syncutils.RWMutex
	onBlockSentCallback  func()

	addrFilter    network.AddressFilter
	autoPeering   *autopeering.Manager
	manualPeering *manualpeering.Manager
}

var _ network.Manager = (*Manager)(nil)

// NewManager creates a new Manager.
func NewManager(logger log.Logger, libp2pHost host.Host, maxAutopeeringPeers int, allowLocalAutopeering bool, onBlockSentCallback func()) *Manager {
	m := &Manager{
		logger:              logger,
		libp2pHost:          libp2pHost,
		neighborAdded:       event.New1[network.Neighbor](),
		neighborRemoved:     event.New1[network.Neighbor](),
		neighbors:           shrinkingmap.New[peer.ID, *neighbor](),
		onBlockSentCallback: onBlockSentCallback,
		addrFilter:          network.PublicOnlyAddressesFilter(allowLocalAutopeering),
	}

	m.autoPeering = autopeering.NewManager(maxAutopeeringPeers, m, libp2pHost, m.addrFilter, logger)
	m.manualPeering = manualpeering.NewManager(m, logger)

	return m
}

// RegisterProtocol registers the handler for the protocol within the manager.
func (m *Manager) RegisterProtocol(factory func() proto.Message, handler func(peer.ID, proto.Message) error) {
	m.protocolHandlerMutex.Lock()
	defer m.protocolHandlerMutex.Unlock()

	m.protocolHandler = &ProtocolHandler{
		PacketFactory: factory,
		PacketHandler: handler,
	}

	m.libp2pHost.SetStreamHandler(network.CoreProtocolID, m.handleStream)
}

// UnregisterProtocol unregisters the handler for the protocol.
func (m *Manager) UnregisterProtocol() {
	m.protocolHandlerMutex.Lock()
	defer m.protocolHandlerMutex.Unlock()

	m.libp2pHost.RemoveStreamHandler(network.CoreProtocolID)
	m.protocolHandler = nil
}

func (m *Manager) OnNeighborAdded(handler func(network.Neighbor)) *event.Hook[func(network.Neighbor)] {
	return m.neighborAdded.Hook(handler)
}

func (m *Manager) OnNeighborRemoved(handler func(network.Neighbor)) *event.Hook[func(network.Neighbor)] {
	return m.neighborRemoved.Hook(handler)
}

// DialPeer connects to a peer.
func (m *Manager) DialPeer(ctx context.Context, peer *network.Peer) error {
	m.protocolHandlerMutex.RLock()
	defer m.protocolHandlerMutex.RUnlock()

	if m.protocolHandler == nil {
		return ierrors.New("no protocol handler registered to dial peer")
	}

	// Do not try to dial already connected peers.
	if m.NeighborExists(peer.ID) {
		return ierrors.WithMessagef(network.ErrDuplicatePeer, "peer %s already exists", peer.ID.String())
	}

	if !m.allowPeer(peer.ID) {
		return ierrors.WithMessagef(network.ErrMaxAutopeeringPeersReached, "peer %s is not allowed", peer.ID.String())
	}

	cancelCtx := ctx

	stream, err := m.P2PHost().NewStream(cancelCtx, peer.ID, network.CoreProtocolID)
	if err != nil {
		return ierrors.Wrapf(err, "dial %s / %s failed to open stream for proto %s", peer.PeerAddresses, peer.ID.String(), network.CoreProtocolID)
	}

	ps := NewPacketsStream(stream, m.protocolHandler.PacketFactory)
	if err := ps.sendNegotiation(); err != nil {
		m.closeStream(stream)

		return ierrors.Wrapf(err, "dial %s / %s failed to send negotiation for proto %s", peer.PeerAddresses, peer.ID.String(), network.CoreProtocolID)
	}

	m.logger.LogDebugf("outgoing stream negotiated, id: %s, addr: %s, proto: %s", peer.ID, ps.Conn().RemoteMultiaddr(), network.CoreProtocolID)

	if err := m.addNeighbor(peer, ps, m.onBlockSentCallback); err != nil {
		m.closeStream(stream)

		return ierrors.Wrapf(err, "failed to add neighbor %s", peer.ID.String())
	}

	return nil
}

// Start starts the manager and initiates manual- and autopeering.
func (m *Manager) Start(ctx context.Context, networkID string, bootstrapPeers []peer.AddrInfo) error {
	m.ctx = ctx

	m.manualPeering.Start()

	if m.autoPeering.MaxNeighbors() > 0 {
		return m.autoPeering.Start(ctx, networkID, bootstrapPeers)
	}

	return nil
}

// Shutdown stops the manager and closes all established connections.
func (m *Manager) Shutdown() {
	m.shutdownMutex.Lock()
	defer m.shutdownMutex.Unlock()

	if m.isShutdown {
		return
	}
	m.isShutdown = true

	if err := m.autoPeering.Stop(); err != nil {
		m.logger.LogErrorf("failed to stop autopeering: %s", err.Error())
	}

	if err := m.manualPeering.Stop(); err != nil {
		m.logger.LogErrorf("failed to stop manualpeering: %s", err.Error())
	}

	m.dropAllNeighbors()

	m.UnregisterProtocol()

	if err := m.libp2pHost.Close(); err != nil {
		m.logger.LogErrorf("failed to close libp2p host: %s", err.Error())
	}
}

func (m *Manager) AddManualPeer(multiAddr multiaddr.Multiaddr) (*network.Peer, error) {
	return m.manualPeering.AddPeer(multiAddr)
}

func (m *Manager) ManualPeer(id peer.ID) (*network.Peer, error) {
	return m.manualPeering.Peer(id)
}

func (m *Manager) ManualPeers(onlyConnected ...bool) []*network.Peer {
	return m.manualPeering.GetPeers(onlyConnected...)
}

// LocalPeerID returns the local peer ID.
func (m *Manager) LocalPeerID() peer.ID {
	return m.libp2pHost.ID()
}

// P2PHost returns the lib-p2p host.
func (m *Manager) P2PHost() host.Host {
	return m.libp2pHost
}

// RemovePeer disconnects the neighbor with the given ID
// and removes it from manual peering in case it was added manually.
func (m *Manager) RemovePeer(id peer.ID) error {
	if m.manualPeering.IsPeerKnown(id) {
		// RemovePeer calls DisconnectNeighbor internally
		if err := m.manualPeering.RemovePeer(id); err != nil {
			return err
		}

		return nil
	}

	if err := m.DisconnectNeighbor(id); err != nil && !ierrors.Is(err, network.ErrUnknownPeer) {
		return ierrors.Wrapf(err, "failed to drop peer %s in the gossip layer", id.String())
	}

	return nil
}

// DisconnectNeighbor disconnects the neighbor with the given ID.
func (m *Manager) DisconnectNeighbor(id peer.ID) error {
	nbr, err := m.neighbor(id)
	if err != nil {
		return ierrors.WithStack(err)
	}
	nbr.Close()

	return nil
}

// Send sends a message with the specific protocol to a set of neighbors.
func (m *Manager) Send(packet proto.Message, to ...peer.ID) {
	var neighbors []*neighbor
	if len(to) == 0 {
		neighbors = m.allNeighbors()
	} else {
		neighbors = m.neighborsByID(to)
	}

	for _, nbr := range neighbors {
		nbr.Enqueue(packet, network.CoreProtocolID)
	}
}

// Neighbors returns all the neighbors that are currently connected.
func (m *Manager) Neighbors() []network.Neighbor {
	neighbors := m.allNeighbors()
	result := make([]network.Neighbor, len(neighbors))
	for i, n := range neighbors {
		result[i] = n
	}

	return result
}

// allNeighbors returns all the neighbors that are currently connected.
func (m *Manager) allNeighbors() []*neighbor {
	return m.neighbors.Values()
}

// AutopeeringNeighbors returns all the neighbors that are currently connected via autopeering.
func (m *Manager) AutopeeringNeighbors() []network.Neighbor {
	return lo.Filter(m.Neighbors(), func(n network.Neighbor) bool {
		return !m.manualPeering.IsPeerKnown(n.Peer().ID)
	})
}

// neighborsByID returns all the neighbors that are currently connected corresponding to the supplied ids.
func (m *Manager) neighborsByID(ids []peer.ID) []*neighbor {
	result := make([]*neighbor, 0, len(ids))
	if len(ids) == 0 {
		return result
	}

	for _, id := range ids {
		if n, ok := m.neighbors.Get(id); ok {
			result = append(result, n)
		}
	}

	return result
}

func (m *Manager) handleStream(stream p2pnetwork.Stream) {
	m.protocolHandlerMutex.RLock()
	defer m.protocolHandlerMutex.RUnlock()

	if m.protocolHandler == nil {
		m.logger.LogError("no protocol handler registered")
		_ = stream.Close()

		return
	}

	if m.ctx == nil {
		m.logger.LogDebugf("aborting handling stream, context is nil")
		m.closeStream(stream)

		return
	}

	if m.ctx.Err() != nil {
		m.logger.LogDebugf("aborting handling stream, context is done")
		m.closeStream(stream)

		return
	}

	peerID := stream.Conn().RemotePeer()

	if !m.allowPeer(peerID) {
		m.logger.LogDebugf("peer %s is not allowed", peerID.String())
		m.closeStream(stream)

		return
	}

	ps := NewPacketsStream(stream, m.protocolHandler.PacketFactory)
	if err := ps.receiveNegotiation(); err != nil {
		m.logger.LogError("failed to receive negotiation message")
		m.closeStream(stream)

		return
	}

	peerAddrInfo := &peer.AddrInfo{
		ID:    peerID,
		Addrs: []multiaddr.Multiaddr{stream.Conn().RemoteMultiaddr()},
	}

	networkPeer := network.NewPeerFromAddrInfo(peerAddrInfo)
	if err := m.addNeighbor(networkPeer, ps, m.onBlockSentCallback); err != nil {
		m.logger.LogErrorf("failed to add neighbor, peerID: %s, error: %s", peerID.String(), err.Error())
		m.closeStream(stream)

		return
	}
}

func (m *Manager) closeStream(s p2pnetwork.Stream) {
	if err := s.Reset(); err != nil {
		m.logger.LogWarnf("close error, error: %s", err.Error())
	}
}

// Neighbor returns the neighbor with the given ID.
func (m *Manager) Neighbor(id peer.ID) (network.Neighbor, error) {
	return m.neighbor(id)
}

// neighbor returns the neighbor with the given ID.
func (m *Manager) neighbor(id peer.ID) (*neighbor, error) {
	nbr, ok := m.neighbors.Get(id)
	if !ok {
		return nil, network.ErrUnknownPeer
	}

	return nbr, nil
}

func (m *Manager) addNeighbor(peer *network.Peer, ps *PacketsStream, onBlockSentCallback func()) error {
	if peer.ID == m.libp2pHost.ID() {
		return ierrors.WithStack(network.ErrLoopbackPeer)
	}

	m.shutdownMutex.RLock()
	defer m.shutdownMutex.RUnlock()

	if m.isShutdown {
		return network.ErrNotRunning
	}

	if m.NeighborExists(peer.ID) {
		return ierrors.WithStack(network.ErrDuplicatePeer)
	}

	var innerErr error
	nbr := newNeighbor(m.logger,
		peer,
		ps,
		func(nbr *neighbor, packet proto.Message) {
			m.protocolHandlerMutex.RLock()
			defer m.protocolHandlerMutex.RUnlock()

			if m.protocolHandler == nil {
				nbr.logger.LogError("Can't handle packet as no protocol is registered")
				return
			}
			if err := m.protocolHandler.PacketHandler(nbr.Peer().ID, packet); err != nil {
				nbr.logger.LogDebugf("Can't handle packet, error: %s", err.Error())
			}
		},
		onBlockSentCallback,
		func(nbr *neighbor) {
			nbr.logger.LogInfof("Neighbor connected: %s", nbr.Peer().ID.String())
			nbr.Peer().SetConnStatus(network.ConnStatusConnected)
			m.neighborAdded.Trigger(nbr)
		}, func(nbr *neighbor) {
			m.deleteNeighbor(nbr)
			m.neighborRemoved.Trigger(nbr)
		})
	if err := m.setNeighbor(nbr); err != nil {
		if resetErr := ps.Reset(); resetErr != nil {
			nbr.logger.LogErrorf("error closing stream, error: %s", resetErr.Error())
		}

		return ierrors.WithStack(err)
	}
	nbr.readLoop()
	nbr.writeLoop()

	return innerErr
}

func (m *Manager) NeighborExists(id peer.ID) bool {
	return m.neighbors.Has(id)
}

func (m *Manager) deleteNeighbor(nbr *neighbor) {
	// Close the connection to the peer.
	_ = m.libp2pHost.Network().ClosePeer(nbr.Peer().ID)

	m.neighbors.Delete(nbr.Peer().ID)

	nbr.Peer().SetConnStatus(network.ConnStatusDisconnected)
}

func (m *Manager) setNeighbor(nbr *neighbor) error {
	var err error
	m.neighbors.Compute(nbr.Peer().ID, func(currentValue *neighbor, exists bool) *neighbor {
		if exists {
			err = ierrors.WithStack(network.ErrDuplicatePeer)
			return currentValue
		}

		return nbr
	})

	return err
}

func (m *Manager) dropAllNeighbors() {
	neighborsList := m.allNeighbors()
	for _, nbr := range neighborsList {
		nbr.Close()
	}
}

func (m *Manager) allowPeer(id peer.ID) (allow bool) {
	// Always allow manual peers
	if m.manualPeering.IsPeerKnown(id) {
		m.logger.LogDebugf("Allow manual peer %s", id.String())
		return true
	}

	// Only allow up to the maximum number of autopeered neighbors
	autopeeredNeighborsCount := len(m.AutopeeringNeighbors())
	if autopeeredNeighborsCount < m.autoPeering.MaxNeighbors() {
		m.logger.LogDebugf("Allow autopeered peer %s. Max %d has not been reached: %d", id.String(), m.autoPeering.MaxNeighbors(), autopeeredNeighborsCount)
		return true
	}

	// Don't allow new peers
	m.logger.LogDebugf("Disallow autopeered peer %s. Max %d has been reached", id.String(), m.autoPeering.MaxNeighbors())

	return false
}
