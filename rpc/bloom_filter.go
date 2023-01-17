package rpc

import (
	"context"
	"fmt"
	"github.com/ankr/dogesyncer/types"
	"github.com/ethereum/go-ethereum/core/bloombits"
)

type Filter struct {
	fm      *FilterManager
	query   *LogQuery
	matcher *bloombits.Matcher
}

func (f *Filter) indexedLogs(ctx context.Context, end uint64) ([]*Log, error) {
	// Create a matcher session and request servicing from the backend
	matches := make(chan uint64, 64)
	session, err := f.matcher.Start(ctx, uint64(f.query.FromBlock), end, matches)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	f.fm.store.ServiceFilter(session)

	// Iterate over the matches until exhausted or context closed
	logs := make([]*Log, 0)

	for {
		select {
		case number, ok := <-matches:
			// Abort if all matches have been fulfilled
			if !ok {
				err := session.Error()
				if err == nil {
					f.query.FromBlock = f.query.ToBlock + 1
				}
				return logs, err
			}
			f.query.FromBlock = BlockNumber(number + 1)

			// Retrieve the suggested block and pull any truly matching logs
			header, found := f.fm.store.GetHeaderByNumber(number)
			if !found || header == nil {
				return logs, fmt.Errorf("block: %d, header not found", number)
			}
			log, err := f.blockLogs(header, true)
			if err != nil {
				return logs, err
			}
			logs = append(logs, log...)

		case <-ctx.Done():
			return logs, ctx.Err()
		}
	}
}

func (f *Filter) unIndexedLogs(end uint64) ([]*Log, error) {
	logs := make([]*Log, 0)

	fmt.Println("========unIndexedLogs=========", f.query.FromBlock, end)

	for ; uint64(f.query.FromBlock) <= end; f.query.FromBlock++ {
		header, found := f.fm.store.GetHeaderByNumber(uint64(f.query.FromBlock))
		if !found || header == nil {
			return logs, fmt.Errorf("block: %d, header not found", f.query.FromBlock)
		}
		log, err := f.blockLogs(header, false)
		if err != nil {
			return logs, err
		}
		logs = append(logs, log...)
	}
	return logs, nil
}

func (f *Filter) blockLogs(header *types.Header, skipBloom bool) ([]*Log, error) {
	// Fast track: no filtering criteria
	if (len(f.query.Addresses) == 0 && len(f.query.Topics) == 0) || (skipBloom || bloomFilter(header.LogsBloom, f.query.Addresses, f.query.Topics)) {
		block, ok := f.fm.store.GetBlockByHash(header.Hash, true)
		if !ok {
			return nil, ErrBlockNotFound
		}

		if len(block.Transactions) == 0 {
			// no txs in block, return empty response
			return nil, nil
		}

		return f.fm.getLogsFromBlock(f.query, block)

	}
	return nil, nil
}

func (f *Filter) Logs(ctx context.Context) ([]*Log, error) {
	// If we're doing singleton block filtering, execute and return
	if f.query.BlockHash != nil && *f.query.BlockHash != (types.Hash{}) {
		header, found := f.fm.store.GetHeaderByHash(*f.query.BlockHash)
		if !found || header == nil {
			return nil, fmt.Errorf("can not find block %s", *f.query.BlockHash)
		}
		return f.blockLogs(header, false)
	}

	// Gather all indexed logs, and finish with non indexed ones
	var (
		logs           = make([]*Log, 0)
		err            error
		end            = uint64(f.query.ToBlock)
		size, sections = f.fm.store.BloomStatus()
	)
	if indexed := sections * size; indexed > uint64(f.query.FromBlock) {
		if indexed > uint64(f.query.ToBlock) {
			logs, err = f.indexedLogs(ctx, end)
		} else {
			logs, err = f.indexedLogs(ctx, indexed-1)
		}
		if err != nil {
			return logs, err
		}
	}
	rest, err := f.unIndexedLogs(end)
	logs = append(logs, rest...)
	return logs, err
}

func bloomFilter(bloom types.Bloom, addresses []types.Address, topics [][]types.Hash) bool {
	if len(addresses) > 0 {
		var included bool
		for _, addr := range addresses {
			if types.BloomLookup(bloom, addr) {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}

	for _, sub := range topics {
		included := len(sub) == 0 // empty rule set == wildcard
		for _, topic := range sub {
			if types.BloomLookup(bloom, topic) {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}
	return true
}
