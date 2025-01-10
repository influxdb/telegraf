// The code contained here came from https://github.com/mongodb/mongo-tools/blob/master/mongostat/stat_types.go
// and contains modifications so that no other dependency from that project is needed. Other modifications included
// removing unnecessary code specific to formatting the output and determine the current state of the database. It
// is licensed under Apache Version 2.0, http://www.apache.org/licenses/LICENSE-2.0.html

package mongodb

import (
	"sort"
	"strings"
	"time"
)

const (
	mongosProcess = "mongos"
)

type mongoStatus struct {
	SampleTime    time.Time
	ServerStatus  *serverStatus
	ReplSetStatus *replSetStatus
	ClusterStatus *clusterStatus
	DbStats       *dbStats
	ColStats      *colStats
	ShardStats    *shardStats
	OplogStats    *oplogStats
	TopStats      *topStats
}

type serverStatus struct {
	SampleTime         time.Time              `bson:""`
	Flattened          map[string]interface{} `bson:""`
	Host               string                 `bson:"host"`
	Version            string                 `bson:"version"`
	Process            string                 `bson:"process"`
	Pid                int64                  `bson:"pid"`
	Uptime             int64                  `bson:"uptime"`
	UptimeMillis       int64                  `bson:"uptimeMillis"`
	UptimeEstimate     int64                  `bson:"uptimeEstimate"`
	LocalTime          time.Time              `bson:"localTime"`
	Asserts            *assertsStats          `bson:"asserts"`
	BackgroundFlushing *flushStats            `bson:"backgroundFlushing"`
	ExtraInfo          *extraInfo             `bson:"extra_info"`
	Connections        *connectionStats       `bson:"connections"`
	Dur                *durStats              `bson:"dur"`
	GlobalLock         *globalLockStats       `bson:"globalLock"`
	Locks              map[string]lockStats   `bson:"locks,omitempty"`
	Network            *networkStats          `bson:"network"`
	Opcounters         *opcountStats          `bson:"opcounters"`
	OpcountersRepl     *opcountStats          `bson:"opcountersRepl"`
	OpLatencies        *opLatenciesStats      `bson:"opLatencies"`
	RecordStats        *dbRecordStats         `bson:"recordStats"`
	Mem                *memStats              `bson:"mem"`
	Repl               *replStatus            `bson:"repl"`
	ShardCursorType    map[string]interface{} `bson:"shardCursorType"`
	StorageEngine      *storageEngine         `bson:"storageEngine"`
	WiredTiger         *wiredTiger            `bson:"wiredTiger"`
	Metrics            *metricsStats          `bson:"metrics"`
	TCMallocStats      *tcMallocStats         `bson:"tcmalloc"`
}

// dbStats stores stats from all dbs
type dbStats struct {
	Dbs []db
}

// db represent a single DB
type db struct {
	Name        string
	DbStatsData *dbStatsData
}

// dbStatsData stores stats from a db
type dbStatsData struct {
	Db          string      `bson:"db"`
	Collections int64       `bson:"collections"`
	Objects     int64       `bson:"objects"`
	AvgObjSize  float64     `bson:"avgObjSize"`
	DataSize    int64       `bson:"dataSize"`
	StorageSize int64       `bson:"storageSize"`
	NumExtents  int64       `bson:"numExtents"`
	Indexes     int64       `bson:"indexes"`
	IndexSize   int64       `bson:"indexSize"`
	Ok          int64       `bson:"ok"`
	GleStats    interface{} `bson:"gleStats"`
	FsUsedSize  int64       `bson:"fsUsedSize"`
	FsTotalSize int64       `bson:"fsTotalSize"`
}

type colStats struct {
	Collections []collection
}

type collection struct {
	Name         string
	DbName       string
	ColStatsData *colStatsData
}

type colStatsData struct {
	Collection     string  `bson:"ns"`
	Count          int64   `bson:"count"`
	Size           int64   `bson:"size"`
	AvgObjSize     float64 `bson:"avgObjSize"`
	StorageSize    int64   `bson:"storageSize"`
	TotalIndexSize int64   `bson:"totalIndexSize"`
	Ok             int64   `bson:"ok"`
}

// clusterStatus stores information related to the whole cluster
type clusterStatus struct {
	JumboChunksCount int64
}

// replSetStatus stores information from replSetGetStatus
type replSetStatus struct {
	Members []replSetMember `bson:"members"`
	MyState int64           `bson:"myState"`
}

// oplogStats stores information from getReplicationInfo
type oplogStats struct {
	TimeDiff int64
}

// replSetMember stores information related to a replica set member
type replSetMember struct {
	Name       string    `bson:"name"`
	Health     int64     `bson:"health"`
	State      int64     `bson:"state"`
	StateStr   string    `bson:"stateStr"`
	OptimeDate time.Time `bson:"optimeDate"`
}

// wiredTiger stores information related to the wiredTiger storage engine.
type wiredTiger struct {
	Transaction transactionStats       `bson:"transaction"`
	Concurrent  concurrentTransactions `bson:"concurrentTransactions"`
	Cache       cacheStats             `bson:"cache"`
	Connection  wtConnectionStats      `bson:"connection"`
	DataHandle  dataHandleStats        `bson:"data-handle"`
}

// shardStats stores information from shardConnPoolStats.
type shardStats struct {
	shardStatsData `bson:",inline"`
	Hosts          map[string]shardHostStatsData `bson:"hosts"`
}

// shardStatsData is the total Shard Stats from shardConnPoolStats database command.
type shardStatsData struct {
	TotalInUse      int64 `bson:"totalInUse"`
	TotalAvailable  int64 `bson:"totalAvailable"`
	TotalCreated    int64 `bson:"totalCreated"`
	TotalRefreshing int64 `bson:"totalRefreshing"`
}

// shardHostStatsData is the host-specific stats from shardConnPoolStats database command.
type shardHostStatsData struct {
	InUse      int64 `bson:"inUse"`
	Available  int64 `bson:"available"`
	Created    int64 `bson:"created"`
	Refreshing int64 `bson:"refreshing"`
}

type topStats struct {
	Totals map[string]topStatCollection `bson:"totals"`
}

type topStatCollection struct {
	Total     topStatCollectionData `bson:"total"`
	ReadLock  topStatCollectionData `bson:"readLock"`
	WriteLock topStatCollectionData `bson:"writeLock"`
	Queries   topStatCollectionData `bson:"queries"`
	GetMore   topStatCollectionData `bson:"getmore"`
	Insert    topStatCollectionData `bson:"insert"`
	Update    topStatCollectionData `bson:"update"`
	Remove    topStatCollectionData `bson:"remove"`
	Commands  topStatCollectionData `bson:"commands"`
}

type topStatCollectionData struct {
	Time  int64 `bson:"time"`
	Count int64 `bson:"count"`
}

type concurrentTransactions struct {
	Write concurrentTransStats `bson:"write"`
	Read  concurrentTransStats `bson:"read"`
}

type concurrentTransStats struct {
	Out          int64 `bson:"out"`
	Available    int64 `bson:"available"`
	TotalTickets int64 `bson:"totalTickets"`
}

// assertsStats stores information related to assertions raised since the MongoDB process started
type assertsStats struct {
	Regular   int64 `bson:"regular"`
	Warning   int64 `bson:"warning"`
	Msg       int64 `bson:"msg"`
	User      int64 `bson:"user"`
	Rollovers int64 `bson:"rollovers"`
}

// cacheStats stores cache statistics for wiredTiger.
type cacheStats struct {
	TrackedDirtyBytes         int64 `bson:"tracked dirty bytes in the cache"`
	CurrentCachedBytes        int64 `bson:"bytes currently in the cache"`
	MaxBytesConfigured        int64 `bson:"maximum bytes configured"`
	AppThreadsPageReadCount   int64 `bson:"application threads page read from disk to cache count"`
	AppThreadsPageReadTime    int64 `bson:"application threads page read from disk to cache time (usecs)"`
	AppThreadsPageWriteCount  int64 `bson:"application threads page write from cache to disk count"`
	AppThreadsPageWriteTime   int64 `bson:"application threads page write from cache to disk time (usecs)"`
	BytesWrittenFrom          int64 `bson:"bytes written from cache"`
	BytesReadInto             int64 `bson:"bytes read into cache"`
	PagesEvictedByAppThread   int64 `bson:"pages evicted by application threads"`
	PagesQueuedForEviction    int64 `bson:"pages queued for eviction"`
	PagesReadIntoCache        int64 `bson:"pages read into cache"`
	PagesWrittenFromCache     int64 `bson:"pages written from cache"`
	PagesRequestedFromCache   int64 `bson:"pages requested from the cache"`
	ServerEvictingPages       int64 `bson:"eviction server evicting pages"`
	WorkerThreadEvictingPages int64 `bson:"eviction worker thread evicting pages"`
	InternalPagesEvicted      int64 `bson:"internal pages evicted"`
	ModifiedPagesEvicted      int64 `bson:"modified pages evicted"`
	UnmodifiedPagesEvicted    int64 `bson:"unmodified pages evicted"`
}

type storageEngine struct {
	Name string `bson:"name"`
}

// transactionStats stores transaction checkpoints in wiredTiger.
type transactionStats struct {
	TransCheckpointsTotalTimeMsecs int64 `bson:"transaction checkpoint total time (msecs)"`
	TransCheckpoints               int64 `bson:"transaction checkpoints"`
}

// wtConnectionStats stores statistics on wiredTiger connections
type wtConnectionStats struct {
	FilesCurrentlyOpen int64 `bson:"files currently open"`
}

// dataHandleStats stores statistics for wiredTiger data-handles
type dataHandleStats struct {
	DataHandlesCurrentlyActive int64 `bson:"connection data handles currently active"`
}

// replStatus stores data related to replica sets.
type replStatus struct {
	SetName           string      `bson:"setName"`
	IsWritablePrimary interface{} `bson:"isWritablePrimary"` // mongodb 5.x
	IsMaster          interface{} `bson:"ismaster"`
	Secondary         interface{} `bson:"secondary"`
	IsReplicaSet      interface{} `bson:"isreplicaset"`
	ArbiterOnly       interface{} `bson:"arbiterOnly"`
	Hosts             []string    `bson:"hosts"`
	Passives          []string    `bson:"passives"`
	Me                string      `bson:"me"`
}

// dbRecordStats stores data related to memory operations across databases.
type dbRecordStats struct {
	AccessesNotInMemory       int64                     `bson:"accessesNotInMemory"`
	PageFaultExceptionsThrown int64                     `bson:"pageFaultExceptionsThrown"`
	DBRecordAccesses          map[string]recordAccesses `bson:",inline"`
}

// recordAccesses stores data related to memory operations scoped to a database.
type recordAccesses struct {
	AccessesNotInMemory       int64 `bson:"accessesNotInMemory"`
	PageFaultExceptionsThrown int64 `bson:"pageFaultExceptionsThrown"`
}

// memStats stores data related to memory statistics.
type memStats struct {
	Bits              int64       `bson:"bits"`
	Resident          int64       `bson:"resident"`
	Virtual           int64       `bson:"virtual"`
	Supported         interface{} `bson:"supported"`
	Mapped            int64       `bson:"mapped"`
	MappedWithJournal int64       `bson:"mappedWithJournal"`
}

// flushStats stores information about memory flushes.
type flushStats struct {
	Flushes      int64     `bson:"flushes"`
	TotalMs      int64     `bson:"total_ms"`
	AverageMs    float64   `bson:"average_ms"`
	LastMs       int64     `bson:"last_ms"`
	LastFinished time.Time `bson:"last_finished"`
}

// connectionStats stores information related to incoming database connections.
type connectionStats struct {
	Current      int64 `bson:"current"`
	Available    int64 `bson:"available"`
	TotalCreated int64 `bson:"totalCreated"`
}

// durTiming stores information related to journaling.
type durTiming struct {
	Dt               int64 `bson:"dt"`
	PrepLogBuffer    int64 `bson:"prepLogBuffer"`
	WriteToJournal   int64 `bson:"writeToJournal"`
	WriteToDataFiles int64 `bson:"writeToDataFiles"`
	RemapPrivateView int64 `bson:"remapPrivateView"`
}

// durStats stores information related to journaling statistics.
type durStats struct {
	Commits            float64 `bson:"commits"`
	JournaledMB        float64 `bson:"journaledMB"`
	WriteToDataFilesMB float64 `bson:"writeToDataFilesMB"`
	Compression        float64 `bson:"compression"`
	CommitsInWriteLock float64 `bson:"commitsInWriteLock"`
	EarlyCommits       float64 `bson:"earlyCommits"`
	TimeMs             durTiming
}

// queueStats stores the number of queued read/write operations.
type queueStats struct {
	Total   int64 `bson:"total"`
	Readers int64 `bson:"readers"`
	Writers int64 `bson:"writers"`
}

// clientStats stores the number of active read/write operations.
type clientStats struct {
	Total   int64 `bson:"total"`
	Readers int64 `bson:"readers"`
	Writers int64 `bson:"writers"`
}

// globalLockStats stores information related locks in the MMAP storage engine.
type globalLockStats struct {
	TotalTime     int64        `bson:"totalTime"`
	LockTime      int64        `bson:"lockTime"`
	CurrentQueue  *queueStats  `bson:"currentQueue"`
	ActiveClients *clientStats `bson:"activeClients"`
}

// networkStats stores information related to network traffic.
type networkStats struct {
	BytesIn     int64 `bson:"bytesIn"`
	BytesOut    int64 `bson:"bytesOut"`
	NumRequests int64 `bson:"numRequests"`
}

// opcountStats stores information related to commands and basic CRUD operations.
type opcountStats struct {
	Insert  int64 `bson:"insert"`
	Query   int64 `bson:"query"`
	Update  int64 `bson:"update"`
	Delete  int64 `bson:"delete"`
	GetMore int64 `bson:"getmore"`
	Command int64 `bson:"command"`
}

// opLatenciesStats stores information related to operation latencies for the database as a whole
type opLatenciesStats struct {
	Reads    *latencyStats `bson:"reads"`
	Writes   *latencyStats `bson:"writes"`
	Commands *latencyStats `bson:"commands"`
}

// latencyStats lists total latency in microseconds and count of operations, enabling you to obtain an average
type latencyStats struct {
	Latency int64 `bson:"latency"`
	Ops     int64 `bson:"ops"`
}

// metricsStats stores information related to metrics
type metricsStats struct {
	TTL           *ttlStats           `bson:"ttl"`
	Cursor        *cursorStats        `bson:"cursor"`
	Document      *documentStats      `bson:"document"`
	Commands      *commandsStats      `bson:"commands"`
	Operation     *operationStats     `bson:"operation"`
	QueryExecutor *queryExecutorStats `bson:"queryExecutor"`
	Repl          *replStats          `bson:"repl"`
	Storage       *storageStats       `bson:"storage"`
}

// ttlStats stores information related to documents with a ttl index.
type ttlStats struct {
	DeletedDocuments int64 `bson:"deletedDocuments"`
	Passes           int64 `bson:"passes"`
}

// cursorStats stores information related to cursor metrics.
type cursorStats struct {
	TimedOut int64            `bson:"timedOut"`
	Open     *openCursorStats `bson:"open"`
}

// documentStats stores information related to document metrics.
type documentStats struct {
	Deleted  int64 `bson:"deleted"`
	Inserted int64 `bson:"inserted"`
	Returned int64 `bson:"returned"`
	Updated  int64 `bson:"updated"`
}

// commandsStats stores information related to document metrics.
type commandsStats struct {
	Aggregate     *commandsStatsValue `bson:"aggregate"`
	Count         *commandsStatsValue `bson:"count"`
	Delete        *commandsStatsValue `bson:"delete"`
	Distinct      *commandsStatsValue `bson:"distinct"`
	Find          *commandsStatsValue `bson:"find"`
	FindAndModify *commandsStatsValue `bson:"findAndModify"`
	GetMore       *commandsStatsValue `bson:"getMore"`
	Insert        *commandsStatsValue `bson:"insert"`
	Update        *commandsStatsValue `bson:"update"`
}

type commandsStatsValue struct {
	Failed int64 `bson:"failed"`
	Total  int64 `bson:"total"`
}

// openCursorStats stores information related to open cursor metrics
type openCursorStats struct {
	NoTimeout int64 `bson:"noTimeout"`
	Pinned    int64 `bson:"pinned"`
	Total     int64 `bson:"total"`
}

// operationStats stores information related to query operations
// using special operation types
type operationStats struct {
	ScanAndOrder   int64 `bson:"scanAndOrder"`
	WriteConflicts int64 `bson:"writeConflicts"`
}

// queryExecutorStats stores information related to query execution
type queryExecutorStats struct {
	Scanned        int64 `bson:"scanned"`
	ScannedObjects int64 `bson:"scannedObjects"`
}

// replStats stores information related to replication process
type replStats struct {
	Apply    *replApplyStats    `bson:"apply"`
	Buffer   *replBufferStats   `bson:"buffer"`
	Executor *replExecutorStats `bson:"executor,omitempty"`
	Network  *replNetworkStats  `bson:"network"`
}

// replApplyStats stores information related to oplog application process
type replApplyStats struct {
	Batches *basicStats `bson:"batches"`
	Ops     int64       `bson:"ops"`
}

// replBufferStats stores information related to oplog buffer
type replBufferStats struct {
	Count     int64 `bson:"count"`
	SizeBytes int64 `bson:"sizeBytes"`
}

// replExecutorStats stores information related to replication executor
type replExecutorStats struct {
	Pool             map[string]int64 `bson:"pool"`
	Queues           map[string]int64 `bson:"queues"`
	UnsignaledEvents int64            `bson:"unsignaledEvents"`
}

// replNetworkStats stores information related to network usage by replication process
type replNetworkStats struct {
	Bytes    int64       `bson:"bytes"`
	GetMores *basicStats `bson:"getmores"`
	Ops      int64       `bson:"ops"`
}

// basicStats stores information about an operation
type basicStats struct {
	Num         int64 `bson:"num"`
	TotalMillis int64 `bson:"totalMillis"`
}

// readWriteLockTimes stores time spent holding read/write locks.
type readWriteLockTimes struct {
	Read       int64 `bson:"R"`
	Write      int64 `bson:"W"`
	ReadLower  int64 `bson:"r"`
	WriteLower int64 `bson:"w"`
}

// lockStats stores information related to time spent acquiring/holding locks for a given database.
type lockStats struct {
	TimeLockedMicros    readWriteLockTimes `bson:"timeLockedMicros"`
	TimeAcquiringMicros readWriteLockTimes `bson:"timeAcquiringMicros"`

	// AcquireCount and AcquireWaitCount are new fields of the lock stats only populated on 3.0 or newer.
	// Typed as a pointer so that if it is nil, mongostat can assume the field is not populated
	// with real namespace data.
	AcquireCount     *readWriteLockTimes `bson:"acquireCount,omitempty"`
	AcquireWaitCount *readWriteLockTimes `bson:"acquireWaitCount,omitempty"`
}

// extraInfo stores additional platform specific information.
type extraInfo struct {
	PageFaults *int64 `bson:"page_faults"`
}

// tcMallocStats stores information related to TCMalloc memory allocator metrics
type tcMallocStats struct {
	Generic  *genericTCMAllocStats  `bson:"generic"`
	TCMalloc *detailedTCMallocStats `bson:"tcmalloc"`
}

// genericTCMAllocStats stores generic TCMalloc memory allocator metrics
type genericTCMAllocStats struct {
	CurrentAllocatedBytes int64 `bson:"current_allocated_bytes"`
	HeapSize              int64 `bson:"heap_size"`
}

// detailedTCMallocStats stores detailed TCMalloc memory allocator metrics
type detailedTCMallocStats struct {
	PageheapFreeBytes            int64 `bson:"pageheap_free_bytes"`
	PageheapUnmappedBytes        int64 `bson:"pageheap_unmapped_bytes"`
	MaxTotalThreadCacheBytes     int64 `bson:"max_total_thread_cache_bytes"`
	CurrentTotalThreadCacheBytes int64 `bson:"current_total_thread_cache_bytes"`
	TotalFreeBytes               int64 `bson:"total_free_bytes"`
	CentralCacheFreeBytes        int64 `bson:"central_cache_free_bytes"`
	TransferCacheFreeBytes       int64 `bson:"transfer_cache_free_bytes"`
	ThreadCacheFreeBytes         int64 `bson:"thread_cache_free_bytes"`
	PageheapComittedBytes        int64 `bson:"pageheap_committed_bytes"`
	PageheapScavengeCount        int64 `bson:"pageheap_scavenge_count"`
	PageheapCommitCount          int64 `bson:"pageheap_commit_count"`
	PageheapTotalCommitBytes     int64 `bson:"pageheap_total_commit_bytes"`
	PageheapDecommitCount        int64 `bson:"pageheap_decommit_count"`
	PageheapTotalDecommitBytes   int64 `bson:"pageheap_total_decommit_bytes"`
	PageheapReserveCount         int64 `bson:"pageheap_reserve_count"`
	PageheapTotalReserveBytes    int64 `bson:"pageheap_total_reserve_bytes"`
	SpinLockTotalDelayNanos      int64 `bson:"spinlock_total_delay_ns"`
}

// storageStats stores information related to record allocations
type storageStats struct {
	FreelistSearchBucketExhausted int64 `bson:"freelist.search.bucketExhausted"`
	FreelistSearchRequests        int64 `bson:"freelist.search.requests"`
	FreelistSearchScanned         int64 `bson:"freelist.search.scanned"`
}

// lockUsage stores information related to a namespace's lock usage.
type lockUsage struct {
	Namespace string
	Reads     int64
	Writes    int64
}

type lockUsages []lockUsage

func percentageInt64(value, outOf int64) float64 {
	if value == 0 || outOf == 0 {
		return 0
	}
	return 100 * (float64(value) / float64(outOf))
}

func averageInt64(value, outOf int64) int64 {
	if value == 0 || outOf == 0 {
		return 0
	}
	return value / outOf
}

func (slice lockUsages) Len() int {
	return len(slice)
}

func (slice lockUsages) Less(i, j int) bool {
	return slice[i].Reads+slice[i].Writes < slice[j].Reads+slice[j].Writes
}

func (slice lockUsages) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

// collectionLockStatus stores a collection's lock statistics.
type collectionLockStatus struct {
	ReadAcquireWaitsPercentage  float64
	WriteAcquireWaitsPercentage float64
	ReadAcquireTimeMicros       int64
	WriteAcquireTimeMicros      int64
}

// lockStatus stores a database's lock statistics.
type lockStatus struct {
	DBName     string
	Percentage float64
	Global     bool
}

// statLine is a wrapper for all metrics reported by mongostat for monitored hosts.
type statLine struct {
	Key string
	// What storage engine is being used for the node with this stat line
	StorageEngine string

	Error    error
	IsMongos bool
	Host     string
	Version  string

	UptimeNanos int64

	// The time at which this statLine was generated.
	Time time.Time

	// The last time at which this statLine was printed to output.
	LastPrinted time.Time

	// Opcounter fields
	Insert, InsertCnt   int64
	Query, QueryCnt     int64
	Update, UpdateCnt   int64
	Delete, DeleteCnt   int64
	GetMore, GetMoreCnt int64
	Command, CommandCnt int64

	// Asserts fields
	Regular   int64
	Warning   int64
	Msg       int64
	User      int64
	Rollovers int64

	// OpLatency fields
	WriteOpsCnt    int64
	WriteLatency   int64
	ReadOpsCnt     int64
	ReadLatency    int64
	CommandOpsCnt  int64
	CommandLatency int64

	// TTL fields
	Passes, PassesCnt                     int64
	DeletedDocuments, DeletedDocumentsCnt int64

	// Cursor fields
	TimedOutC, TimedOutCCnt   int64
	NoTimeoutC, NoTimeoutCCnt int64
	PinnedC, PinnedCCnt       int64
	TotalC, TotalCCnt         int64

	// Document fields
	DeletedD, InsertedD, ReturnedD, UpdatedD int64

	// Commands fields
	AggregateCommandTotal, AggregateCommandFailed         int64
	CountCommandTotal, CountCommandFailed                 int64
	DeleteCommandTotal, DeleteCommandFailed               int64
	DistinctCommandTotal, DistinctCommandFailed           int64
	FindCommandTotal, FindCommandFailed                   int64
	FindAndModifyCommandTotal, FindAndModifyCommandFailed int64
	GetMoreCommandTotal, GetMoreCommandFailed             int64
	InsertCommandTotal, InsertCommandFailed               int64
	UpdateCommandTotal, UpdateCommandFailed               int64

	// Operation fields
	ScanAndOrderOp, WriteConflictsOp int64

	// Query Executor fields
	TotalKeysScanned, TotalObjectsScanned int64

	// Connection fields
	CurrentC, AvailableC, TotalCreatedC int64

	// Collection locks (3.0 mmap only)
	CollectionLocks *collectionLockStatus

	// Cache utilization (wiredtiger only)
	CacheDirtyPercent float64
	CacheUsedPercent  float64

	// Cache utilization extended (wiredtiger only)
	TrackedDirtyBytes         int64
	CurrentCachedBytes        int64
	MaxBytesConfigured        int64
	AppThreadsPageReadCount   int64
	AppThreadsPageReadTime    int64
	AppThreadsPageWriteCount  int64
	BytesWrittenFrom          int64
	BytesReadInto             int64
	PagesEvictedByAppThread   int64
	PagesQueuedForEviction    int64
	PagesReadIntoCache        int64
	PagesWrittenFromCache     int64
	PagesRequestedFromCache   int64
	ServerEvictingPages       int64
	WorkerThreadEvictingPages int64
	InternalPagesEvicted      int64
	ModifiedPagesEvicted      int64
	UnmodifiedPagesEvicted    int64

	// Connection statistics (wiredtiger only)
	FilesCurrentlyOpen int64

	// Data handles statistics (wiredtiger only)
	DataHandlesCurrentlyActive int64

	// Replicated Opcounter fields
	InsertR, InsertRCnt                      int64
	QueryR, QueryRCnt                        int64
	UpdateR, UpdateRCnt                      int64
	DeleteR, DeleteRCnt                      int64
	GetMoreR, GetMoreRCnt                    int64
	CommandR, CommandRCnt                    int64
	ReplLag                                  int64
	OplogStats                               *oplogStats
	Flushes, FlushesCnt                      int64
	FlushesTotalTime                         int64
	Mapped, Virtual, Resident, NonMapped     int64
	Faults, FaultsCnt                        int64
	HighestLocked                            *lockStatus
	QueuedReaders, QueuedWriters             int64
	ActiveReaders, ActiveWriters             int64
	AvailableReaders, AvailableWriters       int64
	TotalTicketsReaders, TotalTicketsWriters int64
	NetIn, NetInCnt                          int64
	NetOut, NetOutCnt                        int64
	NumConnections                           int64
	ReplSetName                              string
	ReplHealthAvg                            float64
	NodeType                                 string
	NodeState                                string
	NodeStateInt                             int64
	NodeHealthInt                            int64

	// Replicated Metrics fields
	ReplNetworkBytes                    int64
	ReplNetworkGetmoresNum              int64
	ReplNetworkGetmoresTotalMillis      int64
	ReplNetworkOps                      int64
	ReplBufferCount                     int64
	ReplBufferSizeBytes                 int64
	ReplApplyBatchesNum                 int64
	ReplApplyBatchesTotalMillis         int64
	ReplApplyOps                        int64
	ReplExecutorPoolInProgressCount     int64
	ReplExecutorQueuesNetworkInProgress int64
	ReplExecutorQueuesSleepers          int64
	ReplExecutorUnsignaledEvents        int64

	// Cluster fields
	JumboChunksCount int64

	// DB stats field
	DbStatsLines []dbStatLine

	// Col Stats field
	ColStatsLines []colStatLine

	// Shard stats
	TotalInUse, TotalAvailable, TotalCreated, TotalRefreshing int64

	// Shard Hosts stats field
	ShardHostStatsLines map[string]shardHostStatLine

	TopStatLines []topStatLine

	// TCMalloc stats field
	TCMallocCurrentAllocatedBytes        int64
	TCMallocHeapSize                     int64
	TCMallocCentralCacheFreeBytes        int64
	TCMallocCurrentTotalThreadCacheBytes int64
	TCMallocMaxTotalThreadCacheBytes     int64
	TCMallocTotalFreeBytes               int64
	TCMallocTransferCacheFreeBytes       int64
	TCMallocThreadCacheFreeBytes         int64
	TCMallocSpinLockTotalDelayNanos      int64
	TCMallocPageheapFreeBytes            int64
	TCMallocPageheapUnmappedBytes        int64
	TCMallocPageheapComittedBytes        int64
	TCMallocPageheapScavengeCount        int64
	TCMallocPageheapCommitCount          int64
	TCMallocPageheapTotalCommitBytes     int64
	TCMallocPageheapDecommitCount        int64
	TCMallocPageheapTotalDecommitBytes   int64
	TCMallocPageheapReserveCount         int64
	TCMallocPageheapTotalReserveBytes    int64

	// Storage stats field
	StorageFreelistSearchBucketExhausted int64
	StorageFreelistSearchRequests        int64
	StorageFreelistSearchScanned         int64
}

type dbStatLine struct {
	Name        string
	Collections int64
	Objects     int64
	AvgObjSize  float64
	DataSize    int64
	StorageSize int64
	NumExtents  int64
	Indexes     int64
	IndexSize   int64
	Ok          int64
	FsUsedSize  int64
	FsTotalSize int64
}
type colStatLine struct {
	Name           string
	DbName         string
	Count          int64
	Size           int64
	AvgObjSize     float64
	StorageSize    int64
	TotalIndexSize int64
	Ok             int64
}

type shardHostStatLine struct {
	InUse      int64
	Available  int64
	Created    int64
	Refreshing int64
}

type topStatLine struct {
	CollectionName                string
	TotalTime, TotalCount         int64
	ReadLockTime, ReadLockCount   int64
	WriteLockTime, WriteLockCount int64
	QueriesTime, QueriesCount     int64
	GetMoreTime, GetMoreCount     int64
	InsertTime, InsertCount       int64
	UpdateTime, UpdateCount       int64
	RemoveTime, RemoveCount       int64
	CommandsTime, CommandsCount   int64
}

func parseLocks(stat serverStatus) map[string]lockUsage {
	returnVal := make(map[string]lockUsage, len(stat.Locks))
	for namespace, lockInfo := range stat.Locks {
		returnVal[namespace] = lockUsage{
			namespace,
			lockInfo.TimeLockedMicros.Read + lockInfo.TimeLockedMicros.ReadLower,
			lockInfo.TimeLockedMicros.Write + lockInfo.TimeLockedMicros.WriteLower,
		}
	}
	return returnVal
}

func computeLockDiffs(prevLocks, curLocks map[string]lockUsage) []lockUsage {
	lockUsages := lockUsages(make([]lockUsage, 0, len(curLocks)))
	for namespace, curUsage := range curLocks {
		prevUsage, hasKey := prevLocks[namespace]
		if !hasKey {
			// This namespace didn't appear in the previous batch of lock info,
			// so we can't compute a diff for it - skip it.
			continue
		}
		// Calculate diff of lock usage for this namespace and add to the list
		lockUsages = append(lockUsages,
			lockUsage{
				namespace,
				curUsage.Reads - prevUsage.Reads,
				curUsage.Writes - prevUsage.Writes,
			})
	}
	// Sort the array in order of least to most locked
	sort.Sort(lockUsages)
	return lockUsages
}

func diff(newVal, oldVal, sampleTime int64) (avg, newValue int64) {
	d := newVal - oldVal
	if d < 0 {
		d = newVal
	}
	return d / sampleTime, newVal
}

// newStatLine constructs a statLine object from two mongoStatus objects.
func newStatLine(oldMongo, newMongo mongoStatus, key string, sampleSecs int64) *statLine {
	oldStat := *oldMongo.ServerStatus
	newStat := *newMongo.ServerStatus

	returnVal := &statLine{
		Key:       key,
		Host:      newStat.Host,
		Version:   newStat.Version,
		Mapped:    -1,
		Virtual:   -1,
		Resident:  -1,
		NonMapped: -1,
		Faults:    -1,
	}

	returnVal.UptimeNanos = 1000 * 1000 * newStat.UptimeMillis

	// set connection info
	returnVal.CurrentC = newStat.Connections.Current
	returnVal.AvailableC = newStat.Connections.Available
	returnVal.TotalCreatedC = newStat.Connections.TotalCreated

	// set the storage engine appropriately
	if newStat.StorageEngine != nil && newStat.StorageEngine.Name != "" {
		returnVal.StorageEngine = newStat.StorageEngine.Name
	} else {
		returnVal.StorageEngine = "mmapv1"
	}

	if newStat.Opcounters != nil && oldStat.Opcounters != nil {
		returnVal.Insert, returnVal.InsertCnt = diff(newStat.Opcounters.Insert, oldStat.Opcounters.Insert, sampleSecs)
		returnVal.Query, returnVal.QueryCnt = diff(newStat.Opcounters.Query, oldStat.Opcounters.Query, sampleSecs)
		returnVal.Update, returnVal.UpdateCnt = diff(newStat.Opcounters.Update, oldStat.Opcounters.Update, sampleSecs)
		returnVal.Delete, returnVal.DeleteCnt = diff(newStat.Opcounters.Delete, oldStat.Opcounters.Delete, sampleSecs)
		returnVal.GetMore, returnVal.GetMoreCnt = diff(newStat.Opcounters.GetMore, oldStat.Opcounters.GetMore, sampleSecs)
		returnVal.Command, returnVal.CommandCnt = diff(newStat.Opcounters.Command, oldStat.Opcounters.Command, sampleSecs)
	}

	if newStat.OpLatencies != nil {
		if newStat.OpLatencies.Reads != nil {
			returnVal.ReadOpsCnt = newStat.OpLatencies.Reads.Ops
			returnVal.ReadLatency = newStat.OpLatencies.Reads.Latency
		}
		if newStat.OpLatencies.Writes != nil {
			returnVal.WriteOpsCnt = newStat.OpLatencies.Writes.Ops
			returnVal.WriteLatency = newStat.OpLatencies.Writes.Latency
		}
		if newStat.OpLatencies.Commands != nil {
			returnVal.CommandOpsCnt = newStat.OpLatencies.Commands.Ops
			returnVal.CommandLatency = newStat.OpLatencies.Commands.Latency
		}
	}

	if newStat.Asserts != nil {
		returnVal.Regular = newStat.Asserts.Regular
		returnVal.Warning = newStat.Asserts.Warning
		returnVal.Msg = newStat.Asserts.Msg
		returnVal.User = newStat.Asserts.User
		returnVal.Rollovers = newStat.Asserts.Rollovers
	}

	if newStat.TCMallocStats != nil {
		if newStat.TCMallocStats.Generic != nil {
			returnVal.TCMallocCurrentAllocatedBytes = newStat.TCMallocStats.Generic.CurrentAllocatedBytes
			returnVal.TCMallocHeapSize = newStat.TCMallocStats.Generic.HeapSize
		}
		if newStat.TCMallocStats.TCMalloc != nil {
			returnVal.TCMallocCentralCacheFreeBytes = newStat.TCMallocStats.TCMalloc.CentralCacheFreeBytes
			returnVal.TCMallocCurrentTotalThreadCacheBytes = newStat.TCMallocStats.TCMalloc.CurrentTotalThreadCacheBytes
			returnVal.TCMallocMaxTotalThreadCacheBytes = newStat.TCMallocStats.TCMalloc.MaxTotalThreadCacheBytes
			returnVal.TCMallocTransferCacheFreeBytes = newStat.TCMallocStats.TCMalloc.TransferCacheFreeBytes
			returnVal.TCMallocThreadCacheFreeBytes = newStat.TCMallocStats.TCMalloc.ThreadCacheFreeBytes
			returnVal.TCMallocTotalFreeBytes = newStat.TCMallocStats.TCMalloc.TotalFreeBytes
			returnVal.TCMallocSpinLockTotalDelayNanos = newStat.TCMallocStats.TCMalloc.SpinLockTotalDelayNanos

			returnVal.TCMallocPageheapFreeBytes = newStat.TCMallocStats.TCMalloc.PageheapFreeBytes
			returnVal.TCMallocPageheapUnmappedBytes = newStat.TCMallocStats.TCMalloc.PageheapUnmappedBytes
			returnVal.TCMallocPageheapComittedBytes = newStat.TCMallocStats.TCMalloc.PageheapComittedBytes
			returnVal.TCMallocPageheapScavengeCount = newStat.TCMallocStats.TCMalloc.PageheapScavengeCount
			returnVal.TCMallocPageheapCommitCount = newStat.TCMallocStats.TCMalloc.PageheapCommitCount
			returnVal.TCMallocPageheapTotalCommitBytes = newStat.TCMallocStats.TCMalloc.PageheapTotalCommitBytes
			returnVal.TCMallocPageheapDecommitCount = newStat.TCMallocStats.TCMalloc.PageheapDecommitCount
			returnVal.TCMallocPageheapTotalDecommitBytes = newStat.TCMallocStats.TCMalloc.PageheapTotalDecommitBytes
			returnVal.TCMallocPageheapReserveCount = newStat.TCMallocStats.TCMalloc.PageheapReserveCount
			returnVal.TCMallocPageheapTotalReserveBytes = newStat.TCMallocStats.TCMalloc.PageheapTotalReserveBytes
		}
	}

	if newStat.Metrics != nil && oldStat.Metrics != nil {
		if newStat.Metrics.TTL != nil && oldStat.Metrics.TTL != nil {
			returnVal.Passes, returnVal.PassesCnt = diff(newStat.Metrics.TTL.Passes, oldStat.Metrics.TTL.Passes, sampleSecs)
			returnVal.DeletedDocuments, returnVal.DeletedDocumentsCnt = diff(
				newStat.Metrics.TTL.DeletedDocuments,
				oldStat.Metrics.TTL.DeletedDocuments,
				sampleSecs,
			)
		}
		if newStat.Metrics.Cursor != nil && oldStat.Metrics.Cursor != nil {
			returnVal.TimedOutC, returnVal.TimedOutCCnt = diff(newStat.Metrics.Cursor.TimedOut, oldStat.Metrics.Cursor.TimedOut, sampleSecs)
			if newStat.Metrics.Cursor.Open != nil && oldStat.Metrics.Cursor.Open != nil {
				returnVal.NoTimeoutC, returnVal.NoTimeoutCCnt = diff(newStat.Metrics.Cursor.Open.NoTimeout, oldStat.Metrics.Cursor.Open.NoTimeout, sampleSecs)
				returnVal.PinnedC, returnVal.PinnedCCnt = diff(newStat.Metrics.Cursor.Open.Pinned, oldStat.Metrics.Cursor.Open.Pinned, sampleSecs)
				returnVal.TotalC, returnVal.TotalCCnt = diff(newStat.Metrics.Cursor.Open.Total, oldStat.Metrics.Cursor.Open.Total, sampleSecs)
			}
		}
		if newStat.Metrics.Document != nil {
			returnVal.DeletedD = newStat.Metrics.Document.Deleted
			returnVal.InsertedD = newStat.Metrics.Document.Inserted
			returnVal.ReturnedD = newStat.Metrics.Document.Returned
			returnVal.UpdatedD = newStat.Metrics.Document.Updated
		}

		if newStat.Metrics.Commands != nil {
			if newStat.Metrics.Commands.Aggregate != nil {
				returnVal.AggregateCommandTotal = newStat.Metrics.Commands.Aggregate.Total
				returnVal.AggregateCommandFailed = newStat.Metrics.Commands.Aggregate.Failed
			}
			if newStat.Metrics.Commands.Count != nil {
				returnVal.CountCommandTotal = newStat.Metrics.Commands.Count.Total
				returnVal.CountCommandFailed = newStat.Metrics.Commands.Count.Failed
			}
			if newStat.Metrics.Commands.Delete != nil {
				returnVal.DeleteCommandTotal = newStat.Metrics.Commands.Delete.Total
				returnVal.DeleteCommandFailed = newStat.Metrics.Commands.Delete.Failed
			}
			if newStat.Metrics.Commands.Distinct != nil {
				returnVal.DistinctCommandTotal = newStat.Metrics.Commands.Distinct.Total
				returnVal.DistinctCommandFailed = newStat.Metrics.Commands.Distinct.Failed
			}
			if newStat.Metrics.Commands.Find != nil {
				returnVal.FindCommandTotal = newStat.Metrics.Commands.Find.Total
				returnVal.FindCommandFailed = newStat.Metrics.Commands.Find.Failed
			}
			if newStat.Metrics.Commands.FindAndModify != nil {
				returnVal.FindAndModifyCommandTotal = newStat.Metrics.Commands.FindAndModify.Total
				returnVal.FindAndModifyCommandFailed = newStat.Metrics.Commands.FindAndModify.Failed
			}
			if newStat.Metrics.Commands.GetMore != nil {
				returnVal.GetMoreCommandTotal = newStat.Metrics.Commands.GetMore.Total
				returnVal.GetMoreCommandFailed = newStat.Metrics.Commands.GetMore.Failed
			}
			if newStat.Metrics.Commands.Insert != nil {
				returnVal.InsertCommandTotal = newStat.Metrics.Commands.Insert.Total
				returnVal.InsertCommandFailed = newStat.Metrics.Commands.Insert.Failed
			}
			if newStat.Metrics.Commands.Update != nil {
				returnVal.UpdateCommandTotal = newStat.Metrics.Commands.Update.Total
				returnVal.UpdateCommandFailed = newStat.Metrics.Commands.Update.Failed
			}
		}

		if newStat.Metrics.Operation != nil {
			returnVal.ScanAndOrderOp = newStat.Metrics.Operation.ScanAndOrder
			returnVal.WriteConflictsOp = newStat.Metrics.Operation.WriteConflicts
		}

		if newStat.Metrics.QueryExecutor != nil {
			returnVal.TotalKeysScanned = newStat.Metrics.QueryExecutor.Scanned
			returnVal.TotalObjectsScanned = newStat.Metrics.QueryExecutor.ScannedObjects
		}

		if newStat.Metrics.Repl != nil {
			if newStat.Metrics.Repl.Apply != nil {
				returnVal.ReplApplyBatchesNum = newStat.Metrics.Repl.Apply.Batches.Num
				returnVal.ReplApplyBatchesTotalMillis = newStat.Metrics.Repl.Apply.Batches.TotalMillis
				returnVal.ReplApplyOps = newStat.Metrics.Repl.Apply.Ops
			}
			if newStat.Metrics.Repl.Buffer != nil {
				returnVal.ReplBufferCount = newStat.Metrics.Repl.Buffer.Count
				returnVal.ReplBufferSizeBytes = newStat.Metrics.Repl.Buffer.SizeBytes
			}
			if newStat.Metrics.Repl.Executor != nil {
				returnVal.ReplExecutorPoolInProgressCount = newStat.Metrics.Repl.Executor.Pool["inProgressCount"]
				returnVal.ReplExecutorQueuesNetworkInProgress = newStat.Metrics.Repl.Executor.Queues["networkInProgress"]
				returnVal.ReplExecutorQueuesSleepers = newStat.Metrics.Repl.Executor.Queues["sleepers"]
				returnVal.ReplExecutorUnsignaledEvents = newStat.Metrics.Repl.Executor.UnsignaledEvents
			}
			if newStat.Metrics.Repl.Network != nil {
				returnVal.ReplNetworkBytes = newStat.Metrics.Repl.Network.Bytes
				if newStat.Metrics.Repl.Network.GetMores != nil {
					returnVal.ReplNetworkGetmoresNum = newStat.Metrics.Repl.Network.GetMores.Num
					returnVal.ReplNetworkGetmoresTotalMillis = newStat.Metrics.Repl.Network.GetMores.TotalMillis
				}
				returnVal.ReplNetworkOps = newStat.Metrics.Repl.Network.Ops
			}
		}

		if newStat.Metrics.Storage != nil {
			returnVal.StorageFreelistSearchBucketExhausted = newStat.Metrics.Storage.FreelistSearchBucketExhausted
			returnVal.StorageFreelistSearchRequests = newStat.Metrics.Storage.FreelistSearchRequests
			returnVal.StorageFreelistSearchScanned = newStat.Metrics.Storage.FreelistSearchScanned
		}
	}

	if newStat.OpcountersRepl != nil && oldStat.OpcountersRepl != nil {
		returnVal.InsertR, returnVal.InsertRCnt = diff(newStat.OpcountersRepl.Insert, oldStat.OpcountersRepl.Insert, sampleSecs)
		returnVal.QueryR, returnVal.QueryRCnt = diff(newStat.OpcountersRepl.Query, oldStat.OpcountersRepl.Query, sampleSecs)
		returnVal.UpdateR, returnVal.UpdateRCnt = diff(newStat.OpcountersRepl.Update, oldStat.OpcountersRepl.Update, sampleSecs)
		returnVal.DeleteR, returnVal.DeleteRCnt = diff(newStat.OpcountersRepl.Delete, oldStat.OpcountersRepl.Delete, sampleSecs)
		returnVal.GetMoreR, returnVal.GetMoreRCnt = diff(newStat.OpcountersRepl.GetMore, oldStat.OpcountersRepl.GetMore, sampleSecs)
		returnVal.CommandR, returnVal.CommandRCnt = diff(newStat.OpcountersRepl.Command, oldStat.OpcountersRepl.Command, sampleSecs)
	}

	returnVal.CacheDirtyPercent = -1
	returnVal.CacheUsedPercent = -1
	if newStat.WiredTiger != nil {
		returnVal.CacheDirtyPercent = float64(newStat.WiredTiger.Cache.TrackedDirtyBytes) / float64(newStat.WiredTiger.Cache.MaxBytesConfigured)
		returnVal.CacheUsedPercent = float64(newStat.WiredTiger.Cache.CurrentCachedBytes) / float64(newStat.WiredTiger.Cache.MaxBytesConfigured)

		returnVal.TrackedDirtyBytes = newStat.WiredTiger.Cache.TrackedDirtyBytes
		returnVal.CurrentCachedBytes = newStat.WiredTiger.Cache.CurrentCachedBytes
		returnVal.MaxBytesConfigured = newStat.WiredTiger.Cache.MaxBytesConfigured
		returnVal.AppThreadsPageReadCount = newStat.WiredTiger.Cache.AppThreadsPageReadCount
		returnVal.AppThreadsPageReadTime = newStat.WiredTiger.Cache.AppThreadsPageReadTime
		returnVal.AppThreadsPageWriteCount = newStat.WiredTiger.Cache.AppThreadsPageWriteCount
		returnVal.BytesWrittenFrom = newStat.WiredTiger.Cache.BytesWrittenFrom
		returnVal.BytesReadInto = newStat.WiredTiger.Cache.BytesReadInto
		returnVal.PagesEvictedByAppThread = newStat.WiredTiger.Cache.PagesEvictedByAppThread
		returnVal.PagesQueuedForEviction = newStat.WiredTiger.Cache.PagesQueuedForEviction
		returnVal.PagesReadIntoCache = newStat.WiredTiger.Cache.PagesReadIntoCache
		returnVal.PagesWrittenFromCache = newStat.WiredTiger.Cache.PagesWrittenFromCache
		returnVal.PagesRequestedFromCache = newStat.WiredTiger.Cache.PagesRequestedFromCache
		returnVal.ServerEvictingPages = newStat.WiredTiger.Cache.ServerEvictingPages
		returnVal.WorkerThreadEvictingPages = newStat.WiredTiger.Cache.WorkerThreadEvictingPages

		returnVal.InternalPagesEvicted = newStat.WiredTiger.Cache.InternalPagesEvicted
		returnVal.ModifiedPagesEvicted = newStat.WiredTiger.Cache.ModifiedPagesEvicted
		returnVal.UnmodifiedPagesEvicted = newStat.WiredTiger.Cache.UnmodifiedPagesEvicted

		returnVal.FlushesTotalTime = newStat.WiredTiger.Transaction.TransCheckpointsTotalTimeMsecs * int64(time.Millisecond)

		returnVal.FilesCurrentlyOpen = newStat.WiredTiger.Connection.FilesCurrentlyOpen

		returnVal.DataHandlesCurrentlyActive = newStat.WiredTiger.DataHandle.DataHandlesCurrentlyActive
	}
	if newStat.WiredTiger != nil && oldStat.WiredTiger != nil {
		returnVal.Flushes, returnVal.FlushesCnt = diff(
			newStat.WiredTiger.Transaction.TransCheckpoints,
			oldStat.WiredTiger.Transaction.TransCheckpoints,
			sampleSecs,
		)
	} else if newStat.BackgroundFlushing != nil && oldStat.BackgroundFlushing != nil {
		returnVal.Flushes, returnVal.FlushesCnt = diff(newStat.BackgroundFlushing.Flushes, oldStat.BackgroundFlushing.Flushes, sampleSecs)
	}

	returnVal.Time = newMongo.SampleTime
	returnVal.IsMongos =
		newStat.ShardCursorType != nil || strings.HasPrefix(newStat.Process, mongosProcess)

	// BEGIN code modification
	if oldStat.Mem.Supported.(bool) {
		// END code modification
		if !returnVal.IsMongos {
			returnVal.Mapped = newStat.Mem.Mapped
		}
		returnVal.Virtual = newStat.Mem.Virtual
		returnVal.Resident = newStat.Mem.Resident

		if !returnVal.IsMongos {
			returnVal.NonMapped = newStat.Mem.Virtual - newStat.Mem.Mapped
		}
	}

	if newStat.Repl != nil {
		returnVal.ReplSetName = newStat.Repl.SetName
		// BEGIN code modification
		if val, ok := newStat.Repl.IsMaster.(bool); ok && val {
			returnVal.NodeType = "PRI"
		} else if val, ok := newStat.Repl.IsWritablePrimary.(bool); ok && val {
			returnVal.NodeType = "PRI"
		} else if val, ok := newStat.Repl.Secondary.(bool); ok && val {
			returnVal.NodeType = "SEC"
		} else if val, ok := newStat.Repl.ArbiterOnly.(bool); ok && val {
			returnVal.NodeType = "ARB"
		} else {
			returnVal.NodeType = "UNK"
		} // END code modification
	} else if returnVal.IsMongos {
		returnVal.NodeType = "RTR"
	}

	if oldStat.ExtraInfo != nil && newStat.ExtraInfo != nil &&
		oldStat.ExtraInfo.PageFaults != nil && newStat.ExtraInfo.PageFaults != nil {
		returnVal.Faults, returnVal.FaultsCnt = diff(*(newStat.ExtraInfo.PageFaults), *(oldStat.ExtraInfo.PageFaults), sampleSecs)
	}
	if !returnVal.IsMongos && oldStat.Locks != nil && newStat.Locks != nil {
		globalCheckOld, hasGlobalOld := oldStat.Locks["Global"]
		globalCheckNew, hasGlobalNew := newStat.Locks["Global"]
		if hasGlobalOld && globalCheckOld.AcquireCount != nil && hasGlobalNew && globalCheckNew.AcquireCount != nil {
			// This appears to be a 3.0+ server so the data in these fields do *not* refer to
			// actual namespaces and thus we can't compute lock %.
			returnVal.HighestLocked = nil

			// Check if it's a 3.0+ MMAP server so we can still compute collection locks
			collectionCheckOld, hasCollectionOld := oldStat.Locks["Collection"]
			collectionCheckNew, hasCollectionNew := newStat.Locks["Collection"]
			if hasCollectionOld && collectionCheckOld.AcquireWaitCount != nil && hasCollectionNew && collectionCheckNew.AcquireWaitCount != nil {
				readWaitCountDiff := newStat.Locks["Collection"].AcquireWaitCount.Read - oldStat.Locks["Collection"].AcquireWaitCount.Read
				readTotalCountDiff := newStat.Locks["Collection"].AcquireCount.Read - oldStat.Locks["Collection"].AcquireCount.Read
				writeWaitCountDiff := newStat.Locks["Collection"].AcquireWaitCount.Write - oldStat.Locks["Collection"].AcquireWaitCount.Write
				writeTotalCountDiff := newStat.Locks["Collection"].AcquireCount.Write - oldStat.Locks["Collection"].AcquireCount.Write
				readAcquireTimeDiff := newStat.Locks["Collection"].TimeAcquiringMicros.Read - oldStat.Locks["Collection"].TimeAcquiringMicros.Read
				writeAcquireTimeDiff := newStat.Locks["Collection"].TimeAcquiringMicros.Write - oldStat.Locks["Collection"].TimeAcquiringMicros.Write
				returnVal.CollectionLocks = &collectionLockStatus{
					ReadAcquireWaitsPercentage:  percentageInt64(readWaitCountDiff, readTotalCountDiff),
					WriteAcquireWaitsPercentage: percentageInt64(writeWaitCountDiff, writeTotalCountDiff),
					ReadAcquireTimeMicros:       averageInt64(readAcquireTimeDiff, readWaitCountDiff),
					WriteAcquireTimeMicros:      averageInt64(writeAcquireTimeDiff, writeWaitCountDiff),
				}
			}
		} else {
			prevLocks := parseLocks(oldStat)
			curLocks := parseLocks(newStat)
			lockdiffs := computeLockDiffs(prevLocks, curLocks)
			if len(lockdiffs) == 0 {
				if newStat.GlobalLock != nil {
					returnVal.HighestLocked = &lockStatus{
						DBName:     "",
						Percentage: percentageInt64(newStat.GlobalLock.LockTime, newStat.GlobalLock.TotalTime),
						Global:     true,
					}
				}
			} else {
				// Get the entry with the highest lock
				highestLocked := lockdiffs[len(lockdiffs)-1]

				timeDiffMillis := newStat.UptimeMillis - oldStat.UptimeMillis
				lockToReport := highestLocked.Writes

				// if the highest locked namespace is not '.'
				if highestLocked.Namespace != "." {
					for _, namespaceLockInfo := range lockdiffs {
						if namespaceLockInfo.Namespace == "." {
							lockToReport += namespaceLockInfo.Writes
						}
					}
				}

				// lock data is in microseconds and uptime is in milliseconds - so
				// divide by 1000 so that they units match
				lockToReport /= 1000

				returnVal.HighestLocked = &lockStatus{
					DBName:     highestLocked.Namespace,
					Percentage: percentageInt64(lockToReport, timeDiffMillis),
					Global:     false,
				}
			}
		}
	} else {
		returnVal.HighestLocked = nil
	}

	if newStat.GlobalLock != nil {
		hasWT := newStat.WiredTiger != nil && oldStat.WiredTiger != nil
		// If we have wiredtiger stats, use those instead
		if newStat.GlobalLock.CurrentQueue != nil {
			if hasWT {
				returnVal.QueuedReaders = newStat.GlobalLock.CurrentQueue.Readers + newStat.GlobalLock.ActiveClients.Readers -
					newStat.WiredTiger.Concurrent.Read.Out
				returnVal.QueuedWriters = newStat.GlobalLock.CurrentQueue.Writers + newStat.GlobalLock.ActiveClients.Writers -
					newStat.WiredTiger.Concurrent.Write.Out
				if returnVal.QueuedReaders < 0 {
					returnVal.QueuedReaders = 0
				}
				if returnVal.QueuedWriters < 0 {
					returnVal.QueuedWriters = 0
				}
			} else {
				returnVal.QueuedReaders = newStat.GlobalLock.CurrentQueue.Readers
				returnVal.QueuedWriters = newStat.GlobalLock.CurrentQueue.Writers
			}
		}

		if hasWT {
			returnVal.ActiveReaders = newStat.WiredTiger.Concurrent.Read.Out
			returnVal.ActiveWriters = newStat.WiredTiger.Concurrent.Write.Out
			returnVal.AvailableReaders = newStat.WiredTiger.Concurrent.Read.Available
			returnVal.AvailableWriters = newStat.WiredTiger.Concurrent.Write.Available
			returnVal.TotalTicketsReaders = newStat.WiredTiger.Concurrent.Read.TotalTickets
			returnVal.TotalTicketsWriters = newStat.WiredTiger.Concurrent.Write.TotalTickets
		} else if newStat.GlobalLock.ActiveClients != nil {
			returnVal.ActiveReaders = newStat.GlobalLock.ActiveClients.Readers
			returnVal.ActiveWriters = newStat.GlobalLock.ActiveClients.Writers
		}
	}

	if oldStat.Network != nil && newStat.Network != nil {
		returnVal.NetIn, returnVal.NetInCnt = diff(newStat.Network.BytesIn, oldStat.Network.BytesIn, sampleSecs)
		returnVal.NetOut, returnVal.NetOutCnt = diff(newStat.Network.BytesOut, oldStat.Network.BytesOut, sampleSecs)
	}

	if newStat.Connections != nil {
		returnVal.NumConnections = newStat.Connections.Current
	}

	if newMongo.ReplSetStatus != nil {
		newReplStat := *newMongo.ReplSetStatus

		if newReplStat.Members != nil {
			myName := newStat.Repl.Me
			// Find the master and myself
			master := replSetMember{}
			me := replSetMember{}
			for _, member := range newReplStat.Members {
				if member.Name == myName {
					// Store my state string
					returnVal.NodeState = member.StateStr
					// Store my state integer
					returnVal.NodeStateInt = member.State
					// Store my health integer
					returnVal.NodeHealthInt = member.Health

					if member.State == 1 {
						// I'm the master
						returnVal.ReplLag = 0
						break
					}

					// I'm secondary
					me = member
				} else if member.State == 1 {
					// Master found
					master = member
				}
			}

			if me.State == 2 {
				// OptimeDate.Unix() type is int64
				lag := master.OptimeDate.Unix() - me.OptimeDate.Unix()
				if lag < 0 {
					returnVal.ReplLag = 0
				} else {
					returnVal.ReplLag = lag
				}
			}

			// Preparations for the average health state of the replica-set
			replMemberCount := len(newReplStat.Members)
			replMemberHealthyCount := 0

			// Second for-loop is needed, because of break-construct above
			for _, member := range newReplStat.Members {
				// Count only healthy members for the average health state of the replica-set
				if member.Health == 1 {
					replMemberHealthyCount++
				}
			}

			// Calculate the average health state of the replica-set (For precise monitoring alerts)
			// To detect if a member is unhealthy from the perspective of another member and also how bad the replica-set health is
			if replMemberCount > 0 {
				returnVal.ReplHealthAvg = float64(replMemberHealthyCount) / float64(replMemberCount)
			} else {
				returnVal.ReplHealthAvg = 0.00
			}
		}
	}

	if newMongo.ClusterStatus != nil {
		newClusterStat := *newMongo.ClusterStatus
		returnVal.JumboChunksCount = newClusterStat.JumboChunksCount
	}

	if newMongo.OplogStats != nil {
		returnVal.OplogStats = newMongo.OplogStats
	}

	if newMongo.DbStats != nil {
		newDbStats := *newMongo.DbStats
		for _, db := range newDbStats.Dbs {
			dbStatsData := db.DbStatsData
			// mongos doesn't have the db key, so setting the db name
			if dbStatsData.Db == "" {
				dbStatsData.Db = db.Name
			}
			dbStatLine := &dbStatLine{
				Name:        dbStatsData.Db,
				Collections: dbStatsData.Collections,
				Objects:     dbStatsData.Objects,
				AvgObjSize:  dbStatsData.AvgObjSize,
				DataSize:    dbStatsData.DataSize,
				StorageSize: dbStatsData.StorageSize,
				NumExtents:  dbStatsData.NumExtents,
				Indexes:     dbStatsData.Indexes,
				IndexSize:   dbStatsData.IndexSize,
				Ok:          dbStatsData.Ok,
				FsTotalSize: dbStatsData.FsTotalSize,
				FsUsedSize:  dbStatsData.FsUsedSize,
			}
			returnVal.DbStatsLines = append(returnVal.DbStatsLines, *dbStatLine)
		}
	}

	if newMongo.ColStats != nil {
		for _, col := range newMongo.ColStats.Collections {
			colStatsData := col.ColStatsData
			// mongos doesn't have the db key, so setting the db name
			if colStatsData.Collection == "" {
				colStatsData.Collection = col.Name
			}
			colStatLine := &colStatLine{
				Name:           colStatsData.Collection,
				DbName:         col.DbName,
				Count:          colStatsData.Count,
				Size:           colStatsData.Size,
				AvgObjSize:     colStatsData.AvgObjSize,
				StorageSize:    colStatsData.StorageSize,
				TotalIndexSize: colStatsData.TotalIndexSize,
				Ok:             colStatsData.Ok,
			}
			returnVal.ColStatsLines = append(returnVal.ColStatsLines, *colStatLine)
		}
	}

	// Set shard stats
	if newMongo.ShardStats != nil {
		newShardStats := *newMongo.ShardStats
		returnVal.TotalInUse = newShardStats.TotalInUse
		returnVal.TotalAvailable = newShardStats.TotalAvailable
		returnVal.TotalCreated = newShardStats.TotalCreated
		returnVal.TotalRefreshing = newShardStats.TotalRefreshing
		returnVal.ShardHostStatsLines = make(map[string]shardHostStatLine, len(newShardStats.Hosts))
		for host, stats := range newShardStats.Hosts {
			shardStatLine := &shardHostStatLine{
				InUse:      stats.InUse,
				Available:  stats.Available,
				Created:    stats.Created,
				Refreshing: stats.Refreshing,
			}

			returnVal.ShardHostStatsLines[host] = *shardStatLine
		}
	}

	if newMongo.TopStats != nil {
		for collection, data := range newMongo.TopStats.Totals {
			topStatDataLine := &topStatLine{
				CollectionName: collection,
				TotalTime:      data.Total.Time,
				TotalCount:     data.Total.Count,
				ReadLockTime:   data.ReadLock.Time,
				ReadLockCount:  data.ReadLock.Count,
				WriteLockTime:  data.WriteLock.Time,
				WriteLockCount: data.WriteLock.Count,
				QueriesTime:    data.Queries.Time,
				QueriesCount:   data.Queries.Count,
				GetMoreTime:    data.GetMore.Time,
				GetMoreCount:   data.GetMore.Count,
				InsertTime:     data.Insert.Time,
				InsertCount:    data.Insert.Count,
				UpdateTime:     data.Update.Time,
				UpdateCount:    data.Update.Count,
				RemoveTime:     data.Remove.Time,
				RemoveCount:    data.Remove.Count,
				CommandsTime:   data.Commands.Time,
				CommandsCount:  data.Commands.Count,
			}
			returnVal.TopStatLines = append(returnVal.TopStatLines, *topStatDataLine)
		}
	}

	return returnVal
}
