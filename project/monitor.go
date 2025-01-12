package project

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/event"
)

func NewMonitor(chainEndpoint string, addresses []string, topics []string, from, step int64, interval time.Duration) (*Monitor, error) {
	client, err := ethclient.Dial(chainEndpoint)
	if err != nil {
		return nil, err
	}

	m := &Monitor{
		client:   client,
		latest:   from,
		step:     step,
		interval: interval,
		events:   nil,
		err:      make(chan error),
		stop:     make(chan struct{}),
	}
	m.addresses = make([]common.Address, 0, len(addresses))
	for _, addr := range addresses {
		m.addresses = append(m.addresses, common.HexToAddress(addr))
	}

	m.topics = make([][]common.Hash, 1)
	m.events = make(map[common.Hash]chan *types.Log)
	for i := range topics {
		topic := crypto.Keccak256Hash([]byte(topics[i]))
		m.topics[0] = append(m.topics[0], topic)
		m.events[topic] = make(chan *types.Log, 100)
	}

	return m, nil
}

func NewDefaultMonitor(chainEndpoint string, addresses []string, topics []string) (*Monitor, error) {
	client, err := ethclient.Dial(chainEndpoint)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	latestBlk, err := client.BlockNumber(context.Background())
	if err != nil {
		slog.Error("query latest block number", "msg", err)
		return nil, err
	}

	return NewMonitor(chainEndpoint, addresses, topics, int64(latestBlk), 100000, time.Second*10)
}

type Monitor struct {
	// client chain client
	client *ethclient.Client
	// latest queried block number
	latest int64
	// step query step, before chased the latest block, query [current, current+step]
	step int64
	// interval query/monitor interval
	interval time.Duration
	// addresses contract addresses
	addresses []common.Address
	// topics care about
	topics [][]common.Hash
	// events channel
	events map[common.Hash]chan *types.Log
	// err failed signal
	err chan error
	// stop signal
	stop chan struct{}
}

var _ event.Subscription = (*Monitor)(nil)

func (m *Monitor) Err() <-chan error {
	return m.err
}

func (m *Monitor) Unsubscribe() {
	m.stop <- struct{}{}
}

func (m *Monitor) Events(topic string) (<-chan *types.Log, bool) {
	ch, ok := m.events[crypto.Keccak256Hash([]byte(topic))]
	return ch, ok
}

func (m *Monitor) MustEvents(topic string) <-chan *types.Log {
	ch, ok := m.Events(topic)
	if !ok {
		panic("event not subscribed " + topic)
	}
	return ch
}

func (m *Monitor) run() {
	query := ethereum.FilterQuery{
		Addresses: m.addresses,
		Topics:    m.topics,
	}
	for {
		select {
		case <-m.stop:
			slog.Info("monitor stopped")
		default:
			latestBlk, err := m.client.BlockNumber(context.Background())
			if err != nil {
				slog.Error("query latest block number", "msg", err)
				time.Sleep(m.interval)
				continue
			}
			slog.Debug("query latest block", "block number", latestBlk)
			if uint64(m.latest) > latestBlk {
				time.Sleep(m.interval)
				continue
			}
			query.FromBlock = big.NewInt(m.latest)
			query.ToBlock = big.NewInt(min(m.latest+m.step, int64(latestBlk)))

			logs, err := m.client.FilterLogs(context.Background(), query)
			if err != nil {
				slog.Error("failed to filter logs", "msg", err)
				time.Sleep(m.interval)
				continue
			}
			slog.Debug("filter logs", "from", query.FromBlock.Uint64(), "to", query.ToBlock.Uint64())
			m.latest = query.ToBlock.Int64()
			if len(logs) == 0 {
				goto TryLater
			}
			slog.Info("filter logs", "count", len(logs))
			for _, l := range logs {
				topic := l.Topics[0]
				if _, ok := m.events[topic]; !ok {
					continue
				}
				m.events[topic] <- &l
			}
		TryLater:
			if query.ToBlock.Int64()-query.FromBlock.Int64() < m.step {
				time.Sleep(m.interval)
			}
		}
	}
}
