package shim

import (
	"errors"
	"fmt"
	"log" //nolint:depguard // Allow exceptional but valid use of log here.
	"os"

	"github.com/BurntSushi/toml"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/outputs"
	"github.com/influxdata/telegraf/plugins/processors"
)

type config struct {
	Inputs     map[string][]toml.Primitive
	Processors map[string][]toml.Primitive
	Outputs    map[string][]toml.Primitive
}

type loadedConfig struct {
	Input     telegraf.Input
	Processor telegraf.StreamingProcessor
	Output    telegraf.Output
}

// LoadConfig Adds plugins to the shim
func (s *Shim) LoadConfig(filePath *string) error {
	conf, err := LoadConfig(filePath)
	if err != nil {
		return err
	}
	if conf.Input != nil {
		if err = s.AddInput(conf.Input); err != nil {
			return fmt.Errorf("failed to add Input: %w", err)
		}
	} else if conf.Processor != nil {
		if err = s.AddStreamingProcessor(conf.Processor); err != nil {
			return fmt.Errorf("failed to add Processor: %w", err)
		}
	} else if conf.Output != nil {
		if err = s.AddOutput(conf.Output); err != nil {
			return fmt.Errorf("failed to add Output: %w", err)
		}
	}
	return nil
}

// LoadConfig loads the config and returns inputs that later need to be loaded.
func LoadConfig(filePath *string) (loaded loadedConfig, err error) {
	var data string
	conf := config{}
	if filePath != nil && *filePath != "" {
		b, err := os.ReadFile(*filePath)
		if err != nil {
			return loadedConfig{}, err
		}

		data = expandEnvVars(b)
	} else {
		conf = DefaultImportedPlugins()
	}

	md, err := toml.Decode(data, &conf)
	if err != nil {
		return loadedConfig{}, err
	}

	return createPluginsWithTomlConfig(md, conf)
}

func expandEnvVars(contents []byte) string {
	return os.Expand(string(contents), getEnv)
}

func getEnv(key string) string {
	v := os.Getenv(key)

	return envVarEscaper.Replace(v)
}

func createPluginsWithTomlConfig(md toml.MetaData, conf config) (loadedConfig, error) {
	loadedConf := loadedConfig{}

	for name, primitives := range conf.Inputs {
		creator, ok := inputs.Inputs[name]
		if !ok {
			return loadedConf, errors.New("unknown input " + name)
		}

		plugin := creator()
		if len(primitives) > 0 {
			primitive := primitives[0]
			if err := md.PrimitiveDecode(primitive, plugin); err != nil {
				return loadedConf, err
			}
		}

		loadedConf.Input = plugin
		break
	}

	for name, primitives := range conf.Processors {
		creator, ok := processors.Processors[name]
		if !ok {
			return loadedConf, errors.New("unknown processor " + name)
		}

		plugin := creator()
		if len(primitives) > 0 {
			primitive := primitives[0]
			var p telegraf.PluginDescriber = plugin
			if processor, ok := plugin.(processors.HasUnwrap); ok {
				p = processor.Unwrap()
			}
			if err := md.PrimitiveDecode(primitive, p); err != nil {
				return loadedConf, err
			}
		}
		loadedConf.Processor = plugin
		break
	}

	for name, primitives := range conf.Outputs {
		creator, ok := outputs.Outputs[name]
		if !ok {
			return loadedConf, errors.New("unknown output " + name)
		}

		plugin := creator()
		if len(primitives) > 0 {
			primitive := primitives[0]
			if err := md.PrimitiveDecode(primitive, plugin); err != nil {
				return loadedConf, err
			}
		}
		loadedConf.Output = plugin
		break
	}
	return loadedConf, nil
}

// DefaultImportedPlugins defaults to whatever plugins happen to be loaded and
// have registered themselves with the registry. This makes loading plugins
// without having to define a config dead easy.
func DefaultImportedPlugins() config {
	conf := config{
		Inputs:     make(map[string][]toml.Primitive, len(inputs.Inputs)),
		Processors: make(map[string][]toml.Primitive, len(processors.Processors)),
		Outputs:    make(map[string][]toml.Primitive, len(outputs.Outputs)),
	}
	for name := range inputs.Inputs {
		log.Println("No config found. Loading default config for plugin", name)
		conf.Inputs[name] = make([]toml.Primitive, 0)
		return conf
	}
	for name := range processors.Processors {
		log.Println("No config found. Loading default config for plugin", name)
		conf.Processors[name] = make([]toml.Primitive, 0)
		return conf
	}
	for name := range outputs.Outputs {
		log.Println("No config found. Loading default config for plugin", name)
		conf.Outputs[name] = make([]toml.Primitive, 0)
		return conf
	}
	return conf
}
