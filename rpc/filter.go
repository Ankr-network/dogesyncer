package rpc

import (
	"container/heap"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankr/dogesyncer/blockchain"
	"github.com/ankr/dogesyncer/types"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/go-hclog"
	"strings"
	"sync"
	"time"
)

var (
	ErrFilterDoesNotExists              = errors.New("filter does not exists")
	ErrWSFilterDoesNotSupportGetChanges = errors.New("web socket Filter doesn't support to return a batch of the changes")
	ErrCastingFilterToLogFilter         = errors.New("casting filter object to logFilter error")
	ErrBlockNotFound                    = errors.New("block not found")
	ErrIncorrectBlockRange              = errors.New("incorrect range")
	ErrBlockRangeTooHigh                = errors.New("block range too high")
	ErrPendingBlockNumber               = errors.New("pending block number is not supported")
	ErrNoWSConnection                   = errors.New("no websocket connection")
)

// defaultTimeout is the timeout to remove the filters that don't have a web socket stream
var defaultTimeout = 1 * time.Minute

const (
	// The index in heap which is indicating the element is not in the heap
	NoIndexInHeap = -1
	// _checkDuration is for filter timeout check
	_checkDuration = time.Second
)

type wsConn interface {
	WriteMessage(messageType int, data []byte) error
	GetFilterID() string
	SetFilterID(string)
}

// filter is an interface that BlockFilter and LogFilter implement
type filter interface {
	// hasWSConn returns the flag indicating the filter has web socket stream
	hasWSConn() bool

	// getFilterBase returns filterBase that has common fields
	getFilterBase() *filterBase

	// getUpdates returns stored data in string
	getUpdates() (string, error)

	// sendUpdates write stored data to web socket stream
	sendUpdates() error
}

// filterBase is a struct for common fields between BlockFilter and LogFilter
type filterBase struct {
	// UUID, a key of filter for client
	id string

	// index in the timeouts heap, -1 for non-existing index
	heapIndex int

	// timestamp to be expired
	expiresAt time.Time

	// websocket connection
	// todo ws wsConn
}

// newFilterBase initializes filterBase with unique ID
func newFilterBase( /*ws wsConn*/ ) filterBase {
	// todo
	/*return filterBase{
		id:        uuid.New().String(),
		ws:        ws,
		heapIndex: NoIndexInHeap,
	}*/
	return filterBase{}
}

// getFilterBase returns its own reference so that child struct can return base
func (f *filterBase) getFilterBase() *filterBase {
	return f
}

// hasWSConn returns the flag indicating this filter has websocket connection
func (f *filterBase) hasWSConn() bool {
	// todo return f.ws != nil
	return false
}

const ethSubscriptionTemplate = `{
	"jsonrpc": "2.0",
	"method": "eth_subscription",
	"params": {
		"subscription":"%s",
		"result": %s
	}
}`

// writeMessageToWs sends given message to websocket stream
func (f *filterBase) writeMessageToWs(msg string) error {
	//todo
	/*if !f.hasWSConn() {
		return ErrNoWSConnection
	}

	var v bytes.Buffer
	if _, err := v.WriteString(fmt.Sprintf(ethSubscriptionTemplate, f.id, msg)); err != nil {
		return err
	}

	return f.ws.WriteMessage(
		websocket.TextMessage,
		v.Bytes(),
	)*/
	return nil
}

// blockFilter is a filter to store the updates of block
type blockFilter struct {
	filterBase
	sync.Mutex
	block *headElem
}

// takeBlockUpdates advances blocks from head to latest and returns header array
func (f *blockFilter) takeBlockUpdates() []*jsonHeader {
	updates, newHead := f.block.getUpdates()

	f.Lock()
	defer f.Unlock()

	f.block = newHead

	return updates
}

// getUpdates returns updates of blocks in string
func (f *blockFilter) getUpdates() (string, error) {
	headers := f.takeBlockUpdates()

	// alloc once and for all
	updates := make([]string, len(headers))
	for i, header := range headers {
		updates[i] = header.Hash.String()
	}

	return fmt.Sprintf("[\"%s\"]", strings.Join(updates, "\",\"")), nil
}

// sendUpdates writes the updates of blocks to web socket stream
func (f *blockFilter) sendUpdates() error {
	// todo
	/*updates := f.takeBlockUpdates()

	// it is block header actually
	for _, header := range updates {
		raw, err := json.Marshal(header)
		if err != nil {
			return err
		}

		if err := f.writeMessageToWs(string(raw)); err != nil {
			return err
		}
	}*/

	return nil
}

// logFilter is a filter to store logs that meet the conditions in query
type logFilter struct {
	filterBase
	sync.Mutex
	query *LogQuery
	logs  []*Log
}

// appendLog appends new log to logs
func (f *logFilter) appendLog(log *Log) {
	f.Lock()
	defer f.Unlock()

	f.logs = append(f.logs, log)
}

// takeLogUpdates returns all saved logs in filter and set new log slice
func (f *logFilter) takeLogUpdates() []*Log {
	f.Lock()
	defer f.Unlock()

	logs := f.logs
	f.logs = []*Log{} // create brand new slice so that prevent new logs from being added to current logs

	return logs
}

// getUpdates returns stored logs in string
func (f *logFilter) getUpdates() (string, error) {
	logs := f.takeLogUpdates()

	res, err := json.Marshal(logs)
	if err != nil {
		return "", err
	}

	return string(res), nil
}

// sendUpdates writes stored logs to web socket stream
func (f *logFilter) sendUpdates() error {
	logs := f.takeLogUpdates()

	for _, log := range logs {
		res, err := json.Marshal(log)
		if err != nil {
			return err
		}

		if err := f.writeMessageToWs(string(res)); err != nil {
			return err
		}
	}

	return nil
}

// filterManagerStore provides methods required by FilterManager
type filterManagerStore interface {
	// Header returns the current header of the chain (genesis if empty)
	Header() *types.Header

	// SubscribeEvents subscribes for chain head events
	SubscribeEvents() blockchain.Subscription

	// GetReceiptsByHash returns the receipts for a block hash
	GetReceiptsByHash(hash types.Hash) ([]*types.Receipt, error)

	// GetBlockByHash returns the block using the block hash
	GetBlockByHash(hash types.Hash, full bool) (*types.Block, bool)

	// GetBlockByNumber returns a block using the provided number
	GetBlockByNumber(num uint64, full bool) (*types.Block, bool)
}

// FilterManager manages all running filters
type FilterManager struct {
	sync.RWMutex // provide basic r/w lock

	logger hclog.Logger

	timeout time.Duration

	store           filterManagerStore
	subscription    blockchain.Subscription
	blockStream     *blockStream
	blockRangeLimit uint64

	filters  map[string]filter
	timeouts timeHeapImpl

	updateCh chan struct{}
	closeCh  chan struct{}
}

func NewFilterManager(logger hclog.Logger, store filterManagerStore, blockRangeLimit uint64) *FilterManager {
	m := &FilterManager{
		logger:          logger.Named("filter"),
		timeout:         defaultTimeout,
		store:           store,
		blockStream:     &blockStream{},
		blockRangeLimit: blockRangeLimit,
		filters:         make(map[string]filter),
		timeouts:        timeHeapImpl{},
		updateCh:        make(chan struct{}),
		closeCh:         make(chan struct{}),
	}

	// start blockstream with the current header
	header := store.Header()
	m.blockStream.push(header)

	// start the head watcher
	m.subscription = store.SubscribeEvents()

	return m
}

// Run starts worker process to handle events
func (f *FilterManager) Run() {
	// watch for new events in the blockchain
	watchCh := make(chan *blockchain.Event)

	go func() {
		for {
			evnt := f.subscription.GetEvent()
			if evnt == nil {
				return
			}
			watchCh <- evnt
		}
	}()

	// Do not use 'for range + create long time after chan' any more,
	// which would bring out some unpredictable result, especially when
	// re-assgining the chan, the elder one would not be recycled by
	// the GC as we expected.
	// Use 'timer + reset' instead.
	var checkTimer = time.NewTimer(_checkDuration)
	defer checkTimer.Stop()

	for {
		// check for the next filter to be removed
		filterBase := f.nextTimeoutFilter()
		// remove expired filter first
		if filterBase != nil && filterBase.expiresAt.Before(time.Now()) {
			f.logger.Info("filter timeout", "id", filterBase.id, "expiresAt", filterBase.expiresAt)
			f.Uninstall(filterBase.id)

			continue
		}

		// reset timer for next check
		// TODO: not safe use case
		checkTimer.Reset(_checkDuration)

		select {
		case ev := <-watchCh:
			// new blockchain event
			if err := f.dispatchEvent(ev); err != nil {
				f.logger.Error("failed to dispatch event", "err", err)
			}
		case <-checkTimer.C:
			// no need to do anything, checkout the timeout filter in the next loop
		case <-f.updateCh:
			// filters change, reset the loop to start the timeout timer
		case <-f.closeCh:
			// stop the filter manager
			return
		}
	}
}

// Close closed closeCh so that terminate worker
func (f *FilterManager) Close() {
	close(f.closeCh)
}

// NewBlockFilter adds new BlockFilter
func (f *FilterManager) NewBlockFilter( /*ws wsConn*/ ) string {
	/*filter := &blockFilter{
		filterBase: newFilterBase(ws),
		block:      f.blockStream.Head(),
	}

	if filter.hasWSConn() {
		ws.SetFilterID(filter.id)
	}

	return f.addFilter(filter)*/
	return ""
}

// NewLogFilter adds new LogFilter
func (f *FilterManager) NewLogFilter(logQuery *LogQuery, ws wsConn) string {
	filter := &logFilter{
		//filterBase: newFilterBase(ws),
		query: logQuery,
	}

	return f.addFilter(filter)
}

// Exists checks the filter with given ID exists
func (f *FilterManager) Exists(id string) bool {
	f.RLock()
	defer f.RUnlock()

	_, ok := f.filters[id]

	return ok
}

func (f *FilterManager) getLogsFromBlock(query *LogQuery, block *types.Block) ([]*Log, error) {
	receipts, err := f.store.GetReceiptsByHash(block.Header.Hash)
	if err != nil {
		return nil, err
	}

	logs := make([]*Log, 0)

	for idx, receipt := range receipts {
		for logIdx, log := range receipt.Logs {
			if !query.Match(log) {
				continue
			}

			logs = append(logs, &Log{
				Address:     log.Address,
				Topics:      log.Topics,
				Data:        argBytes(log.Data),
				BlockNumber: argUint64(block.Header.Number),
				BlockHash:   block.Header.Hash,
				TxHash:      block.Transactions[idx].Hash(),
				TxIndex:     argUint64(idx),
				LogIndex:    argUint64(logIdx),
			})
		}
	}

	return logs, nil
}

func (f *FilterManager) getLogsFromBlocks(query *LogQuery) ([]*Log, error) {
	latestBlockNumber := f.store.Header().Number

	resolveNum := func(num BlockNumber) (uint64, error) {
		switch num {
		case PendingBlockNumber:
			return 0, ErrPendingBlockNumber
		case EarliestBlockNumber:
			num = 0
		case LatestBlockNumber:
			return latestBlockNumber, nil
		}

		return uint64(num), nil
	}

	from, err := resolveNum(query.FromBlock)
	if err != nil {
		return nil, err
	}

	to, err := resolveNum(query.ToBlock)
	if err != nil {
		return nil, err
	}

	// If from equals genesis block
	// skip it
	if from == 0 {
		from = 1
	}

	if to < from {
		return nil, ErrIncorrectBlockRange
	}

	// if not disabled, avoid handling large block ranges
	if f.blockRangeLimit > 0 && to-from > f.blockRangeLimit {
		return nil, ErrBlockRangeTooHigh
	}

	logs := make([]*Log, 0)

	for i := from; i <= to; i++ {
		block, ok := f.store.GetBlockByNumber(i, true)
		if !ok {
			break
		}

		if len(block.Transactions) == 0 {
			// do not check logs if no txs
			continue
		}

		blockLogs, err := f.getLogsFromBlock(query, block)
		if err != nil {
			return nil, err
		}

		logs = append(logs, blockLogs...)
	}

	return logs, nil
}

// GetLogs return array of logs for given query
func (f *FilterManager) GetLogs(query *LogQuery) ([]*Log, error) {
	if query.BlockHash != nil {
		//	BlockHash is set -> fetch logs from this block only
		block, ok := f.store.GetBlockByHash(*query.BlockHash, true)
		if !ok {
			return nil, ErrBlockNotFound
		}

		if len(block.Transactions) == 0 {
			// no txs in block, return empty response
			return []*Log{}, nil
		}

		return f.getLogsFromBlock(query, block)
	}

	//	gets logs from a range of blocks
	return f.getLogsFromBlocks(query)
}

// getFilterByID fetches the filter by the ID
//
// Release lock as quick as possible
func (f *FilterManager) getFilterByID(filterID string) filter {
	f.RLock()
	defer f.RUnlock()

	return f.filters[filterID]
}

// GetLogFilterFromID return log filter for given filterID
func (f *FilterManager) GetLogFilterFromID(filterID string) (*logFilter, error) {
	filterRaw := f.getFilterByID(filterID)
	if filterRaw == nil {
		return nil, ErrFilterDoesNotExists
	}

	logFilter, ok := filterRaw.(*logFilter)
	if !ok {
		return nil, ErrCastingFilterToLogFilter
	}

	return logFilter, nil
}

// refreshFilterTimeout updates the timeout for a filter to the current time
func (f *FilterManager) refreshFilterTimeout(filter *filterBase) {
	f.timeouts.removeFilter(filter)
	f.addFilterTimeout(filter)
}

// addFilterTimeout set timeout and add to heap
func (f *FilterManager) addFilterTimeout(filter *filterBase) {
	filter.expiresAt = time.Now().Add(f.timeout)
	f.timeouts.addFilter(filter)
	f.emitSignalToUpdateCh()
}

// GetFilterChanges returns the updates of the filter with given ID in string,
// and refreshes the timeout on the filter
func (f *FilterManager) GetFilterChanges(id string) (string, error) {
	filter, res, err := f.getFilterAndChanges(id)

	if err == nil && !filter.hasWSConn() {
		// Refresh the timeout on this filter
		f.Lock()
		f.refreshFilterTimeout(filter.getFilterBase())
		f.Unlock()
	}

	return res, err
}

// getFilterAndChanges returns the updates of the filter with given ID in string
func (f *FilterManager) getFilterAndChanges(id string) (filter, string, error) {
	filter := f.getFilterByID(id)
	if filter == nil {
		return nil, "", ErrFilterDoesNotExists
	}

	// we cannot get updates from a ws filter with getFilterChanges
	if filter.hasWSConn() {
		return nil, "", ErrWSFilterDoesNotSupportGetChanges
	}

	res, err := filter.getUpdates()
	if err != nil {
		return nil, "", err
	}

	return filter, res, nil
}

// Uninstall removes the filter with given ID from list
func (f *FilterManager) Uninstall(id string) bool {
	f.Lock()
	defer f.Unlock()

	return f.removeFilterByID(id)
}

// removeFilterByID removes the filter with given ID
//
// Not thread safe
func (f *FilterManager) removeFilterByID(id string) bool {
	filter, ok := f.filters[id]
	if !ok {
		// not exits, should not retry
		f.logger.Debug("filter not in list", "id", id)

		return true
	}

	delete(f.filters, id)

	if removed := f.timeouts.removeFilter(filter.getFilterBase()); removed {
		f.logger.Debug("filter found in timeout heap", "id", id)
		f.emitSignalToUpdateCh()
	} else {
		f.logger.Debug("filter already removed from timeout heap", "id", id)
	}

	return true
}

// RemoveFilterByWs removes the filter with given WS [Thread safe]
func (f *FilterManager) RemoveFilterByWs( /*ws wsConn*/ ) {
	/*f.Lock()
	defer f.Unlock()

	f.removeFilterByID(ws.GetFilterID())*/
}

// addFilter is an internal method to add given filter to list and heap
func (f *FilterManager) addFilter(filter filter) string {
	f.Lock()
	defer f.Unlock()

	base := filter.getFilterBase()

	f.filters[base.id] = filter

	// Set timeout and add to heap if filter doesn't have web socket connection
	if !filter.hasWSConn() {
		f.addFilterTimeout(base)
	}

	f.logger.Debug("filter added", "id", base.id, "timeout", base.expiresAt)

	return base.id
}

func (f *FilterManager) emitSignalToUpdateCh() {
	select {
	// notify worker of new filter with timeout
	case f.updateCh <- struct{}{}:
	default:
	}
}

// nextTimeoutFilter returns the filter that will be expired next
// nextTimeoutFilter returns the only filter with timeout
func (f *FilterManager) nextTimeoutFilter() *filterBase {
	f.RLock()
	defer f.RUnlock()

	if len(f.timeouts) == 0 {
		return nil
	}

	// peek the first item
	base := f.timeouts[0]

	return base
}

// dispatchEvent is an event handler for new block event
func (f *FilterManager) dispatchEvent(evnt *blockchain.Event) error {
	// store new event in each filters
	f.processEvent(evnt)

	// send data to web socket stream
	if err := f.flushWsFilters(); err != nil {
		return err
	}

	return nil
}

// processEvent makes each filter append the new data that interests them
func (f *FilterManager) processEvent(evnt *blockchain.Event) {
	f.RLock()
	defer f.RUnlock()

	for _, header := range evnt.NewChain {
		// first include all the new headers in the blockstream for BlockFilter
		f.blockStream.push(header)

		// process new chain to include new logs for LogFilter
		if processErr := f.appendLogsToFilters(header); processErr != nil {
			f.logger.Error(fmt.Sprintf("Unable to process block, %v", processErr))
		}
	}
}

// appendLogsToFilters makes each LogFilters append logs in the header
//
// Would not append any removed logs.
func (f *FilterManager) appendLogsToFilters(header *types.Header) error {
	receipts, err := f.store.GetReceiptsByHash(header.Hash)
	if err != nil {
		return err
	}

	// Get logFilters from filters
	logFilters := f.getLogFilters()
	if len(logFilters) == 0 {
		return nil
	}

	block, ok := f.store.GetBlockByHash(header.Hash, true)
	if !ok {
		f.logger.Error("could not find block in store", "hash", header.Hash.String())

		return nil
	}

	for indx, receipt := range receipts {
		// check the logs with the filters
		for _, log := range receipt.Logs {
			for _, f := range logFilters {
				if !f.query.Match(log) {
					continue
				}

				if receipt.TxHash == types.ZeroHash {
					// Extract tx Hash
					receipt.TxHash = block.Transactions[indx].Hash()
				}

				f.appendLog(&Log{
					Address:     log.Address,
					Topics:      log.Topics,
					Data:        argBytes(log.Data),
					BlockNumber: argUint64(header.Number),
					BlockHash:   header.Hash,
					TxHash:      receipt.TxHash,
					TxIndex:     argUint64(indx),
					Removed:     false,
				})
			}
		}
	}

	return nil
}

// flushWsFilters make each filters with web socket connection write the updates to web socket stream
// flushWsFilters also removes the filters if flushWsFilters notices the connection is closed
func (f *FilterManager) flushWsFilters() error {
	closedFilterIDs := make([]string, 0)

	f.RLock()

	for id, filter := range f.filters {
		if !filter.hasWSConn() {
			continue
		}

		if flushErr := filter.sendUpdates(); flushErr != nil {
			// mark as closed if the connection is closed
			if errors.Is(flushErr, websocket.ErrCloseSent) {
				closedFilterIDs = append(closedFilterIDs, id)

				f.logger.Warn(fmt.Sprintf("Subscription %s has been closed", id))

				continue
			}

			f.logger.Error(fmt.Sprintf("Unable to process flush, %v", flushErr))
		}
	}

	f.RUnlock()

	// remove filters with closed web socket connections from FilterManager
	if len(closedFilterIDs) > 0 {
		f.Lock()
		for _, id := range closedFilterIDs {
			f.removeFilterByID(id)
		}
		f.Unlock()

		f.logger.Info(fmt.Sprintf("Removed %d filters due to closed connections", len(closedFilterIDs)))
	}

	return nil
}

// getLogFilters returns logFilters
func (f *FilterManager) getLogFilters() []*logFilter {
	f.RLock()
	defer f.RUnlock()

	logFilters := make([]*logFilter, 0)

	for _, f := range f.filters {
		if logFilter, ok := f.(*logFilter); ok {
			logFilters = append(logFilters, logFilter)
		}
	}

	return logFilters
}

type timeHeapImpl []*filterBase

func (t *timeHeapImpl) addFilter(filter *filterBase) {
	heap.Push(t, filter)
}

func (t *timeHeapImpl) removeFilter(filter *filterBase) bool {
	if filter.heapIndex == NoIndexInHeap {
		return false
	}

	heap.Remove(t, filter.heapIndex)

	return true
}

func (t timeHeapImpl) Len() int { return len(t) }

func (t timeHeapImpl) Less(i, j int) bool {
	return t[i].expiresAt.Before(t[j].expiresAt)
}

func (t timeHeapImpl) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
	t[i].heapIndex = i
	t[j].heapIndex = j
}

func (t *timeHeapImpl) Push(x interface{}) {
	n := len(*t)
	item := x.(*filterBase) //nolint:forcetypeassert
	item.heapIndex = n
	*t = append(*t, item)
}

func (t *timeHeapImpl) Pop() interface{} {
	old := *t
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.heapIndex = NoIndexInHeap // pop out and set it to not in heap
	*t = old[0 : n-1]

	return item
}

// blockStream is used to keep the stream of new block hashes and allow subscriptions
// of the stream at any point
type blockStream struct {
	lock sync.Mutex
	head *headElem
}

func (b *blockStream) Head() *headElem {
	b.lock.Lock()
	defer b.lock.Unlock()

	return b.head
}

func (b *blockStream) push(header *types.Header) {
	b.lock.Lock()
	defer b.lock.Unlock()

	newHead := &headElem{
		header: toJSONHeader(header),
	}

	if b.head != nil {
		b.head.next = newHead
	}

	b.head = newHead
}

type headElem struct {
	header *jsonHeader
	next   *headElem
}

func (h *headElem) getUpdates() ([]*jsonHeader, *headElem) {
	res := make([]*jsonHeader, 0)

	cur := h

	for cur.next != nil {
		cur = cur.next
		res = append(res, cur.header)
	}

	return res, cur
}