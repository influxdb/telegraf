package kinesis_consumer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/influxdata/telegraf"
)

var errNotFound = errors.New("no iterator found")

type iterator struct {
	stream   string
	shard    string
	seqnr    string
	modified bool
}

type store struct {
	app      string
	table    string
	interval time.Duration
	log      telegraf.Logger

	client    *dynamodb.Client
	iterators map[string]iterator

	wg     sync.WaitGroup
	cancel context.CancelFunc

	sync.Mutex
}

func newStore(app, table string, interval time.Duration, log telegraf.Logger) *store {
	s := &store{
		app:      app,
		table:    table,
		interval: interval,
		log:      log,
	}

	// Initialize the iterator states
	s.iterators = make(map[string]iterator)

	return s
}

func (s *store) run(ctx context.Context) error {
	rctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	// Create a client to connect to DynamoDB
	cfg, err := config.LoadDefaultConfig(rctx)
	if err != nil {
		return fmt.Errorf("loading default config failed: %w", err)
	}
	s.client = dynamodb.NewFromConfig(cfg)

	// Start the go-routine that pushes the states out to DynamoDB on a
	// regular interval
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-rctx.Done():
				return
			case <-ticker.C:
				s.write(rctx)
			}
		}
	}()

	return nil
}

func (s *store) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), s.interval)
	defer cancel()
	s.write(ctx)

	s.cancel()
	s.wg.Wait()
}

func (s *store) write(ctx context.Context) {
	s.Lock()
	defer s.Unlock()

	for k, iter := range s.iterators {
		// Only write iterators modified since the last write
		if !iter.modified {
			continue
		}

		if _, err := s.client.PutItem(
			ctx,
			&dynamodb.PutItemInput{
				TableName: aws.String(s.table),
				Item: map[string]types.AttributeValue{
					"namespace":       &types.AttributeValueMemberS{Value: s.app + "-" + iter.stream},
					"shard_id":        &types.AttributeValueMemberS{Value: iter.shard},
					"sequence_number": &types.AttributeValueMemberS{Value: iter.seqnr},
				},
			}); err != nil {
			s.log.Errorf("storing iterator %s-%s/%s/%s failed: %v", s.app, iter.stream, iter.shard, iter.seqnr, err)
		}

		// Mark state as saved
		iter.modified = false
		s.iterators[k] = iter
	}
}

func (s *store) set(stream, shard, seqnr string) {
	s.Lock()
	defer s.Unlock()

	s.iterators[stream+"/"+shard] = iterator{
		stream:   stream,
		shard:    shard,
		seqnr:    seqnr,
		modified: true,
	}
}

func (s *store) get(ctx context.Context, stream, shard string) (string, error) {
	s.Lock()
	defer s.Unlock()

	// Return the cached result if possible
	if iter, found := s.iterators[stream+"/"+shard]; found {
		return iter.seqnr, nil
	}

	// Retrieve the information from the database
	resp, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.table),
		ConsistentRead: aws.Bool(true),
		Key: map[string]types.AttributeValue{
			"namespace": &types.AttributeValueMemberS{Value: s.app + "-" + stream},
			"shard_id":  &types.AttributeValueMemberS{Value: shard},
		},
	})
	if err != nil {
		return "", err
	}

	// Extract the sequence number
	raw, found := resp.Item["sequence_number"]
	if !found {
		return "", fmt.Errorf("%w for %s-%s/%s", errNotFound, s.app, stream, shard)
	}
	seqnr, ok := raw.(*types.AttributeValueMemberS)
	if !ok {
		return "", fmt.Errorf("sequence number for %s-%s/%s is of unexpected type %T", s.app, stream, shard, raw)
	}

	// Fill the cache
	s.iterators[stream+"/"+shard] = iterator{
		stream: stream,
		shard:  shard,
		seqnr:  seqnr.Value,
	}

	return seqnr.Value, nil
}
