package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/awnumar/memguard"
	"github.com/urfave/cli/v2"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/goplugin"
	"github.com/influxdata/telegraf/logger"
	_ "github.com/influxdata/telegraf/plugins/aggregators/all"
	"github.com/influxdata/telegraf/plugins/inputs"
	_ "github.com/influxdata/telegraf/plugins/inputs/all"
	"github.com/influxdata/telegraf/plugins/outputs"
	_ "github.com/influxdata/telegraf/plugins/outputs/all"
	_ "github.com/influxdata/telegraf/plugins/parsers/all"
	_ "github.com/influxdata/telegraf/plugins/processors/all"
	_ "github.com/influxdata/telegraf/plugins/secretstores/all"
	_ "github.com/influxdata/telegraf/plugins/serializers/all"
)

type TelegrafConfig interface {
	CollectDeprecationInfos([]string, []string, []string, []string) map[string][]config.PluginDeprecationInfo
	PrintDeprecationList([]config.PluginDeprecationInfo)
}

type Filters struct {
	section     []string
	input       []string
	output      []string
	aggregator  []string
	processor   []string
	secretstore []string
}

func appendFilter(a, b string) string {
	if a != "" && b != "" {
		return fmt.Sprintf("%s:%s", a, b)
	}
	if a != "" {
		return a
	}
	return b
}

func processFilterFlags(ctx *cli.Context) Filters {
	var section, input, output, aggregator, processor, secretstore string

	// Support defining filters before and after the command
	// The old style was:
	// ./telegraf --section-filter inputs --input-filter cpu config >test.conf
	// The new style is:
	// ./telegraf config --section-filter inputs --input-filter cpu >test.conf
	// To support the old style, check if the parent context has the filter flags defined
	if len(ctx.Lineage()) >= 2 {
		parent := ctx.Lineage()[1] // ancestor contexts in order from child to parent
		section = parent.String("section-filter")
		input = parent.String("input-filter")
		output = parent.String("output-filter")
		aggregator = parent.String("aggregator-filter")
		processor = parent.String("processor-filter")
		secretstore = parent.String("secretstore-filter")
	}

	// If both the parent and command filters are defined, append them together
	section = appendFilter(section, ctx.String("section-filter"))
	input = appendFilter(input, ctx.String("input-filter"))
	output = appendFilter(output, ctx.String("output-filter"))
	aggregator = appendFilter(aggregator, ctx.String("aggregator-filter"))
	processor = appendFilter(processor, ctx.String("processor-filter"))
	secretstore = appendFilter(secretstore, ctx.String("secretstore-filter"))

	sectionFilters := deleteEmpty(strings.Split(section, ":"))
	inputFilters := deleteEmpty(strings.Split(input, ":"))
	outputFilters := deleteEmpty(strings.Split(output, ":"))
	aggregatorFilters := deleteEmpty(strings.Split(aggregator, ":"))
	processorFilters := deleteEmpty(strings.Split(processor, ":"))
	secretstoreFilters := deleteEmpty(strings.Split(secretstore, ":"))
	return Filters{sectionFilters, inputFilters, outputFilters, aggregatorFilters, processorFilters, secretstoreFilters}
}

func deleteEmpty(s []string) []string {
	var r []string
	for _, str := range s {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}

// runApp defines all the subcommands and flags for Telegraf
// this abstraction is used for testing, so outputBuffer and args can be changed
func runApp(args []string, outputBuffer io.Writer, pprof Server, c TelegrafConfig, m App) error {
	configHandlingFlags := []cli.Flag{
		&cli.StringSliceFlag{
			Name:  "config",
			Usage: "configuration file to load",
		},
		&cli.StringSliceFlag{
			Name:  "config-directory",
			Usage: "directory containing additional *.conf files",
		},
		&cli.StringFlag{
			Name: "section-filter",
			Usage: "filter the sections to print, separator is ':'. " +
				"Valid values are 'agent', 'global_tags', 'outputs', 'processors', 'aggregators' and 'inputs'",
		},
		&cli.StringFlag{
			Name:  "input-filter",
			Usage: "filter the inputs to enable, separator is ':'",
		},
		&cli.StringFlag{
			Name:  "output-filter",
			Usage: "filter the outputs to enable, separator is ':'",
		},
		&cli.StringFlag{
			Name:  "aggregator-filter",
			Usage: "filter the aggregators to enable, separator is ':'",
		},
		&cli.StringFlag{
			Name:  "processor-filter",
			Usage: "filter the processors to enable, separator is ':'",
		},
		&cli.StringFlag{
			Name:  "secretstore-filter",
			Usage: "filter the secret-stores to enable, separator is ':'",
		},
	}

	mainFlags := append(configHandlingFlags, cliFlags()...)

	// This function is used when Telegraf is run with only flags
	action := func(cCtx *cli.Context) error {
		// We do not expect any arguments this is likely a misspelling of
		// a command...
		if cCtx.NArg() > 0 {
			return fmt.Errorf("unknown command %q", cCtx.Args().First())
		}

		// Deprecated: Use execd instead
		// Load external plugins, if requested.
		if cCtx.String("plugin-directory") != "" {
			log.Printf("I! Loading external plugins from: %s", cCtx.String("plugin-directory"))
			if err := goplugin.LoadExternalPlugins(cCtx.String("plugin-directory")); err != nil {
				return err
			}
		}

		// switch for flags which just do something and exit immediately
		switch {
		// print available input plugins
		case cCtx.Bool("deprecation-list"):
			filters := processFilterFlags(cCtx)
			infos := c.CollectDeprecationInfos(
				filters.input, filters.output, filters.aggregator, filters.processor,
			)
			outputBuffer.Write([]byte("Deprecated Input Plugins:\n"))
			c.PrintDeprecationList(infos["inputs"])
			outputBuffer.Write([]byte("Deprecated Output Plugins:\n"))
			c.PrintDeprecationList(infos["outputs"])
			outputBuffer.Write([]byte("Deprecated Processor Plugins:\n"))
			c.PrintDeprecationList(infos["processors"])
			outputBuffer.Write([]byte("Deprecated Aggregator Plugins:\n"))
			c.PrintDeprecationList(infos["aggregators"])
			return nil
		// print available output plugins
		case cCtx.Bool("output-list"):
			outputBuffer.Write([]byte("DEPRECATED: use telegraf plugins outputs\n"))
			outputBuffer.Write([]byte("Available Output Plugins:\n"))
			names := make([]string, 0, len(outputs.Outputs))
			for k := range outputs.Outputs {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				fmt.Fprintf(outputBuffer, "  %s\n", k)
			}
			return nil
		// print available input plugins
		case cCtx.Bool("input-list"):
			outputBuffer.Write([]byte("DEPRECATED: use telegraf plugins inputs\n"))
			outputBuffer.Write([]byte("Available Input Plugins:\n"))
			names := make([]string, 0, len(inputs.Inputs))
			for k := range inputs.Inputs {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				fmt.Fprintf(outputBuffer, "  %s\n", k)
			}
			return nil
		// print usage for a plugin, ie, 'telegraf --usage mysql'
		case cCtx.String("usage") != "":
			err := PrintInputConfig(cCtx.String("usage"), outputBuffer)
			err2 := PrintOutputConfig(cCtx.String("usage"), outputBuffer)
			if err != nil && err2 != nil {
				return fmt.Errorf("%w and %w", err, err2)
			}
			return nil
		// DEPRECATED
		case cCtx.Bool("version"):
			fmt.Fprintf(outputBuffer, "%s\n", internal.FormatFullVersion())
			return nil
		// DEPRECATED
		case cCtx.Bool("sample-config"):
			filters := processFilterFlags(cCtx)

			printSampleConfig(outputBuffer, filters)
			return nil
		}

		if cCtx.String("pprof-addr") != "" {
			pprof.Start(cCtx.String("pprof-addr"))
		}

		filters := processFilterFlags(cCtx)

		g := GlobalFlags{
			config:                  cCtx.StringSlice("config"),
			configDir:               cCtx.StringSlice("config-directory"),
			testWait:                cCtx.Int("test-wait"),
			configURLRetryAttempts:  cCtx.Int("config-url-retry-attempts"),
			configURLWatchInterval:  cCtx.Duration("config-url-watch-interval"),
			watchConfig:             cCtx.String("watch-config"),
			watchInterval:           cCtx.Duration("watch-interval"),
			pidFile:                 cCtx.String("pidfile"),
			plugindDir:              cCtx.String("plugin-directory"),
			password:                cCtx.String("password"),
			oldEnvBehavior:          cCtx.Bool("old-env-behavior"),
			printPluginConfigSource: cCtx.Bool("print-plugin-config-source"),
			test:                    cCtx.Bool("test"),
			debug:                   cCtx.Bool("debug"),
			once:                    cCtx.Bool("once"),
			quiet:                   cCtx.Bool("quiet"),
			unprotected:             cCtx.Bool("unprotected"),
		}

		w := WindowFlags{
			service:             cCtx.String("service"),
			serviceName:         cCtx.String("service-name"),
			serviceDisplayName:  cCtx.String("service-display-name"),
			serviceRestartDelay: cCtx.String("service-restart-delay"),
			serviceAutoRestart:  cCtx.Bool("service-auto-restart"),
			console:             cCtx.Bool("console"),
		}

		m.Init(pprof.ErrChan(), filters, g, w)
		return m.Run()
	}

	commands := append(
		getConfigCommands(configHandlingFlags, outputBuffer),
		getSecretStoreCommands(m)...,
	)
	commands = append(commands, getPluginCommands(outputBuffer)...)
	commands = append(commands, getServiceCommands(outputBuffer)...)

	app := &cli.App{
		Name:   "Telegraf",
		Usage:  "The plugin-driven server agent for collecting & reporting metrics.",
		Writer: outputBuffer,
		Flags: append(
			[]cli.Flag{
				// Int flags
				&cli.IntFlag{
					Name:  "test-wait",
					Usage: "wait up to this many seconds for service inputs to complete in test mode",
				},
				&cli.IntFlag{
					Name: "config-url-retry-attempts",
					Usage: "Number of attempts to obtain a remote configuration via a URL during startup. " +
						"Set to -1 for unlimited attempts.",
					DefaultText: "3",
				},
				//
				// String flags
				&cli.StringFlag{
					Name:  "usage",
					Usage: "print usage for a plugin, ie, 'telegraf --usage mysql'",
				},
				&cli.StringFlag{
					Name:  "pprof-addr",
					Usage: "pprof host/IP and port to listen on (e.g. 'localhost:6060')",
				},
				&cli.StringFlag{
					Name: "watch-config",
					Usage: "monitoring config changes [notify, poll] of --config and --config-directory options. " +
						"Notify supports linux, *bsd, and macOS. Poll is required for Windows and checks every 250ms.",
				},
				&cli.StringFlag{
					Name:  "pidfile",
					Usage: "file to write our pid to",
				},
				&cli.StringFlag{
					Name:  "password",
					Usage: "password to unlock secret-stores",
				},
				//
				// Bool flags
				&cli.BoolFlag{
					Name:  "old-env-behavior",
					Usage: "switch back to pre v1.27 environment replacement behavior",
				},
				&cli.BoolFlag{
					Name:  "print-plugin-config-source",
					Usage: "print the source for a given plugin",
				},
				&cli.BoolFlag{
					Name:  "once",
					Usage: "run one gather and exit",
				},
				&cli.BoolFlag{
					Name:  "debug",
					Usage: "turn on debug logging",
				},
				&cli.BoolFlag{
					Name:  "quiet",
					Usage: "run in quiet mode",
				},
				&cli.BoolFlag{
					Name:  "unprotected",
					Usage: "do not protect secrets in memory",
				},
				&cli.BoolFlag{
					Name: "test",
					Usage: "enable test mode: gather metrics, print them out, and exit. " +
						"Note: Test mode only runs inputs, not processors, aggregators, or outputs",
				},
				//
				// Duration flags
				&cli.DurationFlag{
					Name: "watch-interval",
					Usage: "Time duration to check for updates to config files specified by --config and " +
						"--config-directory options. Use with '--watch-config poll'",
					DefaultText: "disabled",
				},
				&cli.DurationFlag{
					Name:        "config-url-watch-interval",
					Usage:       "Time duration to check for updates to URL based configuration files",
					DefaultText: "disabled",
				},
				// TODO: Change "deprecation-list, input-list, output-list" flags to become a subcommand "list" that takes
				// "input,output,aggregator,processor, deprecated" as parameters
				&cli.BoolFlag{
					Name:  "deprecation-list",
					Usage: "print all deprecated plugins or plugin options",
				},
				&cli.BoolFlag{
					Name:  "input-list",
					Usage: "print available input plugins",
				},
				&cli.BoolFlag{
					Name:  "output-list",
					Usage: "print available output plugins",
				},
				//
				// !!! The following flags are DEPRECATED !!!
				// Already covered with the subcommand `./telegraf version`
				&cli.BoolFlag{
					Name:  "version",
					Usage: "DEPRECATED: display the version and exit",
				},
				// Already covered with the subcommand `./telegraf config`
				&cli.BoolFlag{
					Name:  "sample-config",
					Usage: "DEPRECATED: print out full sample configuration",
				},
				// Using execd plugin to add external plugins is preferred (less size impact, easier for end user)
				&cli.StringFlag{
					Name:  "plugin-directory",
					Usage: "DEPRECATED: path to directory containing external plugins",
				},
				// !!!
			}, mainFlags...),
		Action: action,
		Commands: append([]*cli.Command{
			{
				Name:  "version",
				Usage: "print current version to stdout",
				Action: func(*cli.Context) error {
					fmt.Fprintf(outputBuffer, "%s\n", internal.FormatFullVersion())
					return nil
				},
			},
		}, commands...),
	}

	// Make sure we safely erase secrets
	defer memguard.Purge()
	defer logger.CloseLogging()

	if err := app.Run(args); err != nil {
		log.Printf("E! %s", err)
		return err
	}
	return nil
}

func main() {
	// #13481: disables gh:99designs/keyring kwallet.go from connecting to dbus
	os.Setenv("DISABLE_KWALLET", "1")

	agent := Telegraf{}
	pprof := NewPprofServer()
	c := config.NewConfig()
	if err := runApp(os.Args, os.Stdout, pprof, c, &agent); err != nil {
		os.Exit(1)
	}
}
