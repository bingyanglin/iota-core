package management

import (
	"github.com/labstack/echo/v4"

	"github.com/iotaledger/hive.go/log"
	"github.com/iotaledger/iota.go/v4/api"
)

/*
// WrapInfoSnapshot wraps the given peer info snapshot with additional metadata, such as gossip protocol information.
func WrapInfoSnapshot(info *p2p.PeerInfoSnapshot) *api.PeerResponse {
	var alias *string

	if info.Alias != "" {
		alias = &info.Alias
	}

	multiAddresses := make([]string, len(info.Addresses))
	for i, multiAddress := range info.Addresses {
		multiAddresses[i] = multiAddress.String()
	}

	gossipProto := deps.GossipService.Protocol(info.Peer.ID)
	var gossipInfo *gossip.Info
	if gossipProto != nil {
		gossipInfo = gossipProto.Info()
	}

	return &PeerResponse{
		ID:             info.ID,
		MultiAddresses: multiAddresses,
		Alias:          alias,
		Relation:       info.Relation,
		Connected:      info.Connected,
		Gossip:         gossipInfo,
	}
	return nil
}
*/

func getPeer(_ echo.Context) (*api.PeerInfo, error) {
	/*
		peerID, err := restapipkg.ParsePeerIDParam(c)
		if err != nil {
			return nil, err
		}

		info := deps.PeeringManager.PeerInfoSnapshot(peerID)
		if info == nil {
			return nil, errors.WithMessagef(echo.ErrNotFound, "peer not found, peerID: %s", peerID.String())
		}

		return WrapInfoSnapshot(info), nil
	*/

	//nolint:revive,nilnil
	return nil, nil
}

func removePeer(_ echo.Context) error {
	/*
		peerID, err := restapipkg.ParsePeerIDParam(c)
		if err != nil {
			return err
		}

		// error is ignored because we don't care about the config here
		_ = deps.PeeringConfigManager.RemovePeer(peerID)

		return deps.PeeringManager.DisconnectPeer(peerID, errors.New("peer was removed via API"))
	*/
	return nil
}

//nolint:unparam // even if the error is never used, the structure of all routes should be the same
func listPeers(_ echo.Context) (*api.PeersResponse, error) {
	/*
		peerInfos := deps.PeeringManager.PeerInfoSnapshots()
		results := make([]*PeerResponse, len(peerInfos))
		for i, info := range peerInfos {
			results[i] = WrapInfoSnapshot(info)
		}

		return results, nil
	*/

	//nolint:revive,nilnil
	return nil, nil
}

func addPeer(_ echo.Context, _ log.Logger) (*api.PeerInfo, error) {
	/*

		request := &addPeerRequest{}

		if err := c.Bind(request); err != nil {
			return nil, errors.WithMessagef(httpserver.ErrInvalidParameter, "invalid addPeerRequest, error: %s", err)
		}

		multiAddr, err := multiaddr.NewMultiaddr(request.MultiAddress)
		if err != nil {
			return nil, errors.WithMessagef(httpserver.ErrInvalidParameter, "invalid multiAddress, error: %s", err)
		}

		addrInfo, err := peer.AddrInfoFromP2pAddr(multiAddr)
		if err != nil {
			return nil, errors.WithMessagef(httpserver.ErrInvalidParameter, "invalid multiAddress, error: %s", err)
		}

		var alias string
		if request.Alias != nil {
			alias = *request.Alias
		}

		// error is ignored because the peer is added to the known peers and protected from trimming
		_ = deps.PeeringManager.ConnectPeer(addrInfo, p2p.PeerRelationKnown, alias)

		info := deps.PeeringManager.PeerInfoSnapshot(addrInfo.ID)
		if info == nil {
			return nil, errors.WithMessagef(echo.ErrNotFound, "peer not found, peerID: %s", addrInfo.ID.String())
		}

		// error is ignored because we don't care about the config here
		if err := deps.PeeringConfigManager.AddPeer(multiAddr, alias); err != nil {
			logger.Warn(err.Error())
		}

		return WrapInfoSnapshot(info), nil
	*/

	//nolint:revive,nilnil
	return nil, nil
}
