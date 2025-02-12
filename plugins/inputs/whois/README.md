# whois Input Plugin

The `whois` input plugin queries **WHOIS** information for configured
domains and provides metrics such as expiration timestamps, registrar
details, and domain status.

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Reads WHOIS data and expose as metrics
[[inputs.whois]]
  ## List of domains to query
  domains = ["example.com", "influxdata.com"]

  ## Timeout for WHOIS queries
  # timeout = "5s"
```

## Metrics

- whois
  - tags:
    - domain
    - registrar
    - status
  - fields:
    - whois_expiration_timestamp (int, seconds)
    - expiry (int, seconds) - Time when the certificate will expire, in seconds
      since the Unix epoch. `SELECT (expiry / 60 / 60 / 24) as "expiry_in_days"`

## Example Output

```text
whois,domain=example.com,registrar=RESERVED-Internet\ Assigned\ Numbers\ Authority,status=LOCKED expiration_timestamp=1755057600 expiry=15688984i 1739368616000000000
whois,domain=influxdata.com,registrar=NameCheap\,\ Inc.,status=LOCKED expiration_timestamp=1750758483,expiry=11389867i 1739368617000000000
```
