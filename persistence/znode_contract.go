package persistence

import (
	"log/slog"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"

	"github.com/machinefi/sprout/persistence/znode"
)

type ZNode struct {
	mux       sync.Mutex
	znodeDIDs map[string]bool

	contractAddress string
	chainEndpoint   string
}

func (z *ZNode) GetAll() []string {
	z.mux.Lock()
	defer z.mux.Unlock()

	dids := []string{}
	for d := range z.znodeDIDs {
		dids = append(dids, d)
	}
	return dids
}

// TODO monitor znode contract event
func NewZNode(chainEndpoint, contractAddress string) (*ZNode, error) {
	client, err := ethclient.Dial(chainEndpoint)
	if err != nil {
		return nil, errors.Wrapf(err, "dial chain endpoint failed, endpoint %s", chainEndpoint)
	}
	instance, err := znode.NewZnode(common.HexToAddress(contractAddress), client)
	if err != nil {
		return nil, errors.Wrapf(err, "new znode contract instance failed, endpoint %s, contractAddress %s", chainEndpoint, contractAddress)
	}

	znodeDIDs := map[string]bool{}

	for i := uint64(1); ; i++ {
		znode, err := instance.Znodes(nil, i)
		if err != nil {
			slog.Error("get znode from chain failed", "znode_id", i, "error", err)
			continue
		}
		if znode.Did == "" {
			break
		}
		znodeDIDs[znode.Did] = true
	}

	return &ZNode{
		znodeDIDs:       znodeDIDs,
		contractAddress: contractAddress,
		chainEndpoint:   chainEndpoint,
	}, nil
}
