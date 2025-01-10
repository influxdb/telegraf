package passenger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func fakePassengerStatus(stat string) (string, error) {
	var fileExtension, content string
	if runtime.GOOS == "windows" {
		fileExtension = ".bat"
		content = "@echo off\n"
		for _, line := range strings.Split(strings.TrimSuffix(stat, "\n"), "\n") {
			content += "for /f \"delims=\" %%A in (\"" + line + "\") do echo %%~A\n" // my eyes are bleeding
		}
	} else {
		content = fmt.Sprintf("#!/bin/sh\ncat << EOF\n%s\nEOF", stat)
	}

	tempFilePath := filepath.Join(os.TempDir(), "passenger-status"+fileExtension)
	//nolint:gosec // G306: Expect WriteFile permissions to be 0640 or less - this file needs to be executed
	if err := os.WriteFile(tempFilePath, []byte(content), 0700); err != nil {
		return "", err
	}

	return tempFilePath, nil
}

func teardown(tempFilePath string) {
	os.Remove(tempFilePath)
}

func Test_Invalid_Passenger_Status_Cli(t *testing.T) {
	r := &Passenger{
		Command: "an-invalid-command passenger-status",
	}

	var acc testutil.Accumulator

	err := r.Gather(&acc)
	require.Error(t, err)
	require.Contains(t, err.Error(), `exec: "an-invalid-command": executable file not found in `)
}

func Test_Invalid_Xml(t *testing.T) {
	tempFilePath, err := fakePassengerStatus("invalid xml")
	require.NoError(t, err)
	defer teardown(tempFilePath)

	r := &Passenger{
		Command: tempFilePath,
	}

	var acc testutil.Accumulator

	err = r.Gather(&acc)
	require.Error(t, err)
	require.Equal(t, "cannot parse input with error: EOF", err.Error())
}

// We test this by ensure that the error message match the path of default cli
func Test_Default_Config_Load_Default_Command(t *testing.T) {
	tempFilePath, err := fakePassengerStatus("invalid xml")
	require.NoError(t, err)
	defer teardown(tempFilePath)

	r := &Passenger{}

	var acc testutil.Accumulator

	err = r.Gather(&acc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exec: \"passenger-status\": executable file not found in ")
}

func TestPassengerGenerateMetric(t *testing.T) {
	tempFilePath, err := fakePassengerStatus(sampleStat)
	require.NoError(t, err)
	defer teardown(tempFilePath)

	// Now we tested again above server, with our authentication data
	r := &Passenger{
		Command: tempFilePath,
	}

	var acc testutil.Accumulator

	require.NoError(t, r.Gather(&acc))

	tags := map[string]string{
		"passenger_version": "5.0.17",
	}
	fields := map[string]interface{}{
		"process_count":      23,
		"max":                23,
		"capacity_used":      23,
		"get_wait_list_size": 3,
	}
	acc.AssertContainsTaggedFields(t, "passenger", fields, tags)

	tags = map[string]string{
		"name":     "/var/app/current/public",
		"app_root": "/var/app/current",
		"app_type": "rack",
	}
	fields = map[string]interface{}{
		"processes_being_spawned": 2,
		"capacity_used":           23,
		"get_wait_list_size":      3,
	}
	acc.AssertContainsTaggedFields(t, "passenger_group", fields, tags)

	tags = map[string]string{
		"name": "/var/app/current/public",
	}

	fields = map[string]interface{}{
		"capacity_used":      23,
		"get_wait_list_size": 3,
	}
	acc.AssertContainsTaggedFields(t, "passenger_supergroup", fields, tags)

	tags = map[string]string{
		"app_root":         "/var/app/current",
		"group_name":       "/var/app/current/public",
		"supergroup_name":  "/var/app/current/public",
		"pid":              "11553",
		"code_revision":    "899ac7f",
		"life_status":      "ALIVE",
		"process_group_id": "13608",
	}
	fields = map[string]interface{}{
		"concurrency":           1,
		"sessions":              0,
		"busyness":              0,
		"processed":             951,
		"spawner_creation_time": int64(1452746835922747),
		"spawn_start_time":      int64(1452746844946982),
		"spawn_end_time":        int64(1452746845013365),
		"last_used":             int64(1452747071764940),
		"uptime":                int64(191026), // in seconds of 2d 5h 3m 46s
		"cpu":                   int64(58),
		"rss":                   int64(418548),
		"pss":                   int64(319391),
		"private_dirty":         int64(314900),
		"swap":                  int64(0),
		"real_memory":           int64(314900),
		"vmsize":                int64(1563580),
	}
	acc.AssertContainsTaggedFields(t, "passenger_process", fields, tags)
}

var sampleStat = `
<?xml version="1.0" encoding="iso8859-1" ?>
<?xml version="1.0" encoding="UTF-8"?>
<info version="3">
  <passenger_version>5.0.17</passenger_version>
  <group_count>1</group_count>
  <process_count>23</process_count>
  <max>23</max>
  <capacity_used>23</capacity_used>
  <get_wait_list_size>3</get_wait_list_size>
  <get_wait_list />
  <supergroups>
    <supergroup>
      <name>/var/app/current/public</name>
      <state>READY</state>
      <get_wait_list_size>3</get_wait_list_size>
      <capacity_used>23</capacity_used>
      <secret>foo</secret>
      <group default="true">
        <name>/var/app/current/public</name>
        <component_name>/var/app/current/public</component_name>
        <app_root>/var/app/current</app_root>
        <app_type>rack</app_type>
        <environment>production</environment>
        <uuid>QQUrbCVYxbJYpfgyDOwJ</uuid>
        <enabled_process_count>23</enabled_process_count>
        <disabling_process_count>0</disabling_process_count>
        <disabled_process_count>0</disabled_process_count>
        <capacity_used>23</capacity_used>
        <get_wait_list_size>3</get_wait_list_size>
        <disable_wait_list_size>0</disable_wait_list_size>
        <processes_being_spawned>2</processes_being_spawned>
        <secret>foo</secret>
        <api_key>foo</api_key>
        <life_status>ALIVE</life_status>
        <user>axcoto</user>
        <uid>1001</uid>
        <group>axcoto</group>
        <gid>1001</gid>
        <options>
          <app_root>/var/app/current</app_root>
          <app_group_name>/var/app/current/public</app_group_name>
          <app_type>rack</app_type>
          <start_command>/var/app/.rvm/gems/ruby-2.2.0-p645/gems/passenger-5.0.17/helper-scripts/rack-loader.rb</start_command>
          <startup_file>config.ru</startup_file>
          <process_title>Passenger RubyApp</process_title>
          <log_level>3</log_level>
          <start_timeout>90000</start_timeout>
          <environment>production</environment>
          <base_uri>/</base_uri>
          <spawn_method>smart</spawn_method>
          <default_user>nobody</default_user>
          <default_group>nogroup</default_group>
          <ruby>/var/app/.rvm/gems/ruby-2.2.0-p645/wrappers/ruby</ruby>
          <python>python</python>
          <nodejs>node</nodejs>
          <ust_router_address>unix:/tmp/passenger.eKFdvdC/agents.s/ust_router</ust_router_address>
          <ust_router_username>logging</ust_router_username>
          <ust_router_password>foo</ust_router_password>
          <debugger>false</debugger>
          <analytics>false</analytics>
          <api_key>foo</api_key>
          <min_processes>22</min_processes>
          <max_processes>0</max_processes>
          <max_preloader_idle_time>300</max_preloader_idle_time>
          <max_out_of_band_work_instances>1</max_out_of_band_work_instances>
        </options>
        <processes>
          <process>
            <pid>11553</pid>
            <sticky_session_id>378579907</sticky_session_id>
            <gupid>17173df-PoNT3J9HCf</gupid>
            <concurrency>1</concurrency>
            <sessions>0</sessions>
            <busyness>0</busyness>
            <processed>951</processed>
            <spawner_creation_time>1452746835922747</spawner_creation_time>
            <spawn_start_time>1452746844946982</spawn_start_time>
            <spawn_end_time>1452746845013365</spawn_end_time>
            <last_used>1452747071764940</last_used>
            <last_used_desc>0s ago</last_used_desc>
            <uptime>2d 5h 3m 46s</uptime>
            <code_revision>899ac7f</code_revision>
            <life_status>ALIVE</life_status>
            <enabled>ENABLED</enabled>
            <has_metrics>true</has_metrics>
            <cpu>58</cpu>
            <rss>418548</rss>
            <pss>319391</pss>
            <private_dirty>314900</private_dirty>
            <swap>0</swap>
            <real_memory>314900</real_memory>
            <vmsize>1563580</vmsize>
            <process_group_id>13608</process_group_id>
            <command>Passenger RubyApp: /var/app/current/public</command>
            <sockets>
              <socket>
                <name>main</name>
                <address>unix:/tmp/passenger.eKFdvdC/apps.s/ruby.UWF6zkRJ71aoMXPxpknpWVfC1POFqgWZzbEsdz5v0G46cSSMxJ3GHLFhJaUrK2I</address>
                <protocol>session</protocol>
                <concurrency>1</concurrency>
                <sessions>0</sessions>
              </socket>
              <socket>
                <name>http</name>
                <address>tcp://127.0.0.1:49888</address>
                <protocol>http</protocol>
                <concurrency>1</concurrency>
                <sessions>0</sessions>
              </socket>
            </sockets>
          </process>
          <process>
            <pid>11563</pid>
            <sticky_session_id>1549681201</sticky_session_id>
            <gupid>17173df-pX5iJOipd8</gupid>
            <concurrency>1</concurrency>
            <sessions>1</sessions>
            <busyness>2147483647</busyness>
            <processed>756</processed>
            <spawner_creation_time>1452746835922747</spawner_creation_time>
            <spawn_start_time>1452746845136882</spawn_start_time>
            <spawn_end_time>1452746845172460</spawn_end_time>
            <last_used>1452747071709179</last_used>
            <last_used_desc>0s ago</last_used_desc>
            <uptime>2d 5h 3m 46s</uptime>
            <code_revision>899ac7f</code_revision>
            <life_status>ALIVE</life_status>
            <enabled>ENABLED</enabled>
            <has_metrics>true</has_metrics>
            <cpu>47</cpu>
            <rss>418296</rss>
            <pss>314036</pss>
            <private_dirty>309240</private_dirty>
            <swap>0</swap>
            <real_memory>309240</real_memory>
            <vmsize>1563608</vmsize>
            <process_group_id>13608</process_group_id>
            <command>Passenger RubyApp: /var/app/current/public</command>
            <sockets>
              <socket>
                <name>main</name>
                <address>unix:/tmp/passenger.eKFdvdC/apps.s/ruby.PVCh7TmvCi9knqhba2vG5qXrlHGEIwhGrxnUvRbIAD6SPz9m0G7YlJ8HEsREHY3</address>
                <protocol>session</protocol>
                <concurrency>1</concurrency>
                <sessions>1</sessions>
              </socket>
              <socket>
                <name>http</name>
                <address>tcp://127.0.0.1:52783</address>
                <protocol>http</protocol>
                <concurrency>1</concurrency>
                <sessions>0</sessions>
              </socket>
            </sockets>
          </process>
        </processes>
      </group>
    </supergroup>
  </supergroups>
</info>`
