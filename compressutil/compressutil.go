// Package compressutil a plugin to handle compression and decompression tasks
package compressutil

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"log"
)

// ReaderError for notating that an error occurred while reading compressed data.
type ReaderError struct {
	cause error
}

// Error implements the error interface.
func (readerError *ReaderError) Error() string {
	return fmt.Sprintf("unable to read compressed data: %v", readerError.cause)
}

// Unwrap returns the failure this error was built from, so that errors.Is and
// errors.As can reach it.
func (readerError *ReaderError) Unwrap() error {
	return readerError.cause
}

// Decode data in a bytes.Reader based on supplied encoding.
func Decode(byteReader *bytes.Buffer, encoding string) ([]byte, error) {
	switch encoding {
	case Deflate:
		return decodeDeflate(byteReader.Bytes())

	case Gzip:
		gzipReader, err := gzip.NewReader(byteReader)
		if err != nil {
			return nil, &ReaderError{cause: err}
		}

		return io.ReadAll(gzipReader)

	default:
		return io.ReadAll(byteReader)
	}
}

// decodeDeflate reads a Content-Encoding: deflate body.
//
// HTTP defines deflate as the zlib format of RFC 1950, but raw DEFLATE
// (RFC 1951) with no zlib wrapper is common in the wild -- this plugin itself
// emitted it before the matching encode fix -- so either has to decode. The
// fallback covers the whole read rather than only the header check, because a
// raw stream can begin with two bytes that happen to pass as a zlib header.
func decodeDeflate(data []byte) ([]byte, error) {
	if zlibReader, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
		if decoded, err := io.ReadAll(zlibReader); err == nil {
			return decoded, nil
		}
	}

	decoded, err := io.ReadAll(flate.NewReader(bytes.NewReader(data)))
	if err != nil {
		return nil, &ReaderError{cause: err}
	}

	return decoded, nil
}

// Encode data in a []byte based on supplied encoding.
func Encode(data []byte, encoding string) ([]byte, error) {
	var buf bytes.Buffer

	switch encoding {
	case Gzip:
		return compress(data, &buf, gzip.NewWriter(&buf))

	// HTTP's Content-Encoding: deflate asks for the zlib format of RFC 1950.
	// This used to call compress/flate and emit raw DEFLATE instead; that
	// round-tripped through this plugin's own decoder but would not decode
	// anywhere that took the spec literally.
	case Deflate:
		return compress(data, &buf, zlib.NewWriter(&buf))

	default:
		return data, nil
	}
}

// compress writes bodyBytes through writer and returns what buf collected.
// Closing the writer is what flushes the final block and the format's trailer,
// so it has to happen before the buffer is read.
func compress(bodyBytes []byte, buf *bytes.Buffer, writer io.WriteCloser) ([]byte, error) {
	if _, err := writer.Write(bodyBytes); err != nil {
		log.Printf("unable to recompress rewrited body: %v", err)

		return nil, err
	}

	if err := writer.Close(); err != nil {
		log.Printf("unable to close compression writer: %v", err)

		return nil, err
	}

	return buf.Bytes(), nil
}
