package starlark

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"strings"

	"go.starlark.net/lib/json"
	"go.starlark.net/lib/math"
	"go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/influxdata/telegraf"
)

type Common struct {
	Source    string                 `toml:"source"`
	Script    string                 `toml:"script"`
	Constants map[string]interface{} `toml:"constants"`

	Log              telegraf.Logger `toml:"-"`
	StarlarkLoadFunc func(module string, logger telegraf.Logger) (starlark.StringDict, error)

	thread     *starlark.Thread
	builtins   starlark.StringDict
	globals    starlark.StringDict
	functions  map[string]*starlark.Function
	parameters map[string]starlark.Tuple
	state      *starlark.Dict
}

func (s *Common) GetState() interface{} {
	// Return the actual byte-type instead of nil allowing the persister
	// to guess instantiate variable of the appropriate type
	if s.state == nil {
		return make([]byte, 0)
	}

	// Convert the starlark dict into a golang dictionary for serialization
	state := make(map[string]interface{}, s.state.Len())
	items := s.state.Items()
	for _, item := range items {
		if len(item) != 2 {
			// We do expect key-value pairs in the state so there should be
			// two items.
			s.Log.Errorf("state item %+v does not contain a key-value pair", item)
			continue
		}
		k, ok := item.Index(0).(starlark.String)
		if !ok {
			s.Log.Errorf("state item %+v has invalid key type %T", item, item.Index(0))
			continue
		}
		v, err := asGoValue(item.Index(1))
		if err != nil {
			s.Log.Errorf("state item %+v value cannot be converted: %v", item, err)
			continue
		}
		state[k.GoString()] = v
	}

	// Do a binary GOB encoding to preserve types
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(state); err != nil {
		s.Log.Errorf("encoding state failed: %v", err)
		return make([]byte, 0)
	}

	return buf.Bytes()
}

func (s *Common) SetState(state interface{}) error {
	data, ok := state.([]byte)
	if !ok {
		return fmt.Errorf("unexpected type %T for state", state)
	}
	if len(data) == 0 {
		return nil
	}

	// Decode the binary GOB encoding
	var dict map[string]interface{}
	if err := gob.NewDecoder(bytes.NewBuffer(data)).Decode(&dict); err != nil {
		return fmt.Errorf("decoding state failed: %w", err)
	}

	// Convert the golang dict back to starlark types
	s.state = starlark.NewDict(len(dict))
	for k, v := range dict {
		sv, err := asStarlarkValue(v)
		if err != nil {
			return fmt.Errorf("value %v of state item %q cannot be set: %w", v, k, err)
		}
		if err := s.state.SetKey(starlark.String(k), sv); err != nil {
			return fmt.Errorf("state item %q cannot be set: %w", k, err)
		}
	}
	s.builtins["state"] = s.state

	return s.InitProgram()
}

func (s *Common) Init() error {
	if s.Source == "" && s.Script == "" {
		return errors.New("one of source or script must be set")
	}
	if s.Source != "" && s.Script != "" {
		return errors.New("both source or script cannot be set")
	}

	s.builtins = starlark.StringDict{}
	s.builtins["Metric"] = starlark.NewBuiltin("Metric", newMetric)
	s.builtins["deepcopy"] = starlark.NewBuiltin("deepcopy", deepcopy)
	s.builtins["catch"] = starlark.NewBuiltin("catch", catch)

	if err := s.addConstants(&s.builtins); err != nil {
		return err
	}

	// Initialize the program
	if err := s.InitProgram(); err != nil {
		// Try again with a declared state. This might be necessary for
		// state persistence.
		s.state = starlark.NewDict(0)
		s.builtins["state"] = s.state
		if serr := s.InitProgram(); serr != nil {
			return err
		}
	}

	s.functions = make(map[string]*starlark.Function)
	s.parameters = make(map[string]starlark.Tuple)

	return nil
}

func (s *Common) InitProgram() error {
	// Load the program. In case of an error we can try to insert the state
	// which can be used implicitly e.g. when persisting states
	program, err := s.sourceProgram(s.builtins)
	if err != nil {
		return err
	}

	// Execute source
	s.thread = &starlark.Thread{
		Print: func(_ *starlark.Thread, msg string) { s.Log.Debug(msg) },
		Load: func(_ *starlark.Thread, module string) (starlark.StringDict, error) {
			return s.StarlarkLoadFunc(module, s.Log)
		},
	}
	globals, err := program.Init(s.thread, s.builtins)
	if err != nil {
		return err
	}

	// In case the program declares a global "state" we should insert it to
	// avoid warnings about inserting into a frozen variable
	if _, found := globals["state"]; found {
		globals["state"] = starlark.NewDict(0)
	}

	// Freeze the global state. This prevents modifications to the processor
	// state and prevents scripts from containing errors storing tracking
	// metrics. Tasks that require global state will not be possible due to
	// this, so maybe we should relax this in the future.
	globals.Freeze()
	s.globals = globals

	return nil
}

func (s *Common) GetParameters(name string) (starlark.Tuple, bool) {
	parameters, found := s.parameters[name]
	return parameters, found
}

func (s *Common) AddFunction(name string, params ...starlark.Value) error {
	globalFn, found := s.globals[name]
	if !found {
		return fmt.Errorf("%s is not defined", name)
	}

	fn, found := globalFn.(*starlark.Function)
	if !found {
		return fmt.Errorf("%s is not a function", name)
	}

	if fn.NumParams() != len(params) {
		return fmt.Errorf("%s function must take %d parameter(s)", name, len(params))
	}
	p := make(starlark.Tuple, len(params))
	copy(p, params)

	s.functions[name] = fn
	s.parameters[name] = params
	return nil
}

// Add all the constants defined in the plugin as constants of the script
func (s *Common) addConstants(builtins *starlark.StringDict) error {
	for key, val := range s.Constants {
		if key == "state" {
			return errors.New("'state' constant uses reserved name")
		}
		sVal, err := asStarlarkValue(val)
		if err != nil {
			return fmt.Errorf("converting type %T failed: %w", val, err)
		}
		(*builtins)[key] = sVal
	}
	return nil
}

func (s *Common) sourceProgram(builtins starlark.StringDict) (*starlark.Program, error) {
	var src interface{}
	if s.Source != "" {
		src = s.Source
	}

	// AllowFloat - obsolete, no effect
	// AllowNestedDef - always on https://github.com/google/starlark-go/pull/328
	// AllowLambda - always on https://github.com/google/starlark-go/pull/328
	options := syntax.FileOptions{
		Recursion:      true,
		GlobalReassign: true,
		Set:            true,
	}

	_, program, err := starlark.SourceProgramOptions(&options, s.Script, src, builtins.Has)
	return program, err
}

// Call calls the function corresponding to the given name.
func (s *Common) Call(name string) (starlark.Value, error) {
	fn, ok := s.functions[name]
	if !ok {
		return nil, fmt.Errorf("function %q does not exist", name)
	}
	args, ok := s.parameters[name]
	if !ok {
		return nil, fmt.Errorf("params for function %q do not exist", name)
	}
	return starlark.Call(s.thread, fn, args, nil)
}

func (s *Common) LogError(err error) {
	var evalErr *starlark.EvalError
	if errors.As(err, &evalErr) {
		for _, line := range strings.Split(evalErr.Backtrace(), "\n") {
			s.Log.Error(line)
		}
	} else {
		s.Log.Error(err)
	}
}

func LoadFunc(module string, logger telegraf.Logger) (starlark.StringDict, error) {
	switch module {
	case "json.star":
		return starlark.StringDict{
			"json": json.Module,
		}, nil
	case "logging.star":
		return starlark.StringDict{
			"log": LogModule(logger),
		}, nil
	case "math.star":
		return starlark.StringDict{
			"math": math.Module,
		}, nil
	case "time.star":
		return starlark.StringDict{
			"time": time.Module,
		}, nil
	default:
		return nil, errors.New("module " + module + " is not available")
	}
}
