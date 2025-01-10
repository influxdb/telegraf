# Probing plugins after startup

## Objective

Allow Telegraf to probe plugins during startup to enable enhanced plugin error
detection like availability of hardware or services

## Keywords

inputs, outputs, startup, probe, error, ignore, behavior

## Overview

When plugins are first instantiated, Telegraf will call the plugin's `Start()`
method (for inputs) or `Connect()` (for outputs) which will initialize its
configuration based off of config options and the running environment. It is
sometimes the case that while the initialization step succeeds, the upstream
service in which the plugin relies on is not actually running, or is not capable
of being communicated with due to incorrect configuration or environmental
problems. In situations like this, Telegraf does not detect that the plugin's
upstream service is not functioning properly, and thus it will continually call
the plugin during each `Gather()` iteration. This often has the effect of
polluting journald and system logs with voluminous error messages, which creates
issues for system administrators who rely on such logs to identify other
unrelated system problems.

More background discussion on this option, including other possible avenues, can
be viewed [here](https://github.com/influxdata/telegraf/issues/16028).

## Probing

Probing is an action whereby the plugin should ensure that the plugin will be
fully functional on a best effort basis. This may comprise communicating with
its external service, trying to access required devices, entities or executables
etc to ensure that the plugin will not produce errors during e.g. data collection
or data output. Probing must *not* produce, process or output any metrics.

Plugins that support probing must implement the `ProbePlugin` interface. Such
plugins must behave in the following manner:

1. Return an error if the external dependencies (hardware, services,
executables, etc.) of the plugin are not available.
2. Return an error if information cannot be gathered (in the case of inputs) or
sent (in the case of outputs) due to unrecoverable issues. For example, invalid
authentication, missing permissions, or non-existent endpoints.
3. Otherwise, return `nil` indicating the plugin will be fully functional.

## Plugin Requirements

Plugins that allow probing must implement the `ProbePlugin` interface. The
exact implementation depends on the plugin's functionality and requirements,
but generally it should take the same actions as it would during normal operation
e.g. calling `Gather()` or `Write()` and check if errors occur. If probing fails,
it must be safe to call the plugin's `Close()` method.

Input plugins must *not* produce metrics, output plugins must *not* send any
metrics to the service. Plugins must *not* influence the later data processing or
collection by modifying the internal state of the plugin or the external state of the
service or hardware. For example, file-offsets or other service states must be
reset to not lose data during the first gather or write cycle.

Plugins must return `nil` upon successful probing or an error otherwise.

## Related Issues

- [#16028](https://github.com/influxdata/telegraf/issues/16028)
- [#15916](https://github.com/influxdata/telegraf/pull/15916)
- [#16001](https://github.com/influxdata/telegraf/pull/16001)
