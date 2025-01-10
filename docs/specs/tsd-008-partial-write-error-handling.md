# Partial write error handling

## Objective

Provide a way to pass information about partial metric write errors from an
output to the output model.

## Keywords

output plugins, write, error, output model, metric, buffer

## Overview

The output model wrapping each output plugin buffers metrics to be able to batch
those metrics for more efficient sending. In each flush cycle, the model
collects a batch of metrics and hands it over to the output plugin for writing
through the `Write` method. Currently, if writing succeeds (i.e. no error is
returned), _all metrics of the batch_ are removed from the buffer and are marked
as __accepted__ both in terms of statistics as well as in tracking-metric terms.
If writing fails (i.e. any error is returned), _all metrics of the batch_ are
__kept__ in the buffer for requeueing them in the next write cycle.

Issues arise when an output plugin cannot write all metrics of a batch bit only
some to its service endpoint, e.g. due to the metrics being serializable or if
metrics are selectively rejected by the service on the server side. This might
happen when reaching submission limits, violating service constraints e.g.
by out-of-order sends, or due to invalid characters in the serialited metric.
In those cases, an output currently is only able to accept or reject the
_complete batch of metrics_ as there is no mechanism to inform the model (and
in turn the buffer) that only _some_ of the metrics in the batch were failing.

As a consequence, outputs often _accept_ the batch to avoid a requeueing of the
failing metrics for the next flush interval. This distorts statistics of
accepted metrics and causes misleading log messages saying all metrics were
written sucessfully which is not true. Even worse, for outputs ending-up with
partial writes, e.g. only the first half of the metrics can be written to the
service, there is no way of telling the model to selectively accept the actually
written metrics and in turn those outputs must internally buffer the remaining,
unwritten metrics leading to a duplication of buffering logic and adding to code
complexity.

This specification aims at defining the handling of partially successful writes
and introduces the concept of a special _partial write error_ type to reflect
partial writes and partial serialization overcoming the aforementioned issues
and limitations.

To do so, the _partial write error_ error type must contain a list of
successfully written metrics, to be marked __accepted__, both in terms of
statistics as well as in terms of metric tracking, and must be removed from the
buffer. Furthermore, the error must contain a list of metrics that cannot be
sent or serialized and cannot be retried. These metrics must be marked as
__rejected__, both in terms of statistics as well as in terms of metric
tracking,  and must be removed from the buffer.

The error may contain a list of metrics not-yet written to be __kept__ for the
next write cylce. Those metrics must not be marked and must be kept in the
buffer. If the error does not contain the list of not-yet written metrics, this
list must be inferred using the accept and reject lists mentioned above.

To allow the model and the buffer to correctly handle tracking metrics ending up
in the buffer and output the tracking information must be preserved during
communication between the output plugin, the model and the buffer through the
specified error. To do so, all metric lists should be communicated as indices
into the batch to be able to handle tracking metrics correctly.

For backward compatibility and simplicity output plugins can return a `nil`
error to indicate that __all__ metrics of the batch are __accepted__. Similarly,
returing an error _not_ being a _partial write error_ indicates that __all__
metrics of the batch should be __kept__ in the buffer for the next write cycle.

## Related Issues

- [issue #11942](https://github.com/influxdata/telegraf/issues/11942) for
  contradicting log messages
- [issue #14802](https://github.com/influxdata/telegraf/issues/14802) for
  rate-limiting multiple batch sends
- [issue #15908](https://github.com/influxdata/telegraf/issues/15908) for
  infinite loop if single metrics cannot be written
