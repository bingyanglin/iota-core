package p2p

import (
	"context"
	"path/filepath"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	p2pbhost "github.com/libp2p/go-libp2p/p2p/host/basic"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
	mamask "github.com/whyrusleeping/multiaddr-filter"
	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/app/configuration"
	hivep2p "github.com/iotaledger/hive.go/crypto/p2p"
	"github.com/iotaledger/hive.go/db"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/daemon"
	"github.com/iotaledger/iota-core/pkg/network"
	"github.com/iotaledger/iota-core/pkg/network/p2p"
	"github.com/iotaledger/iota-core/pkg/protocol"
)

func init() {
	Component = &app.Component{
		Name:             "P2P",
		DepsFunc:         func(cDeps dependencies) { deps = cDeps },
		Params:           params,
		InitConfigParams: initConfigParams,
		Provide:          provide,
		Configure:        configure,
		Run:              run,
	}
}

var (
	Component *app.Component
	deps      dependencies
)

type dependencies struct {
	dig.In
	PeeringConfig        *configuration.Configuration `name:"peeringConfig"`
	PeeringConfigManager *p2p.ConfigManager
	NetworkManager       network.Manager
	PeerDB               *network.DB
	Protocol             *protocol.Protocol
	PeerDBKVSTore        kvstore.KVStore `name:"peerDBKVStore"`
}

func initConfigParams(c *dig.Container) error {
	type cfgResult struct {
		dig.Out
		P2PDatabasePath       string   `name:"p2pDatabasePath"`
		P2PBindMultiAddresses []string `name:"p2pBindMultiAddresses"`
	}

	if err := c.Provide(func() cfgResult {
		return cfgResult{
			P2PDatabasePath:       ParamsP2P.Database.Path,
			P2PBindMultiAddresses: ParamsP2P.BindMultiAddresses,
		}
	}); err != nil {
		Component.LogPanic(err.Error())
	}

	return nil
}

func provide(c *dig.Container) error {
	type peerDatabaseResult struct {
		dig.Out

		PeerDB        *network.DB
		PeerDBKVSTore kvstore.KVStore `name:"peerDBKVStore"`
	}

	if err := c.Provide(func() peerDatabaseResult {
		peerDB, peerDBKVStore, err := initPeerDB()
		if err != nil {
			Component.LogFatal(err.Error())
		}

		return peerDatabaseResult{
			PeerDB:        peerDB,
			PeerDBKVSTore: peerDBKVStore,
		}
	}); err != nil {
		return err
	}

	type configManagerDeps struct {
		dig.In
		PeeringConfig         *configuration.Configuration `name:"peeringConfig"`
		PeeringConfigFilePath *string                      `name:"peeringConfigFilePath"`
	}

	if err := c.Provide(func(deps configManagerDeps) *p2p.ConfigManager {
		p2pConfigManager := p2p.NewConfigManager(func(peers []*p2p.PeerConfigItem) error {
			if err := deps.PeeringConfig.Set(CfgPeers, peers); err != nil {
				return err
			}

			return deps.PeeringConfig.StoreFile(*deps.PeeringConfigFilePath, 0o600, []string{"p2p"})
		})

		// peers from peering config
		var peers []*p2p.PeerConfig
		if err := deps.PeeringConfig.Unmarshal(CfgPeers, &peers); err != nil {
			Component.LogPanicf("invalid peer config: %s", err)
		}

		for i, p := range peers {
			multiAddr, err := multiaddr.NewMultiaddr(p.MultiAddress)
			if err != nil {
				Component.LogPanicf("invalid config peer address at pos %d: %s", i, err)
			}

			if err = p2pConfigManager.AddPeer(multiAddr, p.Alias); err != nil {
				Component.LogWarnf("unable to add peer to config manager %s: %s", p.MultiAddress, err)
			}
		}

		// peers from CLI arguments
		applyAliases := true
		if len(ParamsPeers.Peers) != len(ParamsPeers.PeerAliases) {
			Component.LogWarnf("won't apply peer aliases: you must define aliases for all defined static peers (got %d aliases, %d peers).", len(ParamsPeers.PeerAliases), len(ParamsPeers.Peers))
			applyAliases = false
		}

		peersMultiAddresses, err := getMultiAddrsFromString(ParamsPeers.Peers)
		if err != nil {
			Component.LogFatal(err.Error())
		}

		peerAdded := false
		for i, multiAddr := range peersMultiAddresses {
			var alias string
			if applyAliases {
				alias = ParamsPeers.PeerAliases[i]
			}

			if err = p2pConfigManager.AddPeer(multiAddr, alias); err != nil {
				Component.LogWarnf("unable to add peer to config manager %s: %s", multiAddr.String(), err)
			}

			peerAdded = true
		}

		p2pConfigManager.StoreOnChange(true)

		if peerAdded {
			if err := p2pConfigManager.Store(); err != nil {
				Component.LogWarnf("failed to store peering config: %s", err)
			}
		}

		return p2pConfigManager
	}); err != nil {
		Component.LogPanic(err.Error())
	}

	type p2pDeps struct {
		dig.In
		DatabaseEngine        db.Engine `name:"databaseEngine"`
		P2PDatabasePath       string    `name:"p2pDatabasePath"`
		P2PBindMultiAddresses []string  `name:"p2pBindMultiAddresses"`
	}

	type p2pResult struct {
		dig.Out
		NodePrivateKey crypto.PrivKey `name:"nodePrivateKey"`
		Host           host.Host
	}

	if err := c.Provide(func(deps p2pDeps) p2pResult {
		res := p2pResult{}

		privKeyFilePath := filepath.Join(deps.P2PDatabasePath, IdentityPrivateKeyFileName)

		// make sure nobody copies around the peer store since it contains the private key of the node
		Component.LogInfof(`WARNING: never share your "%s" folder as it contains your node's private key!`, deps.P2PDatabasePath)

		// load up the previously generated identity or create a new one
		nodePrivateKey, newlyCreated, err := hivep2p.LoadOrCreateIdentityPrivateKey(privKeyFilePath, ParamsP2P.IdentityPrivateKey)
		if err != nil {
			Component.LogPanic(err.Error())
		}
		res.NodePrivateKey = nodePrivateKey

		if newlyCreated {
			Component.LogInfof(`stored new private key for peer identity under "%s"`, privKeyFilePath)
		} else {
			Component.LogInfof(`loaded existing private key for peer identity from "%s"`, privKeyFilePath)
		}

		connManager, err := connmgr.NewConnManager(
			ParamsP2P.ConnectionManager.LowWatermark,
			ParamsP2P.ConnectionManager.HighWatermark,
			connmgr.WithEmergencyTrim(true),
		)
		if err != nil {
			Component.LogPanicf("unable to initialize connection manager: %s", err)
		}

		createdHost, err := libp2p.New(
			libp2p.ListenAddrStrings(ParamsP2P.BindMultiAddresses...),
			libp2p.Identity(nodePrivateKey),
			libp2p.Transport(tcp.NewTCPTransport),
			libp2p.ConnectionManager(connManager),
			libp2p.NATPortMap(),
			libp2p.DisableRelay(),
			// Define a custom address factory to inject external addresses to the DHT advertisements.
			libp2p.AddrsFactory(publicOnlyAddresses(ParamsP2P.Autopeering.ExternalMultiAddresses)),
		)
		if err != nil {
			Component.LogFatalf("unable to initialize libp2p host: %s", err)
		}
		res.Host = createdHost

		Component.LogInfof("Initialized P2P host %s %s", createdHost.ID().String(), createdHost.Addrs())

		return res
	}); err != nil {
		Component.LogPanic(err.Error())
	}

	if err := c.Provide(func() *p2p.Metrics {
		return &p2p.Metrics{}
	}); err != nil {
		Component.LogPanic(err.Error())
	}

	type p2pManagerDeps struct {
		dig.In
		Host       host.Host
		PeerDB     *network.DB
		P2PMetrics *p2p.Metrics
	}

	return c.Provide(func(inDeps p2pManagerDeps) network.Manager {
		peersMultiAddresses, err := getMultiAddrsFromString(ParamsPeers.BootstrapPeers)
		if err != nil {
			Component.LogFatalf("Failed to parse bootstrapPeers param: %s", err)
		}

		for _, multiAddr := range peersMultiAddresses {
			bootstrapPeer, err := network.NewPeerFromMultiAddr(multiAddr)
			if err != nil {
				Component.LogFatalf("Failed to parse bootstrap peer multiaddress: %s", err)
			}

			if err := inDeps.PeerDB.UpdatePeer(bootstrapPeer); err != nil {
				Component.LogErrorf("Failed to update bootstrap peer: %s", err)
			}
		}

		onBlockSentCallback := func() {
			inDeps.P2PMetrics.OutgoingBlocks.Add(1)
		}

		return p2p.NewManager(Component.Logger, inDeps.Host, inDeps.PeerDB, ParamsP2P.Autopeering.MaxPeers, onBlockSentCallback)
	})
}

func configure() error {
	if err := Component.Daemon().BackgroundWorker("Close p2p peer database", func(ctx context.Context) {
		<-ctx.Done()

		closeDatabases := func() error {
			if err := deps.PeerDBKVSTore.Flush(); err != nil {
				return err
			}

			return deps.PeerDBKVSTore.Close()
		}

		Component.LogInfo("Syncing p2p peer database to disk ...")
		if err := closeDatabases(); err != nil {
			Component.LogPanicf("Syncing p2p peer database to disk ... failed: %s", err)
		}
		Component.LogInfo("Syncing p2p peer database to disk ... done")
	}, daemon.PriorityCloseDatabase); err != nil {
		Component.LogPanicf("failed to start worker: %s", err)
	}

	// log the p2p events
	deps.NetworkManager.OnNeighborAdded(func(neighbor network.Neighbor) {
		Component.LogInfof("neighbor added: %s / %s", neighbor.Peer().PeerAddresses, neighbor.Peer().ID)
	})

	deps.NetworkManager.OnNeighborRemoved(func(neighbor network.Neighbor) {
		Component.LogInfof("neighbor removed: %s / %s", neighbor.Peer().PeerAddresses, neighbor.Peer().ID)
	})

	return nil
}

func run() error {
	if err := Component.Daemon().BackgroundWorker(Component.Name, func(ctx context.Context) {
		defer deps.NetworkManager.Shutdown()

		if err := deps.NetworkManager.Start(ctx, deps.Protocol.LatestAPI().ProtocolParameters().NetworkName()); err != nil {
			Component.LogFatalf("Failed to start p2p manager: %s", err)
		}

		//nolint:contextcheck // false positive
		connectConfigKnownPeers()

		<-ctx.Done()
	}, daemon.PriorityP2P); err != nil {
		Component.LogFatalf("Failed to start as daemon: %s", err)
	}

	return nil
}

func getMultiAddrsFromString(peers []string) ([]multiaddr.Multiaddr, error) {
	peersMultiAddresses := make([]multiaddr.Multiaddr, 0, len(peers))

	for _, peer := range peers {
		peerMultiAddr, err := multiaddr.NewMultiaddr(peer)
		if err != nil {
			return nil, ierrors.Wrapf(err, "invalid peer multiaddr \"%s\"", peer)
		}
		peersMultiAddresses = append(peersMultiAddresses, peerMultiAddr)
	}

	return peersMultiAddresses, nil
}

// connects to the peers defined in the config.
func connectConfigKnownPeers() {
	for _, p := range deps.PeeringConfigManager.Peers() {
		multiAddr, err := multiaddr.NewMultiaddr(p.MultiAddress)
		if err != nil {
			Component.LogPanicf("invalid peer address: %s", err)
		}

		// we try to parse the multi address and check if there is a "/p2p" part with ID
		_, err = peer.AddrInfoFromP2pAddr(multiAddr)
		if err != nil {
			Component.LogPanicf("invalid peer address info: %s", err)
		}

		if _, err := deps.NetworkManager.AddManualPeer(multiAddr); err != nil {
			Component.LogInfof("failed to add peer: %s, error: %s", multiAddr.String(), err)
		}
	}
}

// Based on https://github.com/ipfs/kubo/blob/master/config/profile.go
// defaultServerFilters has is a list of IPv4 and IPv6 prefixes that are private, local only, or unrouteable.
// according to https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xhtml
// and https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry.xhtml
var reservedFilters = []string{
	"/ip4/0.0.0.0/ipcidr/32",
	"/ip4/10.0.0.0/ipcidr/8",
	"/ip4/100.64.0.0/ipcidr/10",
	"/ip4/127.0.0.0/ipcidr/8",
	"/ip4/169.254.0.0/ipcidr/16",
	"/ip4/172.16.0.0/ipcidr/12",
	"/ip4/192.0.0.0/ipcidr/24",
	"/ip4/192.0.2.0/ipcidr/24",
	"/ip4/192.168.0.0/ipcidr/16",
	"/ip4/192.31.196.0/ipcidr/24",
	"/ip4/192.52.193.0/ipcidr/24",
	"/ip4/198.18.0.0/ipcidr/15",
	"/ip4/198.51.100.0/ipcidr/24",
	"/ip4/203.0.113.0/ipcidr/24",
	"/ip4/240.0.0.0/ipcidr/4",

	"/ip6/::1/ipcidr/64",
	"/ip6/100::/ipcidr/64",
	"/ip6/2001:2::/ipcidr/48",
	"/ip6/2001:db8::/ipcidr/32",
	"/ip6/fc00::/ipcidr/7",
	"/ip6/fe80::/ipcidr/10",
}

func publicOnlyAddresses(additionalMultiaddresses []string) p2pbhost.AddrsFactory {
	var externalMultiAddrs []multiaddr.Multiaddr

	// Add the external multi addresses to the list of addresses to be announced.
	if len(additionalMultiaddresses) > 0 {
		for _, externalMultiAddress := range additionalMultiaddresses {
			addr, err := multiaddr.NewMultiaddr(externalMultiAddress)
			if err != nil {
				Component.LogPanicf("unable to parse external multi address %s: %s", externalMultiAddress, err)
			}

			externalMultiAddrs = append(externalMultiAddrs, addr)
		}
	}

	// Create a filter that blocks localhost and reserved addresses.
	filters := multiaddr.NewFilters()
	for _, addr := range reservedFilters {
		f, err := mamask.NewMask(addr)
		if err != nil {
			Component.LogPanicf("unable to parse ip mask filter %s: %s", addr, err)
		}
		filters.AddFilter(*f, multiaddr.ActionDeny)
	}

	return func(addresses []multiaddr.Multiaddr) []multiaddr.Multiaddr {
		filteredAddresses := lo.Filter(append(addresses, externalMultiAddrs...), func(m multiaddr.Multiaddr) bool {
			blocked := filters.AddrBlocked(m)
			if blocked {
				Component.LogTracef("Filtered out address %s", m)
			}
			return !blocked
		})

		Component.LogTracef("Announcing addresses: %v", filteredAddresses)

		return filteredAddresses
	}
}
