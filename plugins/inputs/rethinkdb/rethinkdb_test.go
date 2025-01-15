//go:build integration

package rethinkdb

import (
	"log"
	"math/rand"
	"net/url"
	"os"
	"testing"
	"time"

	"gopkg.in/gorethink/gorethink.v3"
)

var connect_url, authKey, username, password string
var server *server

func init() {
	connect_url = os.Getenv("RETHINKDB_URL")
	if connect_url == "" {
		connect_url = "127.0.0.1:28015"
	}
	authKey = os.Getenv("RETHINKDB_AUTHKEY")
	username = os.Getenv("RETHINKDB_USERNAME")
	password = os.Getenv("RETHINKDB_PASSWORD")
}

func testSetup(m *testing.M) {
	var err error
	server = &server{url: &url.URL{Host: connect_url}}

	if authKey {
		server.session, _ = gorethink.Connect(gorethink.ConnectOpts{
			Address:          server.url.Host,
			AuthKey:          authKey,
			HandshakeVersion: gorethink.HandshakeV0_4,
			DiscoverHosts:    false,
		})
	} else {
		server.session, _ = gorethink.Connect(gorethink.ConnectOpts{
			Address:          server.url.Host,
			Username:         username,
			Password:         password,
			HandshakeVersion: gorethink.HandshakeV1_0,
			DiscoverHosts:    false,
		})
	}

	if err != nil {
		log.Fatalln(err.Error())
	}

	err = server.getServerStatus()
	if err != nil {
		log.Fatalln(err.Error())
	}
}

func testTeardown(m *testing.M) {
	server.session.Close()
}

func TestMain(m *testing.M) {
	// seed randomness for use with tests
	rand.Seed(time.Now().UTC().UnixNano())

	testSetup(m)
	res := m.Run()
	testTeardown(m)

	os.Exit(res)
}
