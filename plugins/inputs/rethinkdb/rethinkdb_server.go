package rethinkdb

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/gorethink/gorethink.v3"

	"github.com/influxdata/telegraf"
)

type server struct {
	url          *url.URL
	session      *gorethink.Session
	serverStatus serverStatus
}

func (s *server) gatherData(acc telegraf.Accumulator) error {
	if err := s.getServerStatus(); err != nil {
		return fmt.Errorf("failed to get server_status: %w", err)
	}

	if err := s.validateVersion(); err != nil {
		return fmt.Errorf("failed version validation: %w", err)
	}

	if err := s.addClusterStats(acc); err != nil {
		return fmt.Errorf("error adding cluster stats: %w", err)
	}

	if err := s.addMemberStats(acc); err != nil {
		return fmt.Errorf("error adding member stats: %w", err)
	}

	if err := s.addTablesStats(acc); err != nil {
		return fmt.Errorf("error adding table stats: %w", err)
	}

	return nil
}

func (s *server) validateVersion() error {
	if s.serverStatus.Process.Version == "" {
		return errors.New("could not determine the RethinkDB server version: process.version key missing")
	}

	versionRegexp := regexp.MustCompile(`\d.\d.\d`)
	versionString := versionRegexp.FindString(s.serverStatus.Process.Version)
	if versionString == "" {
		return fmt.Errorf("could not determine the RethinkDB server version: malformed version string (%v)", s.serverStatus.Process.Version)
	}

	majorVersion, err := strconv.Atoi(strings.Split(versionString, "")[0])
	if err != nil || majorVersion < 2 {
		return fmt.Errorf("unsupported major version %s", versionString)
	}
	return nil
}

func (s *server) getServerStatus() error {
	cursor, err := gorethink.DB("rethinkdb").Table("server_status").Run(s.session)
	if err != nil {
		return err
	}

	if cursor.IsNil() {
		return errors.New("could not determine the RethinkDB server version: no rows returned from the server_status table")
	}
	defer cursor.Close()
	var serverStatuses []serverStatus
	err = cursor.All(&serverStatuses)
	if err != nil {
		return errors.New("could not parse server_status results")
	}
	host, port, err := net.SplitHostPort(s.url.Host)
	if err != nil {
		return fmt.Errorf("unable to determine provided hostname from %s", s.url.Host)
	}
	driverPort, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("unable to parse port from %s: %w", port, err)
	}
	for _, ss := range serverStatuses {
		for _, address := range ss.Network.Addresses {
			if address.Host == host && ss.Network.DriverPort == driverPort {
				s.serverStatus = ss
				return nil
			}
		}
	}

	return fmt.Errorf("unable to determine host id from server_status with %s", s.url.Host)
}

func (s *server) getDefaultTags() map[string]string {
	tags := make(map[string]string)
	tags["rethinkdb_host"] = s.url.Host
	tags["rethinkdb_hostname"] = s.serverStatus.Network.Hostname
	return tags
}

var clusterTracking = []string{
	"active_clients",
	"clients",
	"queries_per_sec",
	"read_docs_per_sec",
	"written_docs_per_sec",
}

func (s *server) addClusterStats(acc telegraf.Accumulator) error {
	cursor, err := gorethink.DB("rethinkdb").Table("stats").Get([]string{"cluster"}).Run(s.session)
	if err != nil {
		return fmt.Errorf("cluster stats query error: %w", err)
	}
	defer cursor.Close()
	var clusterStats stats
	if err := cursor.One(&clusterStats); err != nil {
		return fmt.Errorf("failure to parse cluster stats: %w", err)
	}

	tags := s.getDefaultTags()
	tags["type"] = "cluster"
	clusterStats.Engine.addEngineStats(clusterTracking, acc, tags)
	return nil
}

var memberTracking = []string{
	"active_clients",
	"clients",
	"queries_per_sec",
	"total_queries",
	"read_docs_per_sec",
	"total_reads",
	"written_docs_per_sec",
	"total_writes",
}

func (s *server) addMemberStats(acc telegraf.Accumulator) error {
	cursor, err := gorethink.DB("rethinkdb").Table("stats").Get([]string{"server", s.serverStatus.ID}).Run(s.session)
	if err != nil {
		return fmt.Errorf("member stats query error: %w", err)
	}
	defer cursor.Close()
	var memberStats stats
	if err := cursor.One(&memberStats); err != nil {
		return fmt.Errorf("failure to parse member stats: %w", err)
	}

	tags := s.getDefaultTags()
	tags["type"] = "member"
	memberStats.Engine.addEngineStats(memberTracking, acc, tags)
	return nil
}

var tableTracking = []string{
	"read_docs_per_sec",
	"total_reads",
	"written_docs_per_sec",
	"total_writes",
}

func (s *server) addTablesStats(acc telegraf.Accumulator) error {
	tablesCursor, err := gorethink.DB("rethinkdb").Table("table_status").Run(s.session)
	if err != nil {
		return fmt.Errorf("table stats query error: %w", err)
	}

	defer tablesCursor.Close()
	var tables []tableStatus
	err = tablesCursor.All(&tables)
	if err != nil {
		return errors.New("could not parse table_status results")
	}
	for _, table := range tables {
		err = s.addTableStats(acc, table)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *server) addTableStats(acc telegraf.Accumulator, table tableStatus) error {
	cursor, err := gorethink.DB("rethinkdb").Table("stats").
		Get([]string{"table_server", table.ID, s.serverStatus.ID}).
		Run(s.session)
	if err != nil {
		return fmt.Errorf("table stats query error: %w", err)
	}
	defer cursor.Close()

	var ts tableStats
	if err := cursor.One(&ts); err != nil {
		return fmt.Errorf("failure to parse table stats: %w", err)
	}

	tags := s.getDefaultTags()
	tags["type"] = "data"
	tags["ns"] = fmt.Sprintf("%s.%s", table.DB, table.Name)
	ts.Engine.addEngineStats(tableTracking, acc, tags)
	ts.Storage.addStats(acc, tags)

	return nil
}
