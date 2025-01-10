# Plugin State-Persistence

## Objective

Retain the state of stateful plugins across restarts of Telegraf.

## Keywords

framework, plugin, stateful, persistence

## Overview

Telegraf contains a number of plugins that hold an internal state while
processing. For some of the plugins this state is important for efficient
processing like the location when reading a large file or when continuously
querying data from a stateful peer requiring for example an offset or the last
queried timestamp. For those plugins it is important to persistent their
internal state over restarts of Telegraf.

It is intended to

- allow for opt-in of plugins to store a state per plugin _instance_
- restore the state for each plugin instances at startup
- track the plugin instances over restarts to relate the stored state with a
  corresponding plugin instance
- automatically compute plugin instance IDs based on the plugin configuration
- provide a way to manually specify instance IDs by the user
- _not_ restore states if the plugin configuration changed between runs
- make implementation easy for plugin developers
- make no assumption on the state _content_

The persistence will use the following steps:

- Compute an unique ID for each of the plugin _instances_
- Startup Telegraf plugins calling `Init()`, etc.
- Initialize persistence framework with the user specified `statefile` location
  and load the state if present
- Determine all stateful plugin instances by fulfilling the `StatefulPlugin`
  interface
- Restore plugin states (if any) for each plugin ID present in the state-file
- Run data-collection etc...
- On shutdown, stopping all Telegraf plugins calling `Stop()` or `Close()`
  depending on the plugin type
- Query the state of all registered stateful plugins state
- Create an overall state-map with the plugin instance ID as a key and the
  serialized plugin state as value.
- Marshal the overall state-map and store to disk

Potential users of this functionality are plugins continuously querying
endpoints with information of a previous query (e.g. timestamps, offsets,
transaction tokens, etc.) The following plugins are known to have an internal
state. This is not a comprehensive list.

- `inputs.win_eventlog` ([PR #8281](https://github.com/influxdata/telegraf/pull/8281))
- `inputs.docker_log` ([PR #7749](https://github.com/influxdata/telegraf/pull/7749))
- `inputs.tail` (file offset)
- `inputs.cloudwatch` (`windowStart`/`windowEnd` parameters)
- `inputs.stackdriver` (`prevEnd` parameter)

### Plugin ID computation

The plugin ID is computed based on the configuration options specified for the
plugin instance. To generate the ID all settings are extracted as `string`
key-value pairs with the option name being the key and the value being the
configuration option setting. For nested configuration options, e.g. if the
plugins has a sub-table, the options are flattened with a canonical key. The
canonical key elements must be concatenated with a dot (`.`) separator. In case
the sub-element is a list of tables, the key must include the index of each
table prefixed by a hash sign i.e. `<parent>#<index>.<child>`.

The resulting key-value pairs of configuration options are then sorted by the
key in lexical order to make the resulting ID invariant against changes in the
order of configuration options. The key and the value of each pair are joined
by a colon (`:`) to a single `string`.

Finally, a SHA256 sum is computed across all key-value strings separated by a
`null` byte. The HEX representation of the resulting SHA256 is used as the
plugin instance ID.

### State serialization format

The overall Telegraf state maps the plugin IDs (keys) to the serialized state
of the corresponding plugin (values). The state data returned by stateful
plugins is serialized to JSON. The resulting byte-sequence is used as the value
for the overall state. On-disk, the overall state of Telegraf is stored as JSON.

To restore the state of a plugin, the overall Telegraf state is first
deserialized from the on-disk JSON data and a lookup for the plugin ID is
performed in the resulting map. The value, if found, is then deserialized to the
plugin's state data-structure and provided to the plugin after calling `Init()`.

## Is / Is-not

### Is

- A framework to persist states over restarts of Telegraf
- A simple local state store
- A way to restore plugin states between restarts without configuration changes
- A unified API for plugins to use when requiring persistence of a state

### Is-Not

- A remote storage framework
- A way to store anything beyond fundamental plugin states
- A data-store or database
- A way to reassign plugin states if their configuration changes
- A tool to interactively adding/removing/modifying states of plugins
- A persistence guarantee beyond clean shutdown (i.e. no crash resistance)

## Prior art

- [PR #8281](https://github.com/influxdata/telegraf/pull/8281): Stores Windows
  event-log bookmarks in the registry
- [PR #7749](https://github.com/influxdata/telegraf/pull/7749): Stores container
  ID and log offset to a file at a user-provided path
- [PR #7537](https://github.com/influxdata/telegraf/pull/7537): Provides a
  global state object and periodically queries plugin states to store the state
  object to a JSON file. This approach does not provide a ID per plugin
  _instance_ so it seems like there is only a single state for a plugin _type_
- [PR #9476](https://github.com/influxdata/telegraf/pull/9476): Register
  stateful plugins to persister and automatically assigns an ID to plugin
  _instances_ based on the configuration. The approach also allows to overwrite
  the automatic ID e.g. with user specified data. It uses the plugin instance ID
  to store/restore state to the same plugin instance and queries the plugin
  state on shutdown and write file (currently JSON).
