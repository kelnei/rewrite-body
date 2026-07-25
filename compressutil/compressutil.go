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
	if encoding == Deflate {
		return decodeDeflate(byteReader.Bytes())
	}

	reader, err := getRawReader(byteReader, encoding)
	if err != nil {
		return nil, &ReaderError{cause: err}
	}

	return io.ReadAll(reader)
}

func getRawReader(byteReader *bytes.Buffer, encoding string) (io.Reader, error) {
	switch encoding {
	case Gzip:
		return gzip.NewReader(byteReader)

	default:
		return byteReader, nil
	}
}

// decodeDeflate reads a Content-Encoding: deflate body.
//
// HTTP defines deflate as the zlib format of RFC 1950, but raw DEFLATE
// (RFC 1951) with no zlib wrapper is common in the wild -- this plugin itself
// emitted it before the matching encode fix. Read the spec-conformant form
// first and fall back to raw, so a body in either format decodes.
//
// The fallback covers the whole read rather than only the header check, because
// a raw stream can begin with two bytes that happen to pass as a zlib header.
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
	switch encoding {
	case Gzip:
		return compressWithGzip(data)

	case Deflate:
		return compressWithZlib(data)

	default:
		return data, nil
	}
}

func compressWithGzip(bodyBytes []byte) ([]byte, error) {
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)

	if _, err := gzipWriter.Write(bodyBytes); err != nil {
		log.Printf("unable to recompress rewrited body: %v", err)

		return nil, err
	}

	if err := gzipWriter.Close(); err != nil {
		log.Printf("unable to close gzip writer: %v", err)

		return nil, err
	}

	return buf.Bytes(), nil
}

// compressWithZlib compresses in the zlib format of RFC 1950, which is what
// HTTP's Content-Encoding: deflate actually asks for. This used to call
// compress/flate and emit raw DEFLATE instead; that round-tripped through this
// plugin's own decoder but would not decode anywhere that took the spec
// literally.
func compressWithZlib(bodyBytes []byte) ([]byte, error) {
	var buf bytes.Buffer
	zlibWriter := zlib.NewWriter(&buf)

	if _, err := zlibWriter.Write(bodyBytes); err != nil {
		log.Printf("unable to recompress rewrited body: %v", err)

		return nil, err
	}

	if err := zlibWriter.Close(); err != nil {
		log.Printf("unable to close zlib writer: %v", err)

		return nil, err
	}

	return buf.Bytes(), nil
}
