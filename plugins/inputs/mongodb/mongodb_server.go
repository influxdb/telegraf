package mongodb

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/influxdata/telegraf"
)

type server struct {
	client     *mongo.Client
	hostname   string
	lastResult *mongoStatus

	log telegraf.Logger
}

type oplogEntry struct {
	Timestamp primitive.Timestamp `bson:"ts"`
}

func isAuthorization(err error) bool {
	return strings.Contains(err.Error(), "not authorized")
}

func (s *server) getDefaultTags() map[string]string {
	tags := make(map[string]string)
	tags["hostname"] = s.hostname
	return tags
}

func (s *server) ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	return s.client.Ping(ctx, nil)
}

func (s *server) authLog(err error) {
	if isAuthorization(err) {
		s.log.Debug(err.Error())
	} else {
		s.log.Error(err.Error())
	}
}

func (s *server) runCommand(database string, cmd, result interface{}) error {
	r := s.client.Database(database).RunCommand(context.Background(), cmd)
	if r.Err() != nil {
		return r.Err()
	}
	return r.Decode(result)
}

func (s *server) gatherServerStatus() (*serverStatus, error) {
	serverStatus := &serverStatus{}
	err := s.runCommand("admin", bson.D{
		{
			Key:   "serverStatus",
			Value: 1,
		},
		{
			Key:   "recordStats",
			Value: 0,
		},
	}, serverStatus)
	if err != nil {
		return nil, err
	}
	return serverStatus, nil
}

func (s *server) gatherReplSetStatus() (*replSetStatus, error) {
	replSetStatus := &replSetStatus{}
	err := s.runCommand("admin", bson.D{
		{
			Key:   "replSetGetStatus",
			Value: 1,
		},
	}, replSetStatus)
	if err != nil {
		return nil, err
	}
	return replSetStatus, nil
}

func (s *server) gatherTopStatData() (*topStats, error) {
	var dest map[string]interface{}
	err := s.runCommand("admin", bson.D{
		{
			Key:   "top",
			Value: 1,
		},
	}, &dest)
	if err != nil {
		return nil, fmt.Errorf("failed running admin cmd: %w", err)
	}

	totals, ok := dest["totals"].(map[string]interface{})
	if !ok {
		return nil, errors.New("collection totals not found or not a map")
	}
	delete(totals, "note")

	recorded, err := bson.Marshal(totals)
	if err != nil {
		return nil, errors.New("unable to marshal totals")
	}

	topInfo := make(map[string]topStatCollection)
	if err := bson.Unmarshal(recorded, &topInfo); err != nil {
		return nil, fmt.Errorf("failed unmarshalling records: %w", err)
	}

	return &topStats{Totals: topInfo}, nil
}

func (s *server) gatherClusterStatus() (*clusterStatus, error) {
	chunkCount, err := s.client.Database("config").Collection("chunks").CountDocuments(context.Background(), bson.M{"jumbo": true})
	if err != nil {
		return nil, err
	}

	return &clusterStatus{
		JumboChunksCount: chunkCount,
	}, nil
}

func poolStatsCommand(version string) (string, error) {
	majorPart := string(version[0])
	major, err := strconv.ParseInt(majorPart, 10, 64)
	if err != nil {
		return "", err
	}

	if major >= 5 {
		return "connPoolStats", nil
	}
	return "shardConnPoolStats", nil
}

func (s *server) gatherShardConnPoolStats(version string) (*shardStats, error) {
	command, err := poolStatsCommand(version)
	if err != nil {
		return nil, err
	}

	shardStats := &shardStats{}
	err = s.runCommand("admin", bson.D{
		{
			Key:   command,
			Value: 1,
		},
	}, &shardStats)
	if err != nil {
		return nil, err
	}
	return shardStats, nil
}

func (s *server) gatherDBStats(name string) (*db, error) {
	stats := &dbStatsData{}
	err := s.runCommand(name, bson.D{
		{
			Key:   "dbStats",
			Value: 1,
		},
	}, stats)
	if err != nil {
		return nil, err
	}

	return &db{
		Name:        name,
		DbStatsData: stats,
	}, nil
}

func (s *server) getOplogReplLag(collection string) (*oplogStats, error) {
	query := bson.M{"ts": bson.M{"$exists": true}}

	var first oplogEntry
	firstResult := s.client.Database("local").Collection(collection).FindOne(context.Background(), query, options.FindOne().SetSort(bson.M{"$natural": 1}))
	if firstResult.Err() != nil {
		return nil, firstResult.Err()
	}
	if err := firstResult.Decode(&first); err != nil {
		return nil, err
	}

	var last oplogEntry
	lastResult := s.client.Database("local").Collection(collection).FindOne(context.Background(), query, options.FindOne().SetSort(bson.M{"$natural": -1}))
	if lastResult.Err() != nil {
		return nil, lastResult.Err()
	}
	if err := lastResult.Decode(&last); err != nil {
		return nil, err
	}

	firstTime := time.Unix(int64(first.Timestamp.T), 0)
	lastTime := time.Unix(int64(last.Timestamp.T), 0)
	stats := &oplogStats{
		TimeDiff: int64(lastTime.Sub(firstTime).Seconds()),
	}
	return stats, nil
}

// The "oplog.rs" collection is stored on all replica set members.
//
// The "oplog.$main" collection is created on the master node of a
// master-slave replicated deployment.  As of MongoDB 3.2, master-slave
// replication has been deprecated.
func (s *server) gatherOplogStats() (*oplogStats, error) {
	stats, err := s.getOplogReplLag("oplog.rs")
	if err == nil {
		return stats, nil
	}

	return s.getOplogReplLag("oplog.$main")
}

func (s *server) gatherCollectionStats(colStatsDbs []string) (*colStats, error) {
	names, err := s.client.ListDatabaseNames(context.Background(), bson.D{})
	if err != nil {
		return nil, err
	}

	results := &colStats{}
	for _, dbName := range names {
		if slices.Contains(colStatsDbs, dbName) || len(colStatsDbs) == 0 {
			// skip views as they fail on collStats below
			filter := bson.M{"type": bson.M{"$in": bson.A{"collection", "timeseries"}}}

			var colls []string
			colls, err = s.client.Database(dbName).ListCollectionNames(context.Background(), filter)
			if err != nil {
				s.log.Errorf("Error getting collection names: %s", err.Error())
				continue
			}
			for _, colName := range colls {
				colStatLine := &colStatsData{}
				err = s.runCommand(dbName, bson.D{
					{
						Key:   "collStats",
						Value: colName,
					},
				}, colStatLine)
				if err != nil {
					s.authLog(fmt.Errorf("error getting col stats from %q: %w", colName, err))
					continue
				}
				collection := &collection{
					Name:         colName,
					DbName:       dbName,
					ColStatsData: colStatLine,
				}
				results.Collections = append(results.Collections, *collection)
			}
		}
	}
	return results, nil
}

func (s *server) gatherData(acc telegraf.Accumulator, gatherClusterStatus, gatherDbStats, gatherColStats, gatherTopStat bool, colStatsDbs []string) error {
	serverStatus, err := s.gatherServerStatus()
	if err != nil {
		return err
	}

	// Get replica set status, an error indicates that the server is not a
	// member of a replica set.
	replSetStatus, err := s.gatherReplSetStatus()
	if err != nil {
		s.log.Debugf("Unable to gather replica set status: %s", err.Error())
	}

	// Gather the oplog if we are a member of a replica set.  Non-replica set
	// members do not have the oplog collections.
	var oplogStats *oplogStats
	if replSetStatus != nil {
		oplogStats, err = s.gatherOplogStats()
		if err != nil {
			s.authLog(fmt.Errorf("unable to get oplog stats: %w", err))
		}
	}

	var clusterStatus *clusterStatus
	if gatherClusterStatus {
		status, err := s.gatherClusterStatus()
		if err != nil {
			s.log.Debugf("Unable to gather cluster status: %s", err.Error())
		}
		clusterStatus = status
	}

	shardStats, err := s.gatherShardConnPoolStats(serverStatus.Version)
	if err != nil {
		s.authLog(fmt.Errorf("unable to gather shard connection pool stats: %w", err))
	}

	var collectionStats *colStats
	if gatherColStats {
		stats, err := s.gatherCollectionStats(colStatsDbs)
		if err != nil {
			return err
		}
		collectionStats = stats
	}

	dbStats := &dbStats{}
	if gatherDbStats {
		names, err := s.client.ListDatabaseNames(context.Background(), bson.D{})
		if err != nil {
			return err
		}

		for _, name := range names {
			db, err := s.gatherDBStats(name)
			if err != nil {
				s.log.Errorf("Error getting db stats from %q: %v", name, err)
				continue
			}
			dbStats.Dbs = append(dbStats.Dbs, *db)
		}
	}

	topStatData := &topStats{}
	if gatherTopStat {
		topStats, err := s.gatherTopStatData()
		if err != nil {
			s.log.Debugf("Unable to gather top stat data: %s", err.Error())
			return err
		}
		topStatData = topStats
	}

	result := &mongoStatus{
		ServerStatus:  serverStatus,
		ReplSetStatus: replSetStatus,
		ClusterStatus: clusterStatus,
		DbStats:       dbStats,
		ColStats:      collectionStats,
		ShardStats:    shardStats,
		OplogStats:    oplogStats,
		TopStats:      topStatData,
	}

	result.SampleTime = time.Now()
	if s.lastResult != nil && result != nil {
		duration := result.SampleTime.Sub(s.lastResult.SampleTime)
		durationInSeconds := int64(duration.Seconds())
		if durationInSeconds == 0 {
			durationInSeconds = 1
		}
		data := newMongodbData(
			newStatLine(*s.lastResult, *result, s.hostname, durationInSeconds),
			s.getDefaultTags(),
		)
		data.addDefaultStats()
		data.addDbStats()
		data.addColStats()
		data.addShardHostStats()
		data.addTopStats()
		data.flush(acc)
	}

	s.lastResult = result
	return nil
}
