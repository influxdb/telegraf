package modbus

import (
	_ "embed"
	"errors"
	"fmt"
	"hash/maphash"
	"math"

	"github.com/influxdata/telegraf"
)

//go:embed sample_metric.conf
var sampleConfigPartPerMetric string

type metricFieldDefinition struct {
	RegisterType string  `toml:"register"`
	Address      uint16  `toml:"address"`
	Length       uint16  `toml:"length"`
	Name         string  `toml:"name"`
	InputType    string  `toml:"type"`
	Scale        float64 `toml:"scale"`
	OutputType   string  `toml:"output"`
	Bit          uint8   `toml:"bit"`
}

type metricDefinition struct {
	SlaveID     byte                    `toml:"slave_id"`
	ByteOrder   string                  `toml:"byte_order"`
	Measurement string                  `toml:"measurement"`
	Fields      []metricFieldDefinition `toml:"fields"`
	Tags        map[string]string       `toml:"tags"`
}

type configurationPerMetric struct {
	Optimization      string             `toml:"optimization"`
	MaxExtraRegisters uint16             `toml:"optimization_max_register_fill"`
	Metrics           []metricDefinition `toml:"metric"`

	workarounds         workarounds
	excludeRegisterType bool
	logger              telegraf.Logger
}

func (*configurationPerMetric) sampleConfigPart() string {
	return sampleConfigPartPerMetric
}

func (c *configurationPerMetric) check() error {
	switch c.workarounds.StringRegisterLocation {
	case "", "both", "lower", "upper":
		// Do nothing as those are valid
	default:
		return fmt.Errorf("invalid 'string_register_location' %q", c.workarounds.StringRegisterLocation)
	}

	seed := maphash.MakeSeed()
	seenFields := make(map[uint64]bool)

	// Check optimization algorithm
	switch c.Optimization {
	case "", "none":
		c.Optimization = "none"
	case "max_insert":
		if c.MaxExtraRegisters == 0 {
			c.MaxExtraRegisters = 50
		}
	default:
		return fmt.Errorf("unknown optimization %q", c.Optimization)
	}

	for defidx, def := range c.Metrics {
		// Check byte order of the data
		switch def.ByteOrder {
		case "":
			def.ByteOrder = "ABCD"
		case "ABCD", "DCBA", "BADC", "CDAB", "MSW-BE", "MSW-LE", "LSW-LE", "LSW-BE":
		default:
			return fmt.Errorf("unknown byte-order %q", def.ByteOrder)
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
			// Name is mandatory
			if f.Name == "" {
				return fmt.Errorf("empty field name in request for slave %d", def.SlaveID)
			}

			// Check register type
			switch f.RegisterType {
			case "":
				f.RegisterType = "holding"
			case "coil", "discrete", "holding", "input":
			default:
				return fmt.Errorf("unknown register-type %q for field %q", f.RegisterType, f.Name)
			}

			// Check the input and output type for all fields as we later need
			// it to determine the number of registers to query.
			switch f.RegisterType {
			case "holding", "input":
				// Check the input type
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

				// Check output type
				switch f.OutputType {
				case "", "INT64", "UINT64", "FLOAT64", "STRING":
				default:
					return fmt.Errorf("unknown output data-type %q for field %q", f.OutputType, f.Name)
				}
			case "coil", "discrete":
				// Bit register types can only be UINT64 or BOOL
				switch f.OutputType {
				case "", "UINT16", "BOOL":
				default:
					return fmt.Errorf("unknown output data-type %q for field %q", f.OutputType, f.Name)
				}
			}
			def.Fields[fidx] = f

			// Check for duplicate field definitions
			id := c.fieldID(seed, def, f)
			if seenFields[id] {
				return fmt.Errorf("field %q duplicated in measurement %q (slave %d)", f.Name, def.Measurement, def.SlaveID)
			}
			seenFields[id] = true
		}
		c.Metrics[defidx] = def
	}

	return nil
}

func (c *configurationPerMetric) process() (map[byte]requestSet, error) {
	collection := make(map[byte]map[string][]field)

	// Collect the requested registers across metrics and transform them into
	// requests. This will produce one request per slave and register-type
	for _, def := range c.Metrics {
		// Make sure we have a set to work with
		set, found := collection[def.SlaveID]
		if !found {
			set = make(map[string][]field)
		}

		for _, fdef := range def.Fields {
			// Construct the field from the field definition
			f, err := c.newField(fdef, def)
			if err != nil {
				return nil, fmt.Errorf("initializing field %q of measurement %q failed: %w", fdef.Name, def.Measurement, err)
			}

			// Attach the field to the correct set
			set[fdef.RegisterType] = append(set[fdef.RegisterType], f)
		}
		collection[def.SlaveID] = set
	}

	result := make(map[byte]requestSet)

	params := groupingParams{
		optimization:      c.Optimization,
		maxExtraRegisters: c.MaxExtraRegisters,
		log:               c.logger,
	}
	for sid, scollection := range collection {
		var set requestSet
		for registerType, fields := range scollection {
			switch registerType {
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
				return nil, fmt.Errorf("unknown register type %q", registerType)
			}
		}
		if !set.empty() {
			result[sid] = set
		}
	}

	return result, nil
}

func (c *configurationPerMetric) newField(def metricFieldDefinition, mdef metricDefinition) (field, error) {
	typed := def.RegisterType == "holding" || def.RegisterType == "input"

	fieldLength := uint16(1)
	if typed {
		var err error
		if fieldLength, err = c.determineFieldLength(def.InputType, def.Length); err != nil {
			return field{}, err
		}
	}

	// Check for address overflow
	if def.Address > math.MaxUint16-fieldLength {
		return field{}, fmt.Errorf("%w for field %q", errAddressOverflow, def.Name)
	}

	// Initialize the field
	f := field{
		measurement: mdef.Measurement,
		name:        def.Name,
		address:     def.Address,
		length:      fieldLength,
		tags:        mdef.Tags,
	}

	// Handle type conversions for coil and discrete registers
	if !typed {
		var err error
		f.converter, err = determineUntypedConverter(def.OutputType)
		if err != nil {
			return field{}, err
		}
		// No more processing for un-typed (coil and discrete registers) fields
		return f, nil
	}

	// Automagically determine the output type...
	if def.OutputType == "" {
		if def.Scale == 0.0 {
			// For non-scaling cases we should choose the output corresponding to the input class
			// i.e. INT64 for INT*, UINT64 for UINT* etc.
			var err error
			if def.OutputType, err = c.determineOutputDatatype(def.InputType); err != nil {
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
	byteOrder := mdef.ByteOrder
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

func (c *configurationPerMetric) fieldID(seed maphash.Seed, def metricDefinition, field metricFieldDefinition) uint64 {
	var mh maphash.Hash
	mh.SetSeed(seed)

	mh.WriteByte(def.SlaveID)
	mh.WriteByte(0)
	if !c.excludeRegisterType {
		mh.WriteString(field.RegisterType)
		mh.WriteByte(0)
	}
	mh.WriteString(def.Measurement)
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

func (*configurationPerMetric) determineOutputDatatype(input string) (string, error) {
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

func (*configurationPerMetric) determineFieldLength(input string, length uint16) (uint16, error) {
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
