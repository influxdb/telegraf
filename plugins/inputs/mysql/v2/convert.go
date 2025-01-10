package v2

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

type conversionFunc func(value sql.RawBytes) (interface{}, error)

func ParseInt(value sql.RawBytes) (interface{}, error) {
	v, err := strconv.ParseInt(string(value), 10, 64)

	// Ignore ErrRange.  When this error is set the returned value is "the
	// maximum magnitude integer of the appropriate bitSize and sign."
	var numErr *strconv.NumError
	if errors.As(err, &numErr) && errors.Is(numErr, strconv.ErrRange) {
		return v, nil
	}

	return v, err
}

func ParseUint(value sql.RawBytes) (interface{}, error) {
	return strconv.ParseUint(string(value), 10, 64)
}

func ParseFloat(value sql.RawBytes) (interface{}, error) {
	return strconv.ParseFloat(string(value), 64)
}

func ParseBoolAsInteger(value sql.RawBytes) (interface{}, error) {
	if bytes.EqualFold(value, []byte("YES")) || bytes.EqualFold(value, []byte("ON")) {
		return int64(1), nil
	}

	return int64(0), nil
}

func ParseString(value sql.RawBytes) (interface{}, error) {
	return string(value), nil
}

func ParseGTIDMode(value sql.RawBytes) (interface{}, error) {
	// https://dev.mysql.com/doc/refman/8.0/en/replication-mode-change-online-concepts.html
	v := string(value)
	switch v {
	case "OFF":
		return int64(0), nil
	case "ON":
		return int64(1), nil
	case "OFF_PERMISSIVE":
		return int64(0), nil
	case "ON_PERMISSIVE":
		return int64(1), nil
	default:
		return nil, fmt.Errorf("unrecognized gtid_mode: %q", v)
	}
}

func ParseValue(value sql.RawBytes) (interface{}, error) {
	if bytes.EqualFold(value, []byte("YES")) || bytes.Equal(value, []byte("ON")) {
		return int64(1), nil
	}

	if bytes.EqualFold(value, []byte("NO")) || bytes.Equal(value, []byte("OFF")) {
		return int64(0), nil
	}

	if val, err := strconv.ParseInt(string(value), 10, 64); err == nil {
		return val, nil
	}
	if val, err := strconv.ParseUint(string(value), 10, 64); err == nil {
		return val, nil
	}
	if val, err := strconv.ParseFloat(string(value), 64); err == nil {
		return val, nil
	}

	if len(string(value)) > 0 {
		return string(value), nil
	}

	return nil, fmt.Errorf("unconvertible value: %q", string(value))
}

var globalStatusConversions = map[string]conversionFunc{
	"innodb_available_undo_logs":    ParseUint,
	"innodb_buffer_pool_pages_misc": ParseUint,
	"innodb_data_pending_fsyncs":    ParseUint,
	"ssl_ctx_verify_depth":          ParseUint,
	"ssl_verify_depth":              ParseUint,

	// see https://galeracluster.com/library/documentation/galera-status-variables.html
	"wsrep_apply_oooe":           ParseFloat,
	"wsrep_apply_oool":           ParseFloat,
	"wsrep_apply_window":         ParseFloat,
	"wsrep_cert_deps_distance":   ParseFloat,
	"wsrep_cert_interval":        ParseFloat,
	"wsrep_commit_oooe":          ParseFloat,
	"wsrep_commit_oool":          ParseFloat,
	"wsrep_commit_window":        ParseFloat,
	"wsrep_flow_control_paused":  ParseFloat,
	"wsrep_local_index":          ParseUint,
	"wsrep_local_recv_queue_avg": ParseFloat,
	"wsrep_local_send_queue_avg": ParseFloat,
}

var globalVariableConversions = map[string]conversionFunc{
	// see https://dev.mysql.com/doc/refman/5.7/en/server-system-variables.html
	// see https://dev.mysql.com/doc/refman/8.0/en/server-system-variables.html
	"delay_key_write":                ParseString,        // ON, OFF, ALL
	"enforce_gtid_consistency":       ParseString,        // ON, OFF, WARN
	"event_scheduler":                ParseString,        // YES, NO, DISABLED
	"have_openssl":                   ParseBoolAsInteger, // alias for have_ssl
	"have_ssl":                       ParseBoolAsInteger, // YES, DISABLED
	"have_symlink":                   ParseBoolAsInteger, // YES, NO, DISABLED
	"session_track_gtids":            ParseString,
	"session_track_transaction_info": ParseString,
	"ssl_fips_mode":                  ParseString,
	"use_secondary_engine":           ParseString,

	// https://dev.mysql.com/doc/refman/5.7/en/replication-options-binary-log.html
	// https://dev.mysql.com/doc/refman/8.0/en/replication-options-binary-log.html
	"transaction_write_set_extraction": ParseString,

	// https://dev.mysql.com/doc/refman/5.7/en/replication-options-replica.html
	// https://dev.mysql.com/doc/refman/8.0/en/replication-options-replica.html
	"slave_skip_errors": ParseString,

	// https://dev.mysql.com/doc/refman/5.7/en/replication-options-gtids.html
	// https://dev.mysql.com/doc/refman/8.0/en/replication-options-gtids.html
	"gtid_mode": ParseGTIDMode,
}

func ConvertGlobalStatus(key string, value sql.RawBytes) (interface{}, error) {
	if bytes.Equal(value, []byte("")) {
		return nil, nil
	}

	if conv, ok := globalStatusConversions[key]; ok {
		return conv(value)
	}

	return ParseValue(value)
}

func ConvertGlobalVariables(key string, value sql.RawBytes) (interface{}, error) {
	if bytes.Equal(value, []byte("")) {
		return nil, nil
	}

	if conv, ok := globalVariableConversions[key]; ok {
		return conv(value)
	}

	return ParseValue(value)
}
