package manualpeering

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/iotaledger/hive.go/autopeering/peer"
	"github.com/iotaledger/hive.go/crypto/ed25519"
	"github.com/iotaledger/hive.go/crypto/identity"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/logger"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/iota-core/pkg/network"
	"github.com/iotaledger/iota-core/pkg/network/p2p"
)

// Manager is the core entity in the manual peering package.
// It holds a list of known peers and constantly provisions it to the gossip layer.
// Its job is to keep in sync the list of known peers
// and the list of current manual neighbors connected in the gossip layer.
// If a new peer is added to known peers, manager will forward it to gossip and make sure it establishes a connection.
// And vice versa, if a peer is being removed from the list of known peers,
// manager will make sure gossip drops that connection.
// Manager also subscribes to the gossip events and in case the connection with a manual peer fails it will reconnect.
type Manager struct {
	p2pm              *p2p.Manager
	log               *logger.Logger
	local             *peer.Local
	startOnce         sync.Once
	isStarted         atomic.Bool
	stopOnce          sync.Once
	stopMutex         syncutils.RWMutex
	isStopped         bool
	reconnectInterval time.Duration
	knownPeersMutex   syncutils.RWMutex
	knownPeers        map[network.PeerID]*network.Peer
	workerPool        *workerpool.WorkerPool

	onGossipNeighborRemovedHook *event.Hook[func(*p2p.Neighbor)]
	onGossipNeighborAddedHook   *event.Hook[func(*p2p.Neighbor)]
}

// NewManager initializes a new Manager instance.
func NewManager(p2pm *p2p.Manager, local *peer.Local, workerPool *workerpool.WorkerPool, log *logger.Logger) *Manager {
	m := &Manager{
		p2pm:              p2pm,
		local:             local,
		log:               log,
		reconnectInterval: network.DefaultReconnectInterval,
		knownPeers:        map[network.PeerID]*network.Peer{},
		workerPool:        workerPool,
	}

	return m
}

// AddPeers adds multiple peers to the list of known peers.
func (m *Manager) AddPeers(peers ...ma.Multiaddr) error {
	var resultErr error
	for _, peer := range peers {
		if err := m.addPeer(peer); err != nil {
			resultErr = err
		}
	}

	return resultErr
}

// RemovePeer removes multiple peers from the list of known peers.
func (m *Manager) RemovePeer(keys ...ed25519.PublicKey) error {
	var resultErr error
	for _, key := range keys {
		if err := m.removePeer(key); err != nil {
			resultErr = err
		}
	}

	return resultErr
}

// GetPeersConfig holds optional parameters for the GetPeers method.
type GetPeersConfig struct {
	// If true, GetPeers returns peers that have actual connection established in the gossip layer.
	OnlyConnected bool `json:"onlyConnected"`
}

// GetPeersOption defines a single option for GetPeers method.
type GetPeersOption func(conf *GetPeersConfig)

// BuildGetPeersConfig builds GetPeersConfig struct from a list of options.
func BuildGetPeersConfig(opts []GetPeersOption) *GetPeersConfig {
	conf := &GetPeersConfig{}
	for _, o := range opts {
		o(conf)
	}

	return conf
}

// ToOptions translates config struct to a list of corresponding options.
func (c *GetPeersConfig) ToOptions() (opts []GetPeersOption) {
	if c.OnlyConnected {
		opts = append(opts, WithOnlyConnectedPeers())
	}

	return opts
}

// WithOnlyConnectedPeers returns a GetPeersOption that sets OnlyConnected field to true.
func WithOnlyConnectedPeers() GetPeersOption {
	return func(conf *GetPeersConfig) {
		conf.OnlyConnected = true
	}
}

// GetPeers returns the list of known peers.
func (m *Manager) GetPeers(opts ...GetPeersOption) []*network.KnownPeer {
	conf := BuildGetPeersConfig(opts)
	m.knownPeersMutex.RLock()
	defer m.knownPeersMutex.RUnlock()

	peers := make([]*network.KnownPeer, 0, len(m.knownPeers))
	for _, kp := range m.knownPeers {
		connStatus := kp.GetConnStatus()
		if !conf.OnlyConnected || connStatus == network.ConnStatusConnected {
			peers = append(peers, &network.KnownPeer{
				PublicKey:  kp.Identity.PublicKey(),
				Addresses:  kp.PeerAddresses,
				ConnStatus: connStatus,
			})
		}
	}

	return peers
}

// Start subscribes to the gossip layer events and starts internal background workers.
// Calling multiple times has no effect.
func (m *Manager) Start() {
	m.startOnce.Do(func() {
		m.workerPool.Start()
		m.onGossipNeighborRemovedHook = m.p2pm.Events.NeighborRemoved.Hook(func(neighbor *p2p.Neighbor) {
			m.onGossipNeighborRemoved(neighbor)
		}, event.WithWorkerPool(m.workerPool))
		m.onGossipNeighborAddedHook = m.p2pm.Events.NeighborAdded.Hook(func(neighbor *p2p.Neighbor) {
			m.onGossipNeighborAdded(neighbor)
		}, event.WithWorkerPool(m.workerPool))
		m.isStarted.Store(true)
	})
}

// Stop terminates internal background workers. Calling multiple times has no effect.
func (m *Manager) Stop() (err error) {
	if !m.isStarted.Load() {
		return ierrors.New("can't stop the manager: it hasn't been started yet")
	}
	m.stopOnce.Do(func() {
		m.stopMutex.Lock()
		defer m.stopMutex.Unlock()

		m.isStopped = true
		err = ierrors.WithStack(m.removeAllKnownPeers())
		m.onGossipNeighborRemovedHook.Unhook()
		m.onGossipNeighborAddedHook.Unhook()
	})

	return err
}

func (m *Manager) addPeer(peerAddr ma.Multiaddr) error {
	if !m.isStarted.Load() {
		return ierrors.New("manual peering manager hasn't been started yet")
	}

	if m.isStopped {
		return ierrors.New("manual peering manager was stopped")
	}

	m.knownPeersMutex.Lock()
	defer m.knownPeersMutex.Unlock()

	p, err := network.NewPeer(peerAddr)
	if err != nil {
		return ierrors.WithStack(err)
	}
	if _, exists := m.knownPeers[p.Identity.ID()]; exists {
		return nil
	}
	m.log.Infow("Adding new peer to the list of known peers in manual peering", "peer", p)
	m.knownPeers[p.Identity.ID()] = p
	go func() {
		defer close(p.DoneCh)
		m.keepPeerConnected(p)
	}()

	return nil
}

func (m *Manager) removePeer(key ed25519.PublicKey) error {
	m.knownPeersMutex.Lock()
	defer m.knownPeersMutex.Unlock()

	m.log.Infow("Removing peer from from the list of known peers in manual peering",
		"publicKey", key)
	peerID := identity.NewID(key)
	err := m.removePeerByID(peerID)

	return ierrors.WithStack(err)
}

func (m *Manager) removeAllKnownPeers() error {
	m.knownPeersMutex.Lock()
	defer m.knownPeersMutex.Unlock()

	var resultErr error
	for peerID := range m.knownPeers {
		if err := m.removePeerByID(peerID); err != nil {
			resultErr = err
		}
	}

	return resultErr
}

func (m *Manager) removePeerByID(peerID network.PeerID) error {
	kp, exists := m.knownPeers[peerID]
	if !exists {
		return nil
	}
	delete(m.knownPeers, peerID)
	close(kp.RemoveCh)
	<-kp.DoneCh
	if err := m.p2pm.DropNeighbor(peerID); err != nil && !ierrors.Is(err, p2p.ErrUnknownNeighbor) {
		return ierrors.Wrapf(err, "failed to drop known peer %s in the gossip layer", peerID)
	}

	return nil
}

func (m *Manager) keepPeerConnected(p *network.Peer) {
	ctx, ctxCancel := context.WithCancel(context.Background())
	cancelContextOnRemove := func() {
		<-p.RemoveCh
		ctxCancel()
	}
	go cancelContextOnRemove()

	ticker := time.NewTicker(m.reconnectInterval)
	defer ticker.Stop()

	peerID := p.Identity.ID()
	for {
		if p.GetConnStatus() == network.ConnStatusDisconnected {
			m.log.Infow("Peer is disconnected, calling gossip layer to establish the connection", "peer", p.Identity.ID())

			var err error
			if err = m.p2pm.DialPeer(ctx, p); err != nil && !ierrors.Is(err, p2p.ErrDuplicateNeighbor) && !ierrors.Is(err, context.Canceled) {
				m.log.Errorw("Failed to connect a neighbor in the gossip layer", "peerID", peerID, "err", err)
			}
		}
		select {
		case <-ticker.C:
		case <-p.RemoveCh:
			<-ctx.Done()
			return
		}
	}
}

func (m *Manager) onGossipNeighborRemoved(neighbor *p2p.Neighbor) {
	m.changeNeighborStatus(neighbor, network.ConnStatusDisconnected)
}

func (m *Manager) onGossipNeighborAdded(neighbor *p2p.Neighbor) {
	m.changeNeighborStatus(neighbor, network.ConnStatusConnected)
	m.log.Infow(
		"Gossip layer successfully connected with the peer",
		"peer", neighbor.Peer,
	)
}

func (m *Manager) changeNeighborStatus(neighbor *p2p.Neighbor, connStatus network.ConnectionStatus) {
	m.knownPeersMutex.RLock()
	defer m.knownPeersMutex.RUnlock()

	kp, exists := m.knownPeers[neighbor.Identity.ID()]
	if !exists {
		return
	}
	kp.SetConnStatus(connStatus)
}
