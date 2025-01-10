# Startup Error Behavior

## Objective

Unified, configurable behavior on retriable startup errors.

## Keywords

inputs, outputs, startup, error, retry

## Overview

Many Telegraf plugins connect to an external service either on the same machine
or via network. On automated startup of Telegraf (e.g. via service) there is no
guarantee that those services are fully started yet, especially when they reside
on a remote host. More and more plugins implement mechanisms to retry reaching
their related service if they failed to do so on startup.

This specification intends to unify the naming of configuration-options, the
values of those options, and their semantic meaning. It describes the behavior
for the different options on handling startup-errors.

Startup errors are all errors occurring in calls to `Start()` for inputs and
service-inputs or `Connect()` for outputs. The behaviors described below
should only be applied in cases where the plugin *explicitly* states that an
startup error is *retriable*. This includes for example network errors
indicating that the host or service is not yet reachable or external
resources, like a machine or file, which are not yet available, but might become
available later. To indicate a retriable startup error the plugin should return
a predefined error-type.

In cases where the error cannot be generally determined be retriable by
the plugin, the plugin might add configuration settings to let the user
configure that property. For example, where an error code indicates a fatal,
non-recoverable error in one case but a non-fatal, recoverable error in another
case.

## Configuration Options and Behaviors

Telegraf must introduce a unified `startup_error_behavior` configuration option
for inputs and output plugins. The option is handled directly by the Telegraf
agent and is not passed down to the plugins. The setting must be available on a
per-plugin basis and defines how Telegraf behaves on startup errors.

For all config option values Telegraf might retry to start the plugin for a
limited number of times during the startup phase before actually processing
data. This corresponds to the current behavior of Telegraf to retry three times
with a fifteen second interval before continuing processing of the plugins.

### `error` behavior

The `error` setting for the `startup_error_behavior` option causes Telegraf to
fail and exit on startup errors. This must be the default behavior.

### `retry` behavior

The `retry` setting for the `startup_error_behavior` option Telegraf must *not*
fail on startup errors and should continue running. Telegraf must retry to
startup the failed plugin in each gather or write cycle, for inputs or for
outputs respectively, for an unlimited number of times. Neither the
plugin's `Gather()` nor `Write()` method is called as long as the startup did
not succeed. Metrics sent to an output plugin will be buffered until the plugin
is actually started. If the metric-buffer limit is reached **metrics might be
dropped**!

In case a plugin signals a partially successful startup, e.g. a subset of the
given endpoints are reachable, Telegraf must try to fully startup the remaining
endpoints by calling `Start()` or `Connect()`, respectively, until full startup
is reached **and** trigger the plugin's `Gather()` nor `Write()` methods.

### `ignore` behavior

When using the `ignore` setting for the `startup_error_behavior` option Telegraf
must *not* fail on startup errors and should continue running. On startup error,
Telegraf must ignore the plugin as-if it was not configured at all, i.e. the
plugin must be completely removed from processing.

### `probe` behavior

When using the `probe` setting for the `startup_error_behavior` option Telegraf
must *not* fail on startup errors and should continue running. On startup error,
Telegraf must ignore the plugin as-if it was not configured at all, i.e. the
plugin must be completely removed from processing, similar to the `ignore`
behavior. Additionally, Telegraf must probe the plugin (as defined in
[TSD-009][tsd_009]) after startup, if it implements the `ProbePlugin` interface.
If probing is available *and* returns an error Telegraf must *ignore* the
plugin as-if it was not configured at all.

[tsd_009]: /docs/specs/tsd-009-probe-on-startup.md

## Plugin Requirements

Plugins participating in handling startup errors must implement the `Start()`
or `Connect()` method for inputs and outputs respectively. Those methods must be
safe to be called multiple times during retries without leaking resources or
causing issues in the service used.

Furthermore, the `Close()` method of the plugins must be safe to be called for
cases where the startup failed without causing panics.

The plugins should return a `nil` error during startup to indicate a successful
startup or a retriable error (via predefined error type) to enable the defined
startup error behaviors. A non-retriable error (via predefined error type) or
a generic error will bypass the startup error behaviors and Telegraf must fail
and exit in the startup phase.

## Related Issues

- [#8586](https://github.com/influxdata/telegraf/issues/8586) `inputs.postgresql`
- [#9778](https://github.com/influxdata/telegraf/issues/9778) `outputs.kafka`
- [#13278](https://github.com/influxdata/telegraf/issues/13278) `outputs.cratedb`
- [#13746](https://github.com/influxdata/telegraf/issues/13746) `inputs.amqp_consumer`
- [#14365](https://github.com/influxdata/telegraf/issues/14365) `outputs.postgresql`
- [#14603](https://github.com/influxdata/telegraf/issues/14603) `inputs.nvidia-smi`
- [#14603](https://github.com/influxdata/telegraf/issues/14603) `inputs.rocm-smi`
