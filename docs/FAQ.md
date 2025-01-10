# Frequently Asked Questions

## When is the next release? When will my PR or fix get released?

Telegraf has four minor releases a year in March, June, September, and
December. In between each of those minor releases, there are 2-4 bug fix
releases that happen every 3 weeks.

This [Google Calendar][] is kept up to date for upcoming releases dates.
Additionally, users can look at the [GitHub milestones][] for the next minor
and bug fix release.

PRs that resolves issues are released in the next release. PRs that introduce
new features are held for the next minor release. Users can view what
[GitHub milestones][] a PR belongs to to determine the release it will go out
with.

[Google Calendar]: https://calendar.google.com/calendar/embed?src=c_03d981cefd8d6432894cb162da5c6186e393bc0f970ca6c371201aa05d30d763%40group.calendar.google.com
[GitHub milestones]: https://github.com/influxdata/telegraf/milestones

## How can I filter or select specific metrics?

Telegraf has options to select certain metrics or tags as well as filter out
specific tags or fields:

- **Selectors** allow a user to include or exclude entire metrics based on the
  metric name or tag key/pair values.
- **Modifiers** allow a user to remove tags and fields based on specific keys,
  with glob support.

For more details and examples, see the [Metric Filtering][metric filtering]
section in the docs.

## Could not find a usable config.yml, you may have revoked the CircleCI OAuth app

This is an error from CircleCI during test runs.

To resolve the error, you need to log back into CircleCI with your
username/password if that is how you log in or if you use GitHub log, re-create
your oauth/re-login with github.

That should regenerate your token and then allow you to push a commit or close
and reopen this PR and tests should run.

## What does "Context Deadline exceeded (Client.Timeout while awaiting headers)" mean?

This is a generic error received from Go's HTTP client. It is generally the
result of a network blip or hiccup as a result of a DNS, proxy, firewall,
and/or other network issue.

The error should be temporary and Telegraf will recover shortly after without
the loss of data.

## How do I set the timestamp format for parsing data?

Telegraf's `timestamp_format` config option requires the use
[Go's reference time][go ref time] to correctly translate the timestamp. For
example, if you have the time:

```s
2023-03-01T00:00:42.586+0800
```

A user needs the timestamp format:

```s
2006-01-02T15:04:05.000-0700
```

User's can try this out in the [Go playground][playground].

[go ref time]: https://pkg.go.dev/time#pkg-constants
[playground]: https://goplay.tools/snippet/hi9GIOG_gVQ

## Q: How can I monitor the Docker Engine Host from within a container?

You will need to setup several volume mounts as well as some environment
variables:

```shell
docker run --name telegraf \
    -v /:/hostfs:ro \
    -e HOST_ETC=/hostfs/etc \
    -e HOST_PROC=/hostfs/proc \
    -e HOST_SYS=/hostfs/sys \
    -e HOST_VAR=/hostfs/var \
    -e HOST_RUN=/hostfs/run \
    -e HOST_MOUNT_PREFIX=/hostfs \
    telegraf
```

## Q: Why do I get a "no such host" error resolving hostnames that other programs can resolve?

Go uses a pure Go resolver by default for [name resolution](https://golang.org/pkg/net/#hdr-Name_Resolution).
This resolver behaves differently than the C library functions but is more
efficient when used with the Go runtime.

If you encounter problems or want to use more advanced name resolution methods
that are unsupported by the pure Go resolver, you can switch to the cgo
resolver.

If running manually set:

```shell
export GODEBUG=netdns=cgo
```

If running as a service add the environment variable to `/etc/default/telegraf`:

```shell
GODEBUG=netdns=cgo
```

## Q: How can I manage series cardinality?

High [series cardinality][], when not properly managed, can cause high load on
your database.  Telegraf attempts to avoid creating series with high
cardinality, but some monitoring workloads such as tracking containers are are
inherently high cardinality.  These workloads can still be monitored, but care
must be taken to manage cardinality growth.

You can use the following techniques to avoid cardinality issues:

- Use [metric filtering][] options to exclude unneeded measurements and tags.
- Write to a database with an appropriate [retention policy][].
- Consider using the [Time Series Index][tsi].
- Monitor your databases using the [show cardinality][] commands.
- Consult the [InfluxDB documentation][influx docs] for the most up-to-date techniques.

[series cardinality]: https://docs.influxdata.com/influxdb/v1.7/concepts/glossary/#series-cardinality
[metric filtering]: https://github.com/influxdata/telegraf/blob/master/docs/CONFIGURATION.md#metric-filtering
[retention policy]: https://docs.influxdata.com/influxdb/latest/guides/downsampling_and_retention/
[tsi]: https://docs.influxdata.com/influxdb/latest/concepts/time-series-index/
[show cardinality]: https://docs.influxdata.com/influxdb/latest/query_language/spec/#show-cardinality
[influx docs]: https://docs.influxdata.com/influxdb/latest/
