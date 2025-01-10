package kinesis_consumer

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
)

type decodingFunc func([]byte) ([]byte, error)

func processGzip(data []byte) ([]byte, error) {
	zipData, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zipData.Close()
	return io.ReadAll(zipData)
}

func processZlib(data []byte) ([]byte, error) {
	zlibData, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zlibData.Close()
	return io.ReadAll(zlibData)
}

func processNoOp(data []byte) ([]byte, error) {
	return data, nil
}

func getDecodingFunc(encoding string) (decodingFunc, error) {
	switch encoding {
	case "gzip":
		return processGzip, nil
	case "zlib":
		return processZlib, nil
	case "none", "identity", "":
		return processNoOp, nil
	}
	return nil, fmt.Errorf("unknown content encoding %q", encoding)
}
