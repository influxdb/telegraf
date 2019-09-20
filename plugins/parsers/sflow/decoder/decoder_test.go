package decoder

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func valueTest(t *testing.T, buffer *bytes.Buffer, fieldName string, iDec func(n string) ItemDecoder, comp func(x interface{}) error) {
	decoder := iDec(fieldName)
	recorder := newDefaultRecorder()
	buffer.WriteByte(55) // put 1 byte on the end
	error := decoder.Decode(buffer, recorder)
	if error != nil {
		t.Error("decode error", error)
	}
	value, ok := recorder.lookup(fieldName)
	if ok {
		if err := comp(value); err != nil {
			t.Fatal("comp failed", err)
		}
	} else {
		t.Fatalf("unable to locate field %s", fieldName)
	}
	if buffer.Len() != 1 { // should be 1 byte left
		t.Fatalf("still bytes left %d", buffer.Len())
	}
}

func Test_ui16(t *testing.T) {

	value := uint16(1001)
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &value); err != nil {
		t.Error("error", err)
	}

	valueTest(
		t,
		&buffer,
		"dstValue",
		func(n string) ItemDecoder { return UI16(n) },
		func(x interface{}) error {
			xui16, ok := x.(uint16)
			if !ok {
				return fmt.Errorf("not a uint16")
			}
			if xui16 != value {
				return fmt.Errorf("value %d not %d", xui16, value)
			}
			return nil
		},
	)
}

func Test_ui32(t *testing.T) {

	value := uint32(1001)
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &value); err != nil {
		t.Error("error", err)
	}

	valueTest(
		t,
		&buffer,
		"dstValue",
		func(n string) ItemDecoder { return UI32(n) },
		func(x interface{}) error {
			xui32, ok := x.(uint32)
			if !ok {
				return fmt.Errorf("not a uint32")
			}
			if xui32 != value {
				return fmt.Errorf("value %d not %d", xui32, value)
			}
			return nil
		},
	)
}

func Test_ui32Mapped(t *testing.T) {

	value := uint32(1001)
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &value); err != nil {
		t.Error("error", err)
	}

	valueTest(
		t,
		&buffer,
		"dstValue",
		func(n string) ItemDecoder { return UI32Mapped(n, map[uint32]string{1001: "1001"}) },
		func(x interface{}) error {
			xstr, ok := x.(string)
			if !ok {
				return fmt.Errorf("not a uint32")
			}
			valueAsString := fmt.Sprintf("%d", value)
			if xstr != valueAsString {
				return fmt.Errorf("value %s not %s", xstr, valueAsString)
			}
			return nil
		},
	)
}

func Test_i32(t *testing.T) {

	value := int32(1001)
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &value); err != nil {
		t.Error("error", err)
	}

	valueTest(
		t,
		&buffer,
		"dstValue",
		func(n string) ItemDecoder { return I32(n) },
		func(x interface{}) error {
			xi32, ok := x.(int32)
			if !ok {
				return fmt.Errorf("not a int32")
			}
			if xi32 != value {
				return fmt.Errorf("value %d not %d", xi32, value)
			}
			return nil
		},
	)
}

func Test_ui64(t *testing.T) {

	value := uint64(1001)
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &value); err != nil {
		t.Error("error", err)
	}

	valueTest(
		t,
		&buffer,
		"dstValue",
		func(n string) ItemDecoder { return UI64(n) },
		func(x interface{}) error {
			xui64, ok := x.(uint64)
			if !ok {
				return fmt.Errorf("not a uint64")
			}
			if xui64 != value {
				return fmt.Errorf("value %d not %d", xui64, value)
			}
			return nil
		},
	)
}

func Test_bin(t *testing.T) {

	value := []byte{0, 255, 42, 255, 100}
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &value); err != nil {
		t.Error("error", err)
	}

	valueTest(
		t,
		&buffer,
		"dstValue",
		func(n string) ItemDecoder { return Bin(n, len(value)) },
		func(x interface{}) error {
			b, ok := x.([]byte)
			if !ok {
				return fmt.Errorf("not a []byte]")
			}
			if bytes.Compare(b, value) != 0 {
				return fmt.Errorf("value %v not %v", b, value)
			}
			return nil
		},
	)
}

func Test_binFn(t *testing.T) {

	value := []byte{223}
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &value); err != nil {
		t.Error("error", err)
	}

	valueTest(
		t,
		&buffer,
		"dstValue",
		func(n string) ItemDecoder {
			return Bin(n, len(value), func(b []byte) interface{} { return uint16(b[0]) })
		},
		func(x interface{}) error {
			b, ok := x.(uint16)
			if !ok {
				return fmt.Errorf("not a []byte]")
			}
			if b != uint16(value[0]) {
				return fmt.Errorf("value %v not %v", b, value)
			}
			return nil
		},
	)

	// This should panic as too many fnctions
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		}
	}()
	Bin("foo", len(value), func(b []byte) interface{} { return nil }, func(b []byte) interface{} { return nil })
}

func Test_sub(t *testing.T) {
	buf := []byte{0, 255, 42, 255, 100}
	recordedBuf := append(buf, []byte{255}...)
	recorder := newDefaultRecorder()
	recorder.record("length", uint32(len(buf)))
	decoder := Sub("length", Bin("dstValue", len(buf)))
	buffer := bytes.NewBuffer(recordedBuf)
	error := decoder.Decode(buffer, recorder)
	if error != nil {
		t.Error("error", error)
	}
	value, ok := recorder.lookup("dstValue")
	if ok {
		bytesOut, ok := value.([]byte)
		if ok {
			if bytes.Compare(bytesOut, buf) != 0 {
				t.Errorf("assigned value was not %v but %v", buf, value)
			}
		} else {
			t.Errorf("resul was nit []bytes but %T", value)
		}
	} else {
		t.Error("unable to locate assigned value")
	}
	if buffer.Len() != 1 {
		t.Errorf("still bytes left %d", buffer.Len())
	}
}

func Test_asgn(t *testing.T) {
	recorder := newDefaultRecorder()
	recorder.record("srcValue", uint(45))
	decoder := Asgn("srcValue", "dstValue")
	err := decoder.Decode(nil, recorder)
	if err != nil {
		t.Errorf("returned error %v", err)
	}
	value, ok := recorder.lookup("dstValue")
	if ok {
		if value != uint(45) {
			t.Errorf("assigned value was not a unit(45) %v", value)
		}
	} else {
		t.Error("unable to locate assigned value")
	}
}

func Test_asrtMaxUnint(t *testing.T) {
	recorder := newDefaultRecorder()
	recorder.record("srcValue", uint(45))
	decoder := AsrtMax("srcValue", uint(45), "Test_asrtMaxUnint", false)
	err := decoder.Decode(nil, recorder)
	if err != nil {
		t.Errorf("returned error %v", err)
	}
	recorder.record("srcValue", uint(46))
	err = decoder.Decode(nil, recorder)
	if err == nil {
		t.Errorf("didn't get expected error")
	} else {
		if _, ok := err.(UnwrapError); !ok {
			t.Errorf("didn't get unwrap expected error")
		}
	}
}

func Test_asrtMaxUnint16(t *testing.T) {
	recorder := newDefaultRecorder()
	recorder.record("srcValue", uint16(45))
	decoder := AsrtMax("srcValue", uint16(45), "Test_asrtMaxUnint16", false)
	err := decoder.Decode(nil, recorder)
	if err != nil {
		t.Errorf("returned error %v", err)
	}
	recorder.record("srcValue", uint16(46))
	err = decoder.Decode(nil, recorder)
	if err == nil {
		t.Errorf("didn't get expected error")
	} else {
		if _, ok := err.(UnwrapError); !ok {
			t.Errorf("didn't get unwrap expected error")
		}
	}
}

func Test_asrtMaxUnint32(t *testing.T) {
	recorder := newDefaultRecorder()
	recorder.record("srcValue", uint32(45))
	decoder := AsrtMax("srcValue", uint32(45), "Test_asrtMaxUnint32", false)
	err := decoder.Decode(nil, recorder)
	if err != nil {
		t.Errorf("returned error %v", err)
	}
	recorder.record("srcValue", uint32(46))
	err = decoder.Decode(nil, recorder)
	if err == nil {
		t.Error("didn't get expected error")
	} else {
		if _, ok := err.(UnwrapError); !ok {
			t.Errorf("didn't get unwrap expected error")
		}
	}
}

func Test_seq(t *testing.T) {
	ui16OneIn := uint16(23)
	ui16TwoIn := uint16(32)
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &ui16OneIn); err != nil {
		t.Error("error", err)
	}
	if err := binary.Write(&buffer, binary.BigEndian, &ui16TwoIn); err != nil {
		t.Error("error", err)
	}
	recorder := newDefaultRecorder()
	decoder := Seq(
		UI16("ui16-1"),
		UI16("ui16-2"),
	)
	err := decoder.Decode(&buffer, recorder)
	if err != nil {
		t.Errorf("returned error %v", err)
	}
	value, ok := recorder.lookup("ui16-1")
	if ok {
		ui16, ok := value.(uint16)
		if ok {
			if ui16 != ui16OneIn {
				t.Fatalf("ui16 != %d but %d", ui16OneIn, ui16)
			}
		} else {
			t.Fatalf("ui16 is not a uint16 but a %v", value)
		}
	}
	value, ok = recorder.lookup("ui16-2")
	if ok {
		ui16, ok := value.(uint16)
		if ok {
			if ui16 != ui16TwoIn {
				t.Fatalf("ui16 != %d but %d", ui16TwoIn, ui16)
			}
		} else {
			t.Fatalf("ui16 is not a uint16 but a %v", value)
		}
	}
}

func Test_altGaurdsAndDefault(t *testing.T) {
	decoder := Alt("",
		Eql("key", uint16(1), UI16("path1")),
		Eql("key", uint16(2), UI16("path2")),
		AltDefault(UI16("defaultPath")),
	)

	// path1 bytes
	ui16Written := uint16(1)
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &ui16Written); err != nil {
		t.Error("error", err)
	}
	recorder := newDefaultRecorder()
	recorder.record("key", uint16(1))
	err := decoder.Decode(&buffer, recorder)
	if err != nil {
		t.Fatal("error", err)
	}
	value, ok := recorder.lookup("path1")
	if ok {
		if ui16Value, ok := value.(uint16); ok {
			if ui16Value != ui16Written {
				t.Fatalf("ui16 read not same as written for path1 %d", ui16Value)
			}
		} else {
			t.Fatalf("path1 value isn't a ui16 but %v", value)
		}
	} else {
		t.Fatal("unable to find path1 value, path1 not taken")
	}

	recorder.record("key", uint16(2))
	buffer.Reset()
	if err := binary.Write(&buffer, binary.BigEndian, &ui16Written); err != nil {
		t.Error("error", err)
	}
	err = decoder.Decode(&buffer, recorder)
	if err != nil {
		t.Fatal("error", err)
	}
	value, ok = recorder.lookup("path2")
	if ok {
		if ui16Value, ok := value.(uint16); ok {
			if ui16Value != ui16Written {
				t.Fatalf("ui16 read not same as written for path2 %d", ui16Value)
			}
		} else {
			t.Fatalf("path2 value isn't a ui16 but %v", value)
		}
	} else {
		t.Fatal("unable to find path2 value, path2 not taken")
	}

	recorder.record("key", uint16(3))
	buffer.Reset()
	if err := binary.Write(&buffer, binary.BigEndian, &ui16Written); err != nil {
		t.Error("error", err)
	}
	err = decoder.Decode(&buffer, recorder)
	if err != nil {
		t.Fatal("error", err)
	}
	value, ok = recorder.lookup("defaultPath")
	if ok {
		if ui16Value, ok := value.(uint16); ok {
			if ui16Value != ui16Written {
				t.Fatalf("ui16 read not same as written for defaultPath %d", ui16Value)
			}
		} else {
			t.Fatalf("defaultPath value isn't a ui16 but %v", value)
		}
	} else {
		t.Fatal("unable to find path2 value, defaultPath not taken")
	}
}

func Test_iter(t *testing.T) {
	ui16OneWritten := uint16(10)
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, &ui16OneWritten); err != nil {
		t.Error("error", err)
	}
	ui16TwoWritten := uint16(11)
	if err := binary.Write(&buffer, binary.BigEndian, &ui16TwoWritten); err != nil {
		t.Error("error", err)
	}
	recorder := newDefaultRecorder()
	recorder.record("count", uint32(2))

	decoder := Iter("result", "count", UI16("value"))
	if err := decoder.Decode(&buffer, recorder); err != nil {
		t.Error("error", err)
	}

	if result, ok := recorder.lookup("result"); ok {
		if resultArray, ok := result.([]map[string]interface{}); ok {
			if len(resultArray) != 2 {
				t.Fatalf("should be 2 entries in result array but %d instead", len(resultArray))
			}
			if v, ok := resultArray[0]["value"]; ok {
				if ui16, ok := v.(uint16); ok {
					if ui16 != ui16OneWritten {
						t.Fatalf("not %d but %d for entry 1", ui16OneWritten, ui16)
					}
				} else {
					t.Fatalf("not ui16 but %T for entry 1", v)
				}
			} else {
				t.Fatal("unable to locate value in 1st array entry")
			}
			if v, ok := resultArray[1]["value"]; ok {
				if ui16, ok := v.(uint16); ok {
					if ui16 != ui16TwoWritten {
						t.Fatalf("not %d but %d for entry 2", ui16TwoWritten, ui16)
					}
				} else {
					t.Fatalf("not ui16 but %T for entry 2", v)
				}
			} else {
				t.Fatal("unable to locate value in 2nd array entry")
			}
		} else {
			t.Fatalf("result is not an []interface but a %T", result)
		}
	} else {
		t.Fatal("unable to locate result entry")
	}
}

func Test_decode(t *testing.T) {
	ui16OneWritten := uint16(10)
	var buffer bytes.Buffer
	assert.NoError(t, binary.Write(&buffer, binary.BigEndian, &ui16OneWritten))
	ui16TwoWritten := uint16(11)
	assert.NoError(t, binary.Write(&buffer, binary.BigEndian, &ui16TwoWritten))
	result, err := Decode(UI16("value"), &buffer)
	assert.NoError(t, err)
	v := result["value"]
	assert.NotNil(t, v)
	vAsUint16, ok := v.(uint16)
	assert.True(t, ok)
	assert.Equal(t, ui16OneWritten, vAsUint16)
}

func Test_Nest(t *testing.T) {
	ui16OneWritten := uint16(10)
	var buffer bytes.Buffer
	assert.NoError(t, binary.Write(&buffer, binary.BigEndian, &ui16OneWritten))
	ui16TwoWritten := uint16(11)
	assert.NoError(t, binary.Write(&buffer, binary.BigEndian, &ui16TwoWritten))
	result, err := Decode(Seq(UI16("value"), Nest("nested", UI16("value"))), &buffer)
	assert.NoError(t, err)
	assert.Equal(t, ui16OneWritten, result["value"])
	assert.Equal(t, []map[string]interface{}{{"value": ui16TwoWritten}}, result["nested"])
}

func Test_WarnAndBreak(t *testing.T) {
	alt := Alt("",
		Eql("key", uint16(1), UI16("path1")),
		AltDefault(WarnAndBreak("path2", "%d", "key")),
	)

	var buffer bytes.Buffer
	ui16OneWritten := uint16(10)
	assert.NoError(t, binary.Write(&buffer, binary.BigEndian, &ui16OneWritten))
	recorder := newDefaultRecorder()
	recorder.record("key", uint16(1))
	assert.NoError(t, alt.Decode(&buffer, recorder))

	// hit the path that has warn and break
	recorder.record("key", uint16(2))
	e := alt.Decode(&buffer, recorder)
	assert.Error(t, e)
	assert.IsType(t, UnwrapError(""), e)

	// now put it in an Sub which should hide the error and just stop cleanly
	recorder.record("length", uint32(buffer.Len()))
	assert.NoError(t, Sub("length", alt).Decode(&buffer, recorder))
}
