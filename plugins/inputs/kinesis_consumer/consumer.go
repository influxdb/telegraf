package kinesis_consumer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/influxdata/telegraf"
)

type recordHandler func(ctx context.Context, shard string, r *types.Record)

type shardConsumer struct {
	seqnr    string
	interval time.Duration
	log      telegraf.Logger

	client *kinesis.Client
	params *kinesis.GetShardIteratorInput

	onMessage recordHandler
}

func (c *shardConsumer) consume(ctx context.Context, shard string) ([]types.ChildShard, error) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Get the first shard iterator
	iter, err := c.iterator(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting first shard iterator failed: %w", err)
	}

	for {
		// Get new records from the shard
		resp, err := c.client.GetRecords(ctx, &kinesis.GetRecordsInput{
			ShardIterator: iter,
		})
		if err != nil {
			// Handle recoverable errors
			var throughputErr *types.ProvisionedThroughputExceededException
			var expiredIterErr *types.ExpiredIteratorException
			switch {
			case errors.As(err, &throughputErr):
				// Wait a second before trying again as suggested by
				// https://docs.aws.amazon.com/streams/latest/dev/service-sizes-and-limits.html
				c.log.Tracef("throughput exceeded when getting records for shard %s...", shard)
				time.Sleep(time.Second)
				continue
			case errors.As(err, &expiredIterErr):
				c.log.Tracef("iterator expired for shard %s...", shard)
				if iter, err = c.iterator(ctx); err != nil {
					return nil, fmt.Errorf("getting shard iterator failed: %w", err)
				}
				continue
			case errors.Is(err, context.Canceled):
				return nil, nil
			default:
				c.log.Tracef("get-records error is of type %T", err)
				return nil, fmt.Errorf("getting records failed: %w", err)
			}
		}
		c.log.Tracef("read %d records for shard %s...", len(resp.Records), shard)

		// Check if we fully read the shard
		if resp.NextShardIterator == nil {
			return resp.ChildShards, nil
		}
		iter = resp.NextShardIterator

		// Process the records and keep track of the last sequence number
		// consumed for recreating the iterator.
		for _, r := range resp.Records {
			c.onMessage(ctx, shard, &r)
			c.seqnr = *r.SequenceNumber
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, nil
			}
		}

		// Wait for the poll interval to pass or cancel
		select {
		case <-ctx.Done():
			return nil, nil
		case <-ticker.C:
			continue
		}
	}
}

func (c *shardConsumer) iterator(ctx context.Context) (*string, error) {
	for {
		resp, err := c.client.GetShardIterator(ctx, c.params)
		if err != nil {
			var throughputErr *types.ProvisionedThroughputExceededException
			if errors.As(err, &throughputErr) {
				// We called the function too often and should wait a bit
				// until trying again
				c.log.Tracef("throughput exceeded when getting iterator for shard %s...", *c.params.ShardId)
				time.Sleep(time.Second)
				continue
			}

			return nil, err
		}
		c.log.Tracef("successfully updated iterator for shard %s (%s)...", *c.params.ShardId, c.seqnr)
		return resp.ShardIterator, nil
	}
}

type consumer struct {
	config              aws.Config
	stream              string
	iterType            types.ShardIteratorType
	pollInterval        time.Duration
	shardUpdateInterval time.Duration
	log                 telegraf.Logger

	onMessage recordHandler
	position  func(shard string) string

	client *kinesis.Client

	shardsConsumed map[string]bool
	shardConsumers map[string]*shardConsumer

	wg sync.WaitGroup

	sync.Mutex
}

func (c *consumer) init() error {
	if c.stream == "" {
		return errors.New("stream cannot be empty")
	}
	if c.pollInterval <= 0 {
		return errors.New("invalid poll interval")
	}

	if c.onMessage == nil {
		return errors.New("message handler is undefined")
	}

	c.shardsConsumed = make(map[string]bool)
	c.shardConsumers = make(map[string]*shardConsumer)

	return nil
}

func (c *consumer) start(ctx context.Context) {
	// Setup the client
	c.client = kinesis.NewFromConfig(c.config)

	// Do the initial discovery of shards
	if err := c.updateShardConsumers(ctx); err != nil {
		c.log.Errorf("Initializing shards failed: %v", err)
	}

	// If the consumer has a shard-update interval, use a ticker to update
	// available shards on a regular basis
	if c.shardUpdateInterval <= 0 {
		return
	}
	ticker := time.NewTicker(c.shardUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.updateShardConsumers(ctx); err != nil {
				c.log.Errorf("Updating shards failed: %v", err)
			}
		}
	}
}

func (c *consumer) updateShardConsumers(ctx context.Context) error {
	// List all shards of the given stream
	var availableShards []types.Shard
	req := &kinesis.ListShardsInput{StreamName: aws.String(c.stream)}
	for {
		resp, err := c.client.ListShards(ctx, req)
		if err != nil {
			return fmt.Errorf("listing shards failed: %w", err)
		}
		availableShards = append(availableShards, resp.Shards...)

		if resp.NextToken == nil {
			break
		}

		req = &kinesis.ListShardsInput{NextToken: resp.NextToken}
	}
	c.log.Tracef("got %d shards during update", len(availableShards))

	// All following operations need to be locked to create a consistent
	// state of the shards and consumers
	c.Lock()
	defer c.Unlock()

	// Filter out all shards actively consumed already
	inactiveShards := make([]types.Shard, 0, len(availableShards))
	for _, shard := range availableShards {
		id := *shard.ShardId
		if _, found := c.shardConsumers[id]; found {
			c.log.Tracef("shard %s is actively consumed...", id)
			continue
		}
		c.log.Tracef("shard %s is not actively consumed...", id)
		inactiveShards = append(inactiveShards, shard)
	}

	// Fill the shards already consumed and get the positions if the consumer
	// is backed by an iterator store
	newShards := make([]types.Shard, 0, len(inactiveShards))
	seqnrs := make(map[string]string, len(inactiveShards))
	for _, shard := range inactiveShards {
		id := *shard.ShardId

		if c.shardsConsumed[id] {
			c.log.Tracef("shard %s is already fully consumed...", id)
			continue
		}
		c.log.Tracef("shard %s is not fully consumed...", id)

		// Retrieve the shard position from the store
		if c.position != nil {
			seqnr := c.position(id)
			if seqnr == "" {
				// A truely new shard
				newShards = append(newShards, shard)
				c.log.Tracef("shard %s is new...", id)
				continue
			}
			seqnrs[id] = seqnr

			// Check if we already fully consumed for closed shards
			end := shard.SequenceNumberRange.EndingSequenceNumber
			if end != nil && *end == seqnr {
				c.log.Tracef("shard %s is closed and already fully consumed...", id)
				c.shardsConsumed[id] = true
				continue
			}
			c.log.Tracef("shard %s is not yet fully consumed...", id)
		}

		// The shard is not fully consumed yet so save the sequence number
		// and the shard as "new".
		newShards = append(newShards, shard)
	}

	// Filter all shards already fully consumed and create a new consumer for
	// every remaining new shard respecting resharding artifacts
	for _, shard := range newShards {
		id := *shard.ShardId

		// Handle resharding by making sure all parents are consumed already
		// before starting a consumer on a child shard. If parents are not
		// consumed fully we ignore this shard here as it will be reported
		// by the call to `GetRecords` as a child later.
		if shard.ParentShardId != nil && *shard.ParentShardId != "" {
			pid := *shard.ParentShardId
			if !c.shardsConsumed[pid] {
				c.log.Tracef("shard %s has parent %s which is not fully consumed yet...", id, pid)
				continue
			}
		}
		if shard.AdjacentParentShardId != nil && *shard.AdjacentParentShardId != "" {
			pid := *shard.AdjacentParentShardId
			if !c.shardsConsumed[pid] {
				c.log.Tracef("shard %s has adjacent parent %s which is not fully consumed yet...", id, pid)
				continue
			}
		}

		// Create a new consumer and start it
		c.wg.Add(1)
		go func(shardID string) {
			defer c.wg.Done()
			c.startShardConsumer(ctx, shardID, seqnrs[shardID])
		}(id)
	}

	return nil
}

func (c *consumer) startShardConsumer(ctx context.Context, id, seqnr string) {
	c.log.Tracef("starting consumer for shard %s at sequence number %q...", id, seqnr)
	sc := &shardConsumer{
		seqnr:     seqnr,
		interval:  c.pollInterval,
		log:       c.log,
		onMessage: c.onMessage,
		client:    c.client,
		params: &kinesis.GetShardIteratorInput{
			ShardId:           &id,
			ShardIteratorType: c.iterType,
			StreamName:        &c.stream,
		},
	}
	if seqnr != "" {
		sc.params.ShardIteratorType = types.ShardIteratorTypeAfterSequenceNumber
		sc.params.StartingSequenceNumber = &seqnr
	}
	c.shardConsumers[id] = sc

	childs, err := sc.consume(ctx, id)
	if err != nil {
		c.log.Errorf("Consuming shard %s failed: %v", id, err)
		return
	}
	c.log.Tracef("finished consuming shard %s", id)

	c.Lock()
	defer c.Unlock()

	c.shardsConsumed[id] = true
	delete(c.shardConsumers, id)

	for _, shard := range childs {
		cid := *shard.ShardId

		startable := true
		for _, pid := range shard.ParentShards {
			startable = startable && c.shardsConsumed[pid]
		}
		if !startable {
			c.log.Tracef("child shard %s of shard %s is not startable as parents are fully consumed yet...", cid, id)
			continue
		}
		c.log.Tracef("child shard %s of shard %s is startable...", cid, id)

		var cseqnr string
		if c.position != nil {
			cseqnr = c.position(cid)
		}
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.startShardConsumer(ctx, cid, cseqnr)
		}()
	}
}

func (c *consumer) stop() {
	c.wg.Wait()
}
