// +build linux
// +build 386 amd64 arm arm64

package sql

// Register sqlite sql driver. This requires cgo. Telegraf's build
// automation relies on cross compiling from linux and cgo doesn't
// work well when cross compiling for different operating systems, so
// this driver is limited to linux for now.
import (
	_ "modernc.org/sqlite" // register sqlite driver
)
