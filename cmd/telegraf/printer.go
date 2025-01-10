package main

import (
	_ "embed"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/aggregators"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/outputs"
	"github.com/influxdata/telegraf/plugins/processors"
	"github.com/influxdata/telegraf/plugins/secretstores"
)

var (
	// Default sections
	sectionDefaults = []string{"global_tags", "agent", "secretstores", "outputs", "processors", "aggregators", "inputs"}

	// Default input plugins
	inputDefaults = []string{"cpu", "mem", "swap", "system", "kernel", "processes", "disk", "diskio"}

	// Default output plugins
	outputDefaults = make([]string, 0)
)

var header = `# Telegraf Configuration
#
# Telegraf is entirely plugin driven. All metrics are gathered from the
# declared inputs, and sent to the declared outputs.
#
# Plugins must be declared in here to be active.
# To deactivate a plugin, comment out the name and any variables.
#
# Use 'telegraf -config telegraf.conf -test' to see what metrics a config
# file would generate.
#
# Environment variables can be used anywhere in this config file, simply surround
# them with ${}. For strings the variable must be within quotes (ie, "${STR_VAR}"),
# for numbers and booleans they should be plain (ie, ${INT_VAR}, ${BOOL_VAR})

`
var globalTagsConfig = `
# Global tags can be specified here in key="value" format.
[global_tags]
  # dc = "us-east-1" # will tag all metrics with dc=us-east-1
  # rack = "1a"
  ## Environment variables can be used as tags, and throughout the config file
  # user = "$USER"

`

// DO NOT REMOVE THE NEXT TWO LINES! This is required to embed the agentConfig data.
//
//go:embed agent.conf
var agentConfig string

var secretstoreHeader = `
###############################################################################
#                            SECRETSTORE PLUGINS                              #
###############################################################################

`

var outputHeader = `
###############################################################################
#                            OUTPUT PLUGINS                                   #
###############################################################################

`

var processorHeader = `
###############################################################################
#                            PROCESSOR PLUGINS                                #
###############################################################################

`

var aggregatorHeader = `
###############################################################################
#                            AGGREGATOR PLUGINS                               #
###############################################################################

`

var inputHeader = `
###############################################################################
#                            INPUT PLUGINS                                    #
###############################################################################

`

var serviceInputHeader = `
###############################################################################
#                            SERVICE INPUT PLUGINS                            #
###############################################################################

`

// printSampleConfig prints the sample config
func printSampleConfig(outputBuffer io.Writer, filters Filters) {
	sectionFilters := filters.section
	inputFilters := filters.input
	outputFilters := filters.output
	aggregatorFilters := filters.aggregator
	processorFilters := filters.processor
	secretstoreFilters := filters.secretstore

	// print headers
	outputBuffer.Write([]byte(header))

	if len(sectionFilters) == 0 {
		sectionFilters = sectionDefaults
	}
	printFilteredGlobalSections(sectionFilters, outputBuffer)

	// print secretstore plugins
	if choice.Contains("secretstores", sectionFilters) {
		if len(secretstoreFilters) != 0 {
			if len(secretstoreFilters) >= 3 && secretstoreFilters[1] != "none" {
				fmt.Print(secretstoreHeader)
			}
			printFilteredSecretstores(secretstoreFilters, false, outputBuffer)
		} else {
			fmt.Print(secretstoreHeader)
			snames := make([]string, 0, len(secretstores.SecretStores))
			for sname := range secretstores.SecretStores {
				snames = append(snames, sname)
			}
			sort.Strings(snames)
			printFilteredSecretstores(snames, true, outputBuffer)
		}
	}

	// print output plugins
	if choice.Contains("outputs", sectionFilters) {
		if len(outputFilters) != 0 {
			if len(outputFilters) >= 3 && outputFilters[1] != "none" {
				outputBuffer.Write([]byte(outputHeader))
			}
			printFilteredOutputs(outputFilters, false, outputBuffer)
		} else {
			outputBuffer.Write([]byte(outputHeader))
			printFilteredOutputs(outputDefaults, false, outputBuffer)
			// Print non-default outputs, commented
			var pnames []string
			for pname := range outputs.Outputs {
				if !choice.Contains(pname, outputDefaults) {
					pnames = append(pnames, pname)
				}
			}
			printFilteredOutputs(pnames, true, outputBuffer)
		}
	}

	// print processor plugins
	if choice.Contains("processors", sectionFilters) {
		if len(processorFilters) != 0 {
			if len(processorFilters) >= 3 && processorFilters[1] != "none" {
				outputBuffer.Write([]byte(processorHeader))
			}
			printFilteredProcessors(processorFilters, false, outputBuffer)
		} else {
			outputBuffer.Write([]byte(processorHeader))
			pnames := make([]string, 0, len(processors.Processors))
			for pname := range processors.Processors {
				pnames = append(pnames, pname)
			}
			printFilteredProcessors(pnames, true, outputBuffer)
		}
	}

	// print aggregator plugins
	if choice.Contains("aggregators", sectionFilters) {
		if len(aggregatorFilters) != 0 {
			if len(aggregatorFilters) >= 3 && aggregatorFilters[1] != "none" {
				outputBuffer.Write([]byte(aggregatorHeader))
			}
			printFilteredAggregators(aggregatorFilters, false, outputBuffer)
		} else {
			outputBuffer.Write([]byte(aggregatorHeader))
			pnames := make([]string, 0, len(aggregators.Aggregators))
			for pname := range aggregators.Aggregators {
				pnames = append(pnames, pname)
			}
			printFilteredAggregators(pnames, true, outputBuffer)
		}
	}

	// print input plugins
	if choice.Contains("inputs", sectionFilters) {
		if len(inputFilters) != 0 {
			if len(inputFilters) >= 3 && inputFilters[1] != "none" {
				outputBuffer.Write([]byte(inputHeader))
			}
			printFilteredInputs(inputFilters, false, outputBuffer)
		} else {
			outputBuffer.Write([]byte(inputHeader))
			printFilteredInputs(inputDefaults, false, outputBuffer)
			// Print non-default inputs, commented
			var pnames []string
			for pname := range inputs.Inputs {
				if !choice.Contains(pname, inputDefaults) {
					pnames = append(pnames, pname)
				}
			}
			printFilteredInputs(pnames, true, outputBuffer)
		}
	}
}

func printFilteredProcessors(processorFilters []string, commented bool, outputBuffer io.Writer) {
	// Filter processors
	var pnames []string
	for pname := range processors.Processors {
		if choice.Contains(pname, processorFilters) {
			pnames = append(pnames, pname)
		}
	}
	sort.Strings(pnames)

	// Print Outputs
	for _, pname := range pnames {
		creator := processors.Processors[pname]
		output := creator()
		printConfig(pname, output, "processors", commented, processors.Deprecations[pname], outputBuffer)
	}
}

func printFilteredAggregators(aggregatorFilters []string, commented bool, outputBuffer io.Writer) {
	// Filter outputs
	var anames []string
	for aname := range aggregators.Aggregators {
		if choice.Contains(aname, aggregatorFilters) {
			anames = append(anames, aname)
		}
	}
	sort.Strings(anames)

	// Print Outputs
	for _, aname := range anames {
		creator := aggregators.Aggregators[aname]
		output := creator()
		printConfig(aname, output, "aggregators", commented, aggregators.Deprecations[aname], outputBuffer)
	}
}

func printFilteredInputs(inputFilters []string, commented bool, outputBuffer io.Writer) {
	// Filter inputs
	var pnames []string
	for pname := range inputs.Inputs {
		if choice.Contains(pname, inputFilters) {
			pnames = append(pnames, pname)
		}
	}
	sort.Strings(pnames)

	// cache service inputs to print them at the end
	servInputs := make(map[string]telegraf.ServiceInput)
	// for alphabetical looping:
	servInputNames := make([]string, 0, len(pnames))

	// Print Inputs
	for _, pname := range pnames {
		// Skip inputs that are registered twice for backward compatibility
		switch pname {
		case "cisco_telemetry_gnmi", "http_listener", "io", "KNXListener":
			continue
		}
		creator := inputs.Inputs[pname]
		input := creator()

		if p, ok := input.(telegraf.ServiceInput); ok {
			servInputs[pname] = p
			servInputNames = append(servInputNames, pname)
			continue
		}

		printConfig(pname, input, "inputs", commented, inputs.Deprecations[pname], outputBuffer)
	}

	// Print Service Inputs
	if len(servInputs) == 0 {
		return
	}
	sort.Strings(servInputNames)

	outputBuffer.Write([]byte(serviceInputHeader))
	for _, name := range servInputNames {
		printConfig(name, servInputs[name], "inputs", commented, inputs.Deprecations[name], outputBuffer)
	}
}

func printFilteredOutputs(outputFilters []string, commented bool, outputBuffer io.Writer) {
	// Filter outputs
	var onames []string
	var influxdbV2 string

	for oname := range outputs.Outputs {
		if choice.Contains(oname, outputFilters) {
			// Make influxdb_v2 the exception and have it be first in the list
			// Store it and add it later
			if oname == "influxdb_v2" {
				influxdbV2 = oname
				continue
			}

			onames = append(onames, oname)
		}
	}
	sort.Strings(onames)

	if influxdbV2 != "" {
		onames = append([]string{influxdbV2}, onames...)
	}

	// Print Outputs
	for _, oname := range onames {
		creator := outputs.Outputs[oname]
		output := creator()
		printConfig(oname, output, "outputs", commented, outputs.Deprecations[oname], outputBuffer)
	}
}

func printFilteredSecretstores(secretstoreFilters []string, commented bool, outputBuffer io.Writer) {
	// Filter secretstores
	var snames []string
	for sname := range secretstores.SecretStores {
		if choice.Contains(sname, secretstoreFilters) {
			snames = append(snames, sname)
		}
	}
	sort.Strings(snames)

	// Print SecretStores
	for _, sname := range snames {
		creator := secretstores.SecretStores[sname]
		store := creator("dummy")
		printConfig(sname, store, "secretstores", commented, secretstores.Deprecations[sname], outputBuffer)
	}
}

func printFilteredGlobalSections(sectionFilters []string, outputBuffer io.Writer) {
	if choice.Contains("global_tags", sectionFilters) {
		outputBuffer.Write([]byte(globalTagsConfig))
	}

	if choice.Contains("agent", sectionFilters) {
		outputBuffer.Write([]byte(agentConfig))
	}
}

func printConfig(name string, p telegraf.PluginDescriber, op string, commented bool, di telegraf.DeprecationInfo, outputBuffer io.Writer) {
	comment := ""
	if commented {
		comment = "# "
	}

	if di.Since != "" {
		removalNote := ""
		if di.RemovalIn != "" {
			removalNote = " and will be removed in " + di.RemovalIn
		}
		fmt.Fprintf(outputBuffer, "\n%s## DEPRECATED: The %q plugin is deprecated in version %s%s, %s.",
			comment, name, di.Since, removalNote, di.Notice)
	}

	sample := p.SampleConfig()
	if sample == "" {
		fmt.Fprintf(outputBuffer, "\n#[[%s.%s]]", op, name)
		fmt.Fprintf(outputBuffer, "\n%s  # no configuration\n\n", comment)
	} else {
		lines := strings.Split(sample, "\n")
		outputBuffer.Write([]byte("\n"))
		for i, line := range lines {
			if i == len(lines)-1 {
				outputBuffer.Write([]byte("\n"))
				continue
			}
			outputBuffer.Write([]byte(strings.TrimRight(comment+line, " ") + "\n"))
		}
	}
}

// PrintInputConfig prints the config usage of a single input.
func PrintInputConfig(name string, outputBuffer io.Writer) error {
	creator, ok := inputs.Inputs[name]
	if !ok {
		return fmt.Errorf("input %s not found", name)
	}

	printConfig(name, creator(), "inputs", false, inputs.Deprecations[name], outputBuffer)
	return nil
}

// PrintOutputConfig prints the config usage of a single output.
func PrintOutputConfig(name string, outputBuffer io.Writer) error {
	creator, ok := outputs.Outputs[name]
	if !ok {
		return fmt.Errorf("output %s not found", name)
	}

	printConfig(name, creator(), "outputs", false, outputs.Deprecations[name], outputBuffer)
	return nil
}
