package modbus

import (
	_ "embed"
	"errors"
	"fmt"
	"hash/maphash"
	"math"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
)

//go:embed sample_request.conf
var sampleConfigPartPerRequest string

type requestFieldDefinition struct {
	Address     uint16  `toml:"address"`
	Name        string  `toml:"name"`
	InputType   string  `toml:"type"`
	Length      uint16  `toml:"length"`
	Scale       float64 `toml:"scale"`
	OutputType  string  `toml:"output"`
	Measurement string  `toml:"measurement"`
	Omit        bool    `toml:"omit"`
	Bit         uint8   `toml:"bit"`
}

type requestDefinition struct {
	SlaveID           byte                     `toml:"slave_id"`
	ByteOrder         string                   `toml:"byte_order"`
	RegisterType      string                   `toml:"register"`
	Measurement       string                   `toml:"measurement"`
	Optimization      string                   `toml:"optimization"`
	MaxExtraRegisters uint16                   `toml:"optimization_max_register_fill"`
	Fields            []requestFieldDefinition `toml:"fields"`
	Tags              map[string]string        `toml:"tags"`
}

type configurationPerRequest struct {
	Requests []requestDefinition `toml:"request"`

	workarounds         workarounds
	excludeRegisterType bool
	logger              telegraf.Logger
}

func (*configurationPerRequest) sampleConfigPart() string {
	return sampleConfigPartPerRequest
}

func (c *configurationPerRequest) check() error {
	switch c.workarounds.StringRegisterLocation {
	case "", "both", "lower", "upper":
		// Do nothing as those are valid
	default:
		return fmt.Errorf("invalid 'string_register_location' %q", c.workarounds.StringRegisterLocation)
	}

	seed := maphash.MakeSeed()
	seenFields := make(map[uint64]bool)

	for _, def := range c.Requests {
		// Check byte order of the data
		switch def.ByteOrder {
		case "":
			def.ByteOrder = "ABCD"
		case "ABCD", "DCBA", "BADC", "CDAB", "MSW-BE", "MSW-LE", "LSW-LE", "LSW-BE":
		default:
			return fmt.Errorf("unknown byte-order %q", def.ByteOrder)
		}

		// Check register type
		switch def.RegisterType {
		case "":
			def.RegisterType = "holding"
		case "coil", "discrete", "holding", "input":
		default:
			return fmt.Errorf("unknown register-type %q", def.RegisterType)
		}
		// Check for valid optimization
		switch def.Optimization {
		case "", "none", "shrink", "rearrange":
		case "aggressive":
			config.PrintOptionValueDeprecationNotice(
				"inputs.modbus",
				"optimization",
				"aggressive",
				telegraf.DeprecationInfo{
					Since:     "1.28.2",
					RemovalIn: "1.30.0",
					Notice:    `use "max_insert" instead`,
				},
			)
		case "max_insert":
			switch def.RegisterType {
			case "coil":
				if def.MaxExtraRegisters <= 0 || def.MaxExtraRegisters > maxQuantityCoils {
					return fmt.Errorf("optimization_max_register_fill has to be between 1 and %d", maxQuantityCoils)
				}
			case "discrete":
				if def.MaxExtraRegisters <= 0 || def.MaxExtraRegisters > maxQuantityDiscreteInput {
					return fmt.Errorf("optimization_max_register_fill has to be between 1 and %d", maxQuantityDiscreteInput)
				}
			case "holding":
				if def.MaxExtraRegisters <= 0 || def.MaxExtraRegisters > maxQuantityHoldingRegisters {
					return fmt.Errorf("optimization_max_register_fill has to be between 1 and %d", maxQuantityHoldingRegisters)
				}
			case "input":
				if def.MaxExtraRegisters <= 0 || def.MaxExtraRegisters > maxQuantityInputRegisters {
					return fmt.Errorf("optimization_max_register_fill has to be between 1 and %d", maxQuantityInputRegisters)
				}
			}
		default:
			return fmt.Errorf("unknown optimization %q", def.Optimization)
		}
		// Set the default for measurement if required
		if def.Measurement == "" {
			def.Measurement = "modbus"
		}

		// Reject any configuration without fields as it
		// makes no sense to not define anything but a request.
		if len(def.Fields) == 0 {
			return errors.New("found request section without fields")
		}

		// Check the fields
		for fidx, f := range def.Fields {
			// Check the input type for all fields except the bit-field ones.
			// We later need the type (even for omitted fields) to determine the length.
			if def.RegisterType == "holding" || def.RegisterType == "input" {
				switch f.InputType {
				case "":
				case "INT8L", "INT8H", "INT16", "INT32", "INT64",
					"UINT8L", "UINT8H", "UINT16", "UINT32", "UINT64",
					"FLOAT16", "FLOAT32", "FLOAT64":
					if f.Length != 0 {
						return fmt.Errorf("length option cannot be used for type %q of field %q", f.InputType, f.Name)
					}
					if f.Bit != 0 {
						return fmt.Errorf("bit option cannot be used for type %q of field %q", f.InputType, f.Name)
					}
					if f.OutputType == "STRING" {
						return fmt.Errorf("cannot output field %q as string", f.Name)
					}
				case "STRING":
					if f.Length < 1 {
						return fmt.Errorf("missing length for string field %q", f.Name)
					}
					if f.Bit != 0 {
						return fmt.Errorf("bit option cannot be used for type %q of field %q", f.InputType, f.Name)
					}
					if f.Scale != 0.0 {
						return fmt.Errorf("scale option cannot be used for string field %q", f.Name)
					}
					if f.OutputType != "" && f.OutputType != "STRING" {
						return fmt.Errorf("invalid output type %q for string field %q", f.OutputType, f.Name)
					}
				case "BIT":
					if f.Length != 0 {
						return fmt.Errorf("length option cannot be used for type %q of field %q", f.InputType, f.Name)
					}
					if f.OutputType == "STRING" {
						return fmt.Errorf("cannot output field %q as string", f.Name)
					}
				default:
					return fmt.Errorf("unknown register data-type %q for field %q", f.InputType, f.Name)
				}
			}

			// Other properties don't need to be checked for omitted fields
			if f.Omit {
				continue
			}

			// Name is mandatory
			if f.Name == "" {
				return fmt.Errorf("empty field name in request for slave %d", def.SlaveID)
			}

			// Check output type
			if def.RegisterType == "holding" || def.RegisterType == "input" {
				switch f.OutputType {
				case "", "INT64", "UINT64", "FLOAT64", "STRING":
				default:
					return fmt.Errorf("unknown output data-type %q for field %q", f.OutputType, f.Name)
				}
			} else {
				// Bit register types can only be UINT64 or BOOL
				switch f.OutputType {
				case "", "UINT16", "BOOL":
				default:
					return fmt.Errorf("unknown output data-type %q for field %q", f.OutputType, f.Name)
				}
			}

			// Handle the default for measurement
			if f.Measurement == "" {
				f.Measurement = def.Measurement
			}
			def.Fields[fidx] = f

			// Check for duplicate field definitions
			id := c.fieldID(seed, def, f)
			if seenFields[id] {
				return fmt.Errorf("field %q duplicated in measurement %q (slave %d/%q)", f.Name, f.Measurement, def.SlaveID, def.RegisterType)
			}
			seenFields[id] = true
		}
	}

	return nil
}

func (c *configurationPerRequest) process() (map[byte]requestSet, error) {
	result := make(map[byte]requestSet, len(c.Requests))
	for _, def := range c.Requests {
		// Set default
		if def.RegisterType == "" {
			def.RegisterType = "holding"
		}

		// Construct the fields
		isTyped := def.RegisterType == "holding" || def.RegisterType == "input"
		fields, err := c.initFields(def.Fields, isTyped, def.ByteOrder)
		if err != nil {
			return nil, err
		}

		// Make sure we have a set to work with
		set, found := result[def.SlaveID]
		if !found {
			set = requestSet{}
		}

		params := groupingParams{
			maxExtraRegisters: def.MaxExtraRegisters,
			optimization:      def.Optimization,
			tags:              def.Tags,
			log:               c.logger,
		}
		switch def.RegisterType {
		case "coil":
			params.maxBatchSize = maxQuantityCoils
			if c.workarounds.OnRequestPerField {
				params.maxBatchSize = 1
			}
			params.enforceFromZero = c.workarounds.ReadCoilsStartingAtZero
			requests := groupFieldsToRequests(fields, params)
			set.coil = append(set.coil, requests...)
		case "discrete":
			params.maxBatchSize = maxQuantityDiscreteInput
			if c.workarounds.OnRequestPerField {
				params.maxBatchSize = 1
			}
			requests := groupFieldsToRequests(fields, params)
			set.discrete = append(set.discrete, requests...)
		case "holding":
			params.maxBatchSize = maxQuantityHoldingRegisters
			if c.workarounds.OnRequestPerField {
				params.maxBatchSize = 1
			}
			requests := groupFieldsToRequests(fields, params)
			set.holding = append(set.holding, requests...)
		case "input":
			params.maxBatchSize = maxQuantityInputRegisters
			if c.workarounds.OnRequestPerField {
				params.maxBatchSize = 1
			}
			requests := groupFieldsToRequests(fields, params)
			set.input = append(set.input, requests...)
		default:
			return nil, fmt.Errorf("unknown register type %q", def.RegisterType)
		}
		if !set.empty() {
			result[def.SlaveID] = set
		}
	}

	return result, nil
}

func (c *configurationPerRequest) initFields(fieldDefs []requestFieldDefinition, typed bool, byteOrder string) ([]field, error) {
	// Construct the fields from the field definitions
	fields := make([]field, 0, len(fieldDefs))
	for _, def := range fieldDefs {
		f, err := c.newFieldFromDefinition(def, typed, byteOrder)
		if err != nil {
			return nil, fmt.Errorf("initializing field %q failed: %w", def.Name, err)
		}
		fields = append(fields, f)
	}

	return fields, nil
}

func (c *configurationPerRequest) newFieldFromDefinition(def requestFieldDefinition, typed bool, byteOrder string) (field, error) {
	var err error

	fieldLength := uint16(1)
	if typed {
		if fieldLength, err = determineFieldLength(def.InputType, def.Length); err != nil {
			return field{}, err
		}
	}

	// Check for address overflow
	if def.Address > math.MaxUint16-fieldLength {
		return field{}, fmt.Errorf("%w for field %q", errAddressOverflow, def.Name)
	}

	// Initialize the field
	f := field{
		measurement: def.Measurement,
		name:        def.Name,
		address:     def.Address,
		length:      fieldLength,
		omit:        def.Omit,
	}

	// Handle type conversions for coil and discrete registers
	if !typed {
		f.converter, err = determineUntypedConverter(def.OutputType)
		if err != nil {
			return field{}, err
		}
	}

	// No more processing for un-typed (coil and discrete registers) or omitted fields
	if !typed || def.Omit {
		return f, nil
	}

	// Automagically determine the output type...
	if def.OutputType == "" {
		if def.Scale == 0.0 {
			// For non-scaling cases we should choose the output corresponding to the input class
			// i.e. INT64 for INT*, UINT64 for UINT* etc.
			var err error
			if def.OutputType, err = determineOutputDatatype(def.InputType); err != nil {
				return field{}, err
			}
		} else {
			// For scaling cases we always want FLOAT64 by default except for
			// string fields
			if def.InputType != "STRING" {
				def.OutputType = "FLOAT64"
			} else {
				def.OutputType = "STRING"
			}
		}
	}

	// Setting default byte-order
	if byteOrder == "" {
		byteOrder = "ABCD"
	}

	// Normalize the data relevant for determining the converter
	inType, err := normalizeInputDatatype(def.InputType)
	if err != nil {
		return field{}, err
	}
	outType, err := normalizeOutputDatatype(def.OutputType)
	if err != nil {
		return field{}, err
	}
	order, err := normalizeByteOrder(byteOrder)
	if err != nil {
		return field{}, err
	}

	f.converter, err = determineConverter(inType, order, outType, def.Scale, def.Bit, c.workarounds.StringRegisterLocation)
	if err != nil {
		return field{}, err
	}

	return f, nil
}

func (c *configurationPerRequest) fieldID(seed maphash.Seed, def requestDefinition, field requestFieldDefinition) uint64 {
	var mh maphash.Hash
	mh.SetSeed(seed)

	mh.WriteByte(def.SlaveID)
	mh.WriteByte(0)
	if !c.excludeRegisterType {
		mh.WriteString(def.RegisterType)
		mh.WriteByte(0)
	}
	mh.WriteString(field.Measurement)
	mh.WriteByte(0)
	mh.WriteString(field.Name)
	mh.WriteByte(0)

	// tags
	for k, v := range def.Tags {
		mh.WriteString(k)
		mh.WriteByte('=')
		mh.WriteString(v)
		mh.WriteByte(':')
	}
	mh.WriteByte(0)

	return mh.Sum64()
}

func determineOutputDatatype(input string) (string, error) {
	// Handle our special types
	switch input {
	case "INT8L", "INT8H", "INT16", "INT32", "INT64":
		return "INT64", nil
	case "BIT", "UINT8L", "UINT8H", "UINT16", "UINT32", "UINT64":
		return "UINT64", nil
	case "FLOAT16", "FLOAT32", "FLOAT64":
		return "FLOAT64", nil
	case "STRING":
		return "STRING", nil
	}
	return "unknown", fmt.Errorf("invalid input datatype %q for determining output", input)
}

func determineFieldLength(input string, length uint16) (uint16, error) {
	// Handle our special types
	switch input {
	case "BIT", "INT8L", "INT8H", "UINT8L", "UINT8H":
		return 1, nil
	case "INT16", "UINT16", "FLOAT16":
		return 1, nil
	case "INT32", "UINT32", "FLOAT32":
		return 2, nil
	case "INT64", "UINT64", "FLOAT64":
		return 4, nil
	case "STRING":
		return length, nil
	}
	return 0, fmt.Errorf("invalid input datatype %q for determining field length", input)
}
