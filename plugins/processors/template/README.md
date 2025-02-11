# Template Processor Plugin

The `template` processor applies a Go template to metrics to generate a new
tag.  The primary use case of this plugin is to create a tag that can be used
for dynamic routing to multiple output plugins or using an output specific
routing option.

The template has access to each metric's measurement name, tags, fields, and
timestamp using the [interface in `/template_metric.go`](template_metric.go).
[Sprig](http://masterminds.github.io/sprig/) helper functions are available.

Read the full [Go Template Documentation][].

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Uses a Go template to create a new tag
[[processors.template]]
  ## Go template used to create the tag name of the output. In order to
  ## ease TOML escaping requirements, you should use single quotes around
  ## the template string.
  tag = "topic"

  ## Go template used to create the tag value of the output. In order to
  ## ease TOML escaping requirements, you should use single quotes around
  ## the template string.
  template = '{{ .Tag "hostname" }}.{{ .Tag "level" }}'
```

## Examples

### Combine multiple tags to create a single tag

```toml
[[processors.template]]
  tag = "topic"
  template = '{{ .Tag "hostname" }}.{{ .Tag "level" }}'
```

```diff
- cpu,level=debug,hostname=localhost time_idle=42
+ cpu,level=debug,hostname=localhost,topic=localhost.debug time_idle=42
```

### Use a field value as tag name

```toml
[[processors.template]]
  tag = '{{ .Field "type" }}'
  template = '{{ .Name }}'
```

```diff
- cpu,level=debug,hostname=localhost time_idle=42,type=sensor
+ cpu,level=debug,hostname=localhost,sensor=cpu time_idle=42,type=sensor
```

### Add measurement name as a tag

```toml
[[processors.template]]
  tag = "measurement"
  template = '{{ .Name }}'
```

```diff
- cpu,hostname=localhost time_idle=42
+ cpu,hostname=localhost,measurement=cpu time_idle=42
```

### Add the year as a tag, similar to the date processor

```toml
[[processors.template]]
  tag = "year"
  template = '{{.Time.UTC.Year}}'
```

### Add all fields as a tag

Sometimes it is useful to pass all fields with their values into a single
message for sending it to a monitoring system (e.g. Syslog, GroundWork), then
you can use `.Fields` or `.Tags`:

```toml
[[processors.template]]
  tag = "message"
  template = 'Message about {{.Name}} fields: {{.Fields}}'
```

```diff
- cpu,hostname=localhost time_idle=42
+ cpu,hostname=localhost,message=Message\ about\ cpu\ fields:\ map[time_idle:42] time_idle=42
```

More advanced example, which might make more sense:

```toml
[[processors.template]]
  tag = "message"
  template = '''Message about {{.Name}} fields:
{{ range $field, $value := .Fields -}}
{{$field}}:{{$value}}
{{ end }}'''
```

```diff
- cpu,hostname=localhost time_idle=42
+ cpu,hostname=localhost,message=Message\ about\ cpu\ fields:\ntime_idle:42\n time_idle=42
```

### Just add the current metric as a tag

```toml
[[processors.template]]
  tag = "metric"
  template = '{{.}}'
```

```diff
- cpu,hostname=localhost time_idle=42
+ cpu,hostname=localhost,metric=cpu\ map[hostname:localhost]\ map[time_idle:42]\ 1257894000000000000 time_idle=42
```

[Go Template Documentation]: https://golang.org/pkg/text/template/
