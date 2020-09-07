package indexer

import (
	"github.com/couchbase/indexing/secondary/common"
	"github.com/couchbase/indexing/secondary/logging"
	"github.com/couchbase/indexing/secondary/manager/client"
	mc "github.com/couchbase/indexing/secondary/manager/common"

	"container/heap"
	"errors"
	"io"
	"math/rand"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var SCHED_TOKEN_CHECK_INTERVAL = 5000   // Milliseconds
var SCHED_TOKEN_PROCESS_INTERVAL = 5000 // Milliseconds
var STOP_TOKEN_CLEANER_INITERVAL = 60   // Seconds
var STOP_TOKEN_RETENTION_TIME = 600     // Seconds

var RETRYABLE_ERROR_BACKOFF = int64(5 * time.Second)
var NON_RETRYABLE_ERROR_BACKOFF = int64(5 * time.Second)
var NETWORK_ERROR_BACKOFF = int64(5 * time.Second)

var RANDOM_BACKOFF_START = 50 // Milliseconds
var RANDOM_BACKOFF_END = 5000 // Milliseconds

var MAX_CREATION_RETRIES = 100

/////////////////////////////////////////////////////////////////////
// Global Variables
/////////////////////////////////////////////////////////////////////

var gSchedIndexCreator *schedIndexCreator
var gSchedIndexCreatorLck sync.Mutex
var useSecondsFromCtime bool

func init() {
	if unsafe.Sizeof(int(1)) < unsafe.Sizeof(int64(1)) {
		useSecondsFromCtime = true
	}
}

//
// Get schedIndexCreator singleton
//
func getSchedIndexCreator() *schedIndexCreator {

	gSchedIndexCreatorLck.Lock()
	defer gSchedIndexCreatorLck.Unlock()

	return gSchedIndexCreator
}

/////////////////////////////////////////////////////////////////////
// Data Types
/////////////////////////////////////////////////////////////////////

//
// Scheduled index creator (schedIndexCreator), checks up on the scheduled
// create tokens and tries to create index with the help of metadata provider.
// 1. If the index creation succeeds, scheduled create token is deleted.
// 2. If the index creation fails, the index creation will be retried, based
//    on the reason for failure.
// 3. The retry count is tracked in memory and after the retry limit is reached
//    the stop schedule create token will be posted with last error.
// 3.1. Once the stop schedule create token is posted, the index creation will
//      not be retried again (until indexer restarts). The schedule create token
//      will NOT be deleted. Failed index will continue to show on UI (in Error
//      state), until the user explicitly deletes the failed index.
// 4. Similar to DDL service manager, scheduled index creator observes mutual
//    exclusion with rebalance operation.
type schedIndexCreator struct {
	indexerId   common.IndexerId
	config      common.ConfigHolder
	provider    *client.MetadataProvider
	proMutex    sync.Mutex
	supvCmdch   MsgChannel //supervisor sends commands on this channel
	supvMsgch   MsgChannel //channel to send any message to supervisor
	clusterAddr string
	settings    *ddlSettings
	killch      chan bool
	allowDDL    bool
	mutex       sync.Mutex
	mon         *schedTokenMonitor
	indexQueue  *schedIndexQueue
	queueMutex  sync.Mutex
	backoff     int64 // random backoff in nanoseconds before the next request.
	cleanerStop chan bool
}

//
// scheduledIndex type holds information about one index that is scheduled
// for background creation. This information contains the (1) token and
// (2) information related to failed attempts to create this index.
// The scheduledIndexes are maintained as a heap, so the struct implements
// properties required by golang contanier/heap implementation.
//
type scheduledIndex struct {
	token *mc.ScheduleCreateToken
	state *scheduledIndexState

	// container/heap properties
	priority int
	index    int
}

//
// scheduledIndexState maintains the information about the failed attempts
// at the time of creating the index.
//
type scheduledIndexState struct {
	lastError   error // Error returned by the last creation attempt.
	retryCount  int   // Number of attempts failed by retryable errors.
	nRetryCount int   // Number of attempts failed by non retryable errors.
}

//
// Schedule token monitor checks if there are any new indexes that are
// scheduled for creation.
//
type schedTokenMonitor struct {
	creator         *schedIndexCreator
	commandListener *mc.CommandListener
	listenerDonech  chan bool
	uCloseCh        chan bool
	processed       map[string]bool
	indexerId       common.IndexerId
}

//
// schedIndexQueue is used as a priority queue where the indexes are processed
// in fist-come-first-served manner, based on the creation time of the request.
//
type schedIndexQueue []*scheduledIndex

///////////////////////////////////////////////////////////////////
// Constructor and member functions for schedIndexCreator
/////////////////////////////////////////////////////////////////////

func NewSchedIndexCreator(indexerId common.IndexerId, supvCmdch MsgChannel,
	supvMsgch MsgChannel, config common.Config) (*schedIndexCreator, Message) {

	addr := config["clusterAddr"].String()
	numReplica := int32(config["settings.num_replica"].Int())
	settings := &ddlSettings{numReplica: numReplica}

	iq := make(schedIndexQueue, 0)
	heap.Init(&iq)

	mgr := &schedIndexCreator{
		indexerId:   indexerId,
		supvCmdch:   supvCmdch,
		supvMsgch:   supvMsgch,
		clusterAddr: addr,
		settings:    settings,
		killch:      make(chan bool),
		allowDDL:    true,
		indexQueue:  &iq,
		cleanerStop: make(chan bool),
	}

	mgr.mon = NewSchedTokenMonitor(mgr, indexerId)

	mgr.config.Store(config)

	go mgr.run()
	go mgr.stopTokenCleaner()

	gSchedIndexCreatorLck.Lock()
	defer gSchedIndexCreatorLck.Unlock()
	gSchedIndexCreator = mgr

	logging.Infof("schedIndexCreator: intialized.")

	return mgr, &MsgSuccess{}
}

func (m *schedIndexCreator) run() {

	go m.processSchedIndexes()

loop:
	for {
		select {

		case cmd, ok := <-m.supvCmdch:
			if ok {
				if cmd.GetMsgType() == ADMIN_MGR_SHUTDOWN {
					m.Close()
					m.supvCmdch <- &MsgSuccess{}
					break loop
				}

				m.handleSupervisorCommands(cmd)
			} else {
				//supervisor channel closed. exit
				break loop
			}
		}
	}
}

func (m *schedIndexCreator) handleSupervisorCommands(cmd Message) {
	switch cmd.GetMsgType() {

	case CONFIG_SETTINGS_UPDATE:
		cfgUpdate := cmd.(*MsgConfigUpdate)
		m.config.Store(cfgUpdate.GetConfig())
		m.settings.handleSettings(cfgUpdate.GetConfig())
		m.supvCmdch <- &MsgSuccess{}

	default:
		logging.Fatalf("schedIndexCreator::handleSupervisorCommands Unknown Message %+v", cmd)
		common.CrashOnError(errors.New("Unknown Msg On Supv Channel"))
	}
}

func (m *schedIndexCreator) stopProcessDDL() {

	func() {
		m.mutex.Lock()
		defer m.mutex.Unlock()

		m.allowDDL = false
	}()
}

func (m *schedIndexCreator) canProcessDDL() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.allowDDL
}

func (m *schedIndexCreator) startProcessDDL() {
	func() {
		m.mutex.Lock()
		defer m.mutex.Unlock()

		m.allowDDL = true
	}()

	func() {
		m.proMutex.Lock()
		defer m.proMutex.Unlock()

		m.provider = nil
	}()
}

func (m *schedIndexCreator) rebalanceDone() {
	// TODO: Need to check if provider needs to be reset.
}

// TODO: Integrate with rebalance start / finish

func (m *schedIndexCreator) processSchedIndexes() {

	// Check for new indexes to be created every 5 seconds
	ticker := time.NewTicker(time.Duration(SCHED_TOKEN_PROCESS_INTERVAL) * time.Millisecond)

	for {
		select {
		case <-ticker.C:
			if !m.canProcessDDL() {
				continue
			}

		innerLoop:
			for {
				if !m.canProcessDDL() {
					break innerLoop
				}

				index := m.popQ()
				if index == nil {
					break innerLoop
				}

				m.processIndex(index)
			}

		case <-m.killch:
			logging.Infof("schedIndexCreator: Stopping processSchedIndexes routine ...")
			return
		}
	}
}

func (m *schedIndexCreator) processIndex(index *scheduledIndex) {
	logging.Infof("schedIndexCreator: Trying to create index %v, %v", index.token.Definition.DefnId, index.token.Ctime)
	err, success := m.tryCreateIndex(index)
	if err != nil {
		retry, dropToken := m.handleError(index, err)
		if retry {
			logging.Errorf("schedIndexCreator: error(%v) while creating index %v. The operation will be retried. Current retry counts (%v,%v).",
				err, index.token.Definition.DefnId, index.state.retryCount, index.state.nRetryCount)
			m.pushQ(index)
		} else {
			// TODO: Check if we need a console log
			logging.Errorf("schedIndexCreator: error(%v) while creating index %v. The operation failed after retry counts (%v,%v).",
				err, index.token.Definition.DefnId, index.state.retryCount, index.state.nRetryCount)

			if !dropToken {
				err := mc.PostStopScheduleCreateToken(index.token.Definition.DefnId, err.Error(), time.Now().UnixNano())
				if err != nil {
					logging.Errorf("schedIndexCreator: error (%v) in posting the stop schedule create token for %v",
						err, index.token.Definition.DefnId)
				}
			} else {
				err := mc.DeleteScheduleCreateToken(index.token.Definition.DefnId)
				if err != nil {
					logging.Errorf("schedIndexCreator: error (%v) in deleting the schedule create token for %v",
						err, index.token.Definition.DefnId)
				}
			}
		}
	} else {
		if success {
			logging.Infof("schedIndexCreator: successfully created index %v", index.token.Definition.DefnId)
			if !index.token.Definition.Deferred {
				logging.Infof("schedIndexCreator: index %v was created with non-deferred build. "+
					"DDL service manager will build the index", index.token.Definition.DefnId)
			}

			// TODO: Check if this doesn't error out in case of key not found.
			err := mc.DeleteScheduleCreateToken(index.token.Definition.DefnId)
			if err != nil {
				logging.Errorf("schedIndexCreator: error (%v) in deleting the schedule create token for %v",
					err, index.token.Definition.DefnId)
			}
		}
	}

}

func (m *schedIndexCreator) handleError(index *scheduledIndex, err error) (bool, bool) {
	index.state.lastError = err

	checkErr := func(knownErrs []error) bool {
		for _, e := range knownErrs {
			if strings.Contains(err.Error(), e.Error()) {
				return true
			}
		}

		return false
	}

	retryable := false
	network := false

	setBackoff := func() {
		if strings.Contains(err.Error(), common.ErrAnotherIndexCreation.Error()) {

			// TODO: The value of this backoff should be a function of
			//       network latency and number of indexer nodes.

			diff := RANDOM_BACKOFF_END - RANDOM_BACKOFF_START
			b := rand.Intn(diff) + RANDOM_BACKOFF_START
			m.backoff = int64(b * 1000 * 1000)
			return
		}

		if retryable {
			if network {
				m.backoff = NETWORK_ERROR_BACKOFF
				return
			}

			m.backoff = RETRYABLE_ERROR_BACKOFF
			return
		}

		m.backoff = NON_RETRYABLE_ERROR_BACKOFF
	}

	defer setBackoff()

	// Check for known non-retryable errors.
	if checkErr(common.NonRetryableErrorsInCreate) {
		// TODO: Fix non-retryable error retry count.
		index.state.nRetryCount++

		if checkErr(common.KeyspaceDeletedErrorsInCreate) {
			return false, true
		}
		return false, false
	}

	retryable = true
	index.state.retryCount++
	if index.state.retryCount > MAX_CREATION_RETRIES {
		return false, false
	}

	// Check for known retryable error
	if checkErr(common.RetryableErrorsInCreate) {
		return true, false
	}

	isNetworkError := func() bool {
		// Because the exact error may have got embedded in the error
		// received, need to check for substring. If any of the following
		// substring is found, most likely the error is network error.
		if strings.Contains(err.Error(), io.EOF.Error()) ||
			strings.Contains(err.Error(), syscall.ECONNRESET.Error()) ||
			strings.Contains(err.Error(), syscall.EPIPE.Error()) ||
			strings.Contains(err.Error(), "i/o timeout") {

			return true
		}

		return false
	}

	if isNetworkError() {
		network = true
		return true, false
	}

	// Treat all unknown erros as retryable errors
	return true, false
}

func (m *schedIndexCreator) getMetadataProvider() (*client.MetadataProvider, error) {

	m.proMutex.Lock()
	defer m.proMutex.Unlock()

	if m.provider == nil {
		provider, _, err := newMetadataProvider(m.clusterAddr, nil, m.settings, "schedIndexCreator")
		if err != nil {
			return nil, err
		}

		m.provider = provider
	}

	return m.provider, nil
}

func (m *schedIndexCreator) tryCreateIndex(index *scheduledIndex) (error, bool) {
	exists, err := mc.StopScheduleCreateTokenExist(index.token.Definition.DefnId)
	if err != nil {
		logging.Errorf("schedIndexCreator:tryCreateIndex error (%v) in getting stop schedule create token for %v",
			err, index.token.Definition.DefnId)
		return err, false
	}

	if exists {
		logging.Debugf("schedIndexCreator:tryCreateIndex stop schedule token exists for %v",
			index.token.Definition.DefnId)
		return nil, false
	}

	exists, err = mc.DeleteCommandTokenExist(index.token.Definition.DefnId)
	if err != nil {
		logging.Errorf("schedIndexCreator:tryCreateIndex error (%v) in getting delete command token for %v",
			err, index.token.Definition.DefnId)
		return err, false
	}

	if exists {
		logging.Infof("schedIndexCreator:tryCreateIndex delete command token exists for %v",
			index.token.Definition.DefnId)
		return nil, false
	}

	if m.backoff > 0 {
		logging.Debugf("schedIndexCreator:tryCreateIndex using %v backoff for index %v",
			time.Duration(m.backoff), index.token.Definition.DefnId)
		time.Sleep(time.Duration(m.backoff))

		// Reset the backoff for the next attempt
		m.backoff = 0
	}

	var provider *client.MetadataProvider
	provider, err = m.getMetadataProvider()
	if err != nil {
		logging.Errorf("schedIndexCreator:tryCreateIndex error (%v) in getting getMetadataProvider for %v",
			err, index.token.Definition.DefnId)
		return err, false
	}

	// Following check is an effort to check if index was created but the
	// schedule create token was not deleted.
	if provider.FindIndexIgnoreStatus(index.token.Definition.DefnId) != nil {
		logging.Infof("schedIndexCreator:tryCreateIndex index %v is already created", index.token.Definition.DefnId)
		return nil, true
	}

	// If the bucket/scope/collection was dropped after the token creation,
	// the index creation should fail due to id mismatch.
	index.token.Definition.BucketUUID = index.token.BucketUUID
	index.token.Definition.ScopeId = index.token.ScopeId
	index.token.Definition.CollectionId = index.token.CollectionId

	err = provider.CreateIndexWithDefnAndPlan(&index.token.Definition, index.token.Plan, index.token.Ctime)
	if err != nil {
		return err, false
	}

	return nil, true
}

func (m *schedIndexCreator) pushQ(item *scheduledIndex) {
	m.queueMutex.Lock()
	defer m.queueMutex.Unlock()

	heap.Push(m.indexQueue, item)
}

func (m *schedIndexCreator) popQ() *scheduledIndex {
	m.queueMutex.Lock()
	defer m.queueMutex.Unlock()

	if m.indexQueue.Len() <= 0 {
		return nil
	}

	si, ok := heap.Pop(m.indexQueue).(*scheduledIndex)
	if !ok {
		return nil
	}

	return si
}

func (m *schedIndexCreator) stopTokenCleaner() {
	ticker := time.NewTicker(time.Duration(STOP_TOKEN_CLEANER_INITERVAL) * time.Second)
	retention := int64(STOP_TOKEN_RETENTION_TIME) * int64(time.Second)

	for {
		select {
		case <-ticker.C:
			stopTokens, err := mc.ListAllStopScheduleCreateTokens()
			if err != nil {
				logging.Errorf("schedIndexCreator:stopTokenCleaner error in getting stop schedule create tokens: %v", err)
				continue
			}

			for _, token := range stopTokens {
				exists, err := mc.ScheduleCreateTokenExist(token.DefnId)
				if err != nil {
					logging.Infof("schedIndexCreator:stopTokenCleaner error (%v) in ScheduleCreateTokenExist for %v", err, token.DefnId)
					continue
				}

				if exists {
					logging.Debugf("schedIndexCreator:stopTokenCleaner schedule create token exists for defnId %v", token.DefnId)
					continue
				}

				if (time.Now().UnixNano() - retention) > token.Ctime {
					logging.Infof("schedIndexCreator:stopTokenCleaner deleting stop create token for %v", token.DefnId)
					// TODO: Avoid deletion by all indexers.
					err := mc.DeleteStopScheduleCreateToken(token.DefnId)
					if err != nil {
						logging.Errorf("schedIndexCreator:stopTokenCleaner error (%v) in deleting stop create token for %v", err, token.DefnId)
					}
				}
			}

		case <-m.cleanerStop:
			logging.Infof("schedIndexCreator: Stoppinig stopTokenCleaner routine")
			return
		}
	}
}

func (m *schedIndexCreator) Close() {
	logging.Infof("schedIndexCreator: Shutting Down ...")
	close(m.killch)
	close(m.cleanerStop)
}

/////////////////////////////////////////////////////////////////////
// Constructor and member functions for scheduledIndex
/////////////////////////////////////////////////////////////////////

func NewScheduledIndex(token *mc.ScheduleCreateToken) *scheduledIndex {
	var priority int
	if useSecondsFromCtime {
		// get ctime at the granularity of a second
		seconds := token.Ctime / int64(1000*1000*1000)
		priority = int(seconds)
	} else {
		priority = int(token.Ctime)
	}

	return &scheduledIndex{
		token:    token,
		state:    &scheduledIndexState{},
		priority: priority,
	}
}

/////////////////////////////////////////////////////////////////////
// Constructor and member functions for schedTokenMonitor
/////////////////////////////////////////////////////////////////////

func NewSchedTokenMonitor(creator *schedIndexCreator, indexerId common.IndexerId) *schedTokenMonitor {

	lCloseCh := make(chan bool)
	listener := mc.NewCommandListener(lCloseCh, false, false, false, false, true, false)

	s := &schedTokenMonitor{
		creator:         creator,
		commandListener: listener,
		listenerDonech:  lCloseCh,
		processed:       make(map[string]bool),
		uCloseCh:        make(chan bool),
		indexerId:       indexerId,
	}

	go s.updater()

	return s
}

func (s *schedTokenMonitor) checkProcessed(key string) bool {

	if _, ok := s.processed[key]; ok {
		return true
	}

	return false
}

func (s *schedTokenMonitor) markProcessed(key string) {

	s.processed[key] = true
}

func (s *schedTokenMonitor) update() {
	createTokens := s.commandListener.GetNewScheduleCreateTokens()
	for key, token := range createTokens {
		if s.checkProcessed(key) {
			continue
		}

		if token.IndexerId != s.indexerId {
			continue
		}

		exists, err := mc.StopScheduleCreateTokenExist(token.Definition.DefnId)
		if err != nil {
			logging.Errorf("schedIndexCreator: Error in getting stop schedule create token for %v", token.Definition.DefnId)
			continue
		}

		if exists {
			logging.Debugf("schedIndexCreator: stop schedule token exists for %v", token.Definition.DefnId)
			continue
		}

		s.creator.pushQ(NewScheduledIndex(token))

		s.markProcessed(key)
	}
}

func (s *schedTokenMonitor) updater() {
	s.commandListener.ListenTokens()

	ticker := time.NewTicker(time.Duration(SCHED_TOKEN_CHECK_INTERVAL) * time.Millisecond)

	for {
		select {
		case <-ticker.C:
			s.update()

		case <-s.uCloseCh:
			return
		}
	}
}

func (s *schedTokenMonitor) Close() {
	s.commandListener.Close()
	close(s.uCloseCh)
}

/////////////////////////////////////////////////////////////////////
// Member functions (heap implementation) for schedIndexQueue
// As the schedIndexQueue is implemented as a container/heap,
// following methods are the exposed methods and will be called
// from the golang container/heap library. Please don't call
// these methods directly.
/////////////////////////////////////////////////////////////////////

func (q schedIndexQueue) Len() int {
	return len(q)
}

func (q schedIndexQueue) Less(i, j int) bool {
	return q[i].priority < q[j].priority
}

func (q schedIndexQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *schedIndexQueue) Push(x interface{}) {
	n := len(*q)
	item := x.(*scheduledIndex)
	item.index = n
	*q = append(*q, item)
}

func (q *schedIndexQueue) Pop() interface{} {
	old := *q
	n := len(old)
	if n == 0 {
		return nil
	}

	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	*q = old[0 : n-1]
	return item
}