# Quix Output Plugin

This plugin writes metrics to a [Quix][quix] endpoint.

Please consult Quix's [official documentation][docs] for more details on the
Quix platform architecture and concepts.

⭐ Telegraf v1.33.0
🏷️ cloud, messaging
💻 all

[quix]: https://quix.io
[docs]: https://quix.io/docs/

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Secret-store support

This plugin supports secrets from secret-stores for the `token` option.
See the [secret-store documentation][SECRETSTORE] for more details on how
to use them.

[SECRETSTORE]: ../../../docs/CONFIGURATION.md#secret-store-secrets

## Configuration

```toml @sample.conf
# Send metrics to a Quix data processing pipeline
[[outputs.quix]]
  ## Endpoint for providing the configuration
  # url = "https://portal-api.platform.quix.io"

  ## Workspace and topics to send the metrics to
  workspace = "your_workspace"
  topic = "your_topic"

  ## Authentication token created in Quix
  token = "your_auth_token"

  ## Amount of time allowed to complete the HTTP request for fetching the config
  # timeout = "5s"
```

The plugin requires a [SDK token][token] for authentication with Quix. You can
generate the `token` in settings under the `API and tokens` section.

Furthermore, the `workspace` parameter must be set to the `Workspace ID` or the
`Environment ID` of your Quix project. Those values can be found in settings
under the `General settings` section.

[token]: https://quix.io/docs/develop/authentication/personal-access-token.html
