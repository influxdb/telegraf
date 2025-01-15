package binaryio

import "io"

// MinimumReader is the implementation for MinReader.
type MinimumReader struct {
	reader                 io.Reader
	minNumberOfBytesToRead int64 // Min number of bytes we need to read from the reader
}

// MinReader reads from the reader but ensures there is at least N bytes read from the reader.
// The reader should call Close() when they are done reading.
// Closing the MinReader will read and discard any unread bytes up to minNumberOfBytesToRead.
// CLosing the MinReader does NOT close the underlying reader.
// The underlying implementation is a MinimumReader, which implements ReaderCloser.
func MinReader(r io.Reader, minNumberOfBytesToRead int64) *MinimumReader {
	return &MinimumReader{
		reader:                 r,
		minNumberOfBytesToRead: minNumberOfBytesToRead,
	}
}

func (r *MinimumReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	r.minNumberOfBytesToRead -= int64(n)
	return n, err
}

// Close does not close the underlying reader, only the MinimumReader
func (r *MinimumReader) Close() error {
	if r.minNumberOfBytesToRead > 0 {
		b := make([]byte, r.minNumberOfBytesToRead)
		_, err := r.reader.Read(b)
		return err
	}
	return nil
}
