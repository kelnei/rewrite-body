package compressutil_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"testing"

	"github.com/kelnei/rewrite-body/compressutil"
)

type TestStruct struct {
	desc        string
	input       []byte
	expected    []byte
	encoding    string
	shouldMatch bool
}

// payload is the decoded content every fixture in this file carries.
const payload = "foo is the new bar"

var normalBytes = []byte(payload)

// gzippedBytes is that content in the gzip format of RFC 1952.
var gzippedBytes = []byte{
	31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 74, 203, 207, 87, 200, 44, 86, 40, 201, 72, 85,
	200, 75, 45, 87, 72, 74, 44, 2, 4, 0, 0, 255, 255, 251, 28, 166, 187, 18, 0, 0, 0,
}

// zlibBytes is "foo is the new bar" in the zlib format of RFC 1950, which is
// what HTTP means by Content-Encoding: deflate: the rawDeflateBytes payload
// below wrapped in a 2 byte header and a 4 byte adler32 checksum.
var zlibBytes = []byte{
	120, 156, 74, 203, 207, 87, 200, 44, 86, 40, 201, 72, 85,
	200, 75, 45, 87, 72, 74, 44, 2, 4, 0, 0, 255, 255, 60, 168, 6, 97,
}

// rawDeflateBytes is the same content as bare RFC 1951 DEFLATE, with no zlib
// framing. Not what the spec asks for, but common enough on the wire -- and
// what this plugin itself used to emit -- that decoding still has to accept it.
var rawDeflateBytes = []byte{
	74, 203, 207, 87, 200, 44, 86, 40, 201, 72, 85,
	200, 75, 45, 87, 72, 74, 44, 2, 4, 0, 0, 255, 255,
}

// asIs opens output that should already be the payload itself.
func asIs(reader io.Reader) (io.Reader, error) {
	return reader, nil
}

// TestEncode checks what the output decodes to, not its exact bytes. Those
// are the compressor's choice rather than part of either format: Go 1.27's
// compress/flate stores this payload in a literal block where earlier releases
// Huffman-coded it, and both are valid.
func TestEncode(t *testing.T) {
	tests := []struct {
		desc     string
		encoding string
		// open reads the output with the standard library's reader for the
		// format, not with Decode: Decode also accepts raw DEFLATE under
		// deflate, so it would pass an encoder that left off the zlib wrapper
		// anything reading Content-Encoding: deflate to the letter expects.
		open func(io.Reader) (io.Reader, error)
	}{
		{
			desc:     "should support identity",
			encoding: compressutil.Identity,
			open:     asIs,
		},
		{
			desc:     "should support gzip",
			encoding: compressutil.Gzip,
			open:     func(reader io.Reader) (io.Reader, error) { return gzip.NewReader(reader) },
		},
		{
			desc:     "should support deflate as zlib",
			encoding: compressutil.Deflate,
			open:     func(reader io.Reader) (io.Reader, error) { return zlib.NewReader(reader) },
		},
		{
			desc:     "should NOT support brotli",
			encoding: "br",
			open:     asIs,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			output, err := compressutil.Encode(normalBytes, test.encoding)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			reader, err := test.open(bytes.NewReader(output))
			if err != nil {
				t.Fatalf("got % x, which does not open as %s: %v", output, test.encoding, err)
			}

			decoded, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("got % x, which does not read as %s: %v", output, test.encoding, err)
			}

			if string(decoded) != payload {
				t.Errorf("got %q, want %q", decoded, payload)
			}
		})
	}
}

func TestDecodeDeflateAcceptsBothWireFormats(t *testing.T) {
	tests := []struct {
		desc  string
		input []byte
	}{
		{
			desc:  "zlib wrapped, as HTTP specifies",
			input: zlibBytes,
		},
		{
			desc:  "raw DEFLATE, as many servers actually send",
			input: rawDeflateBytes,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			decoded, err := compressutil.Decode(bytes.NewBuffer(test.input), compressutil.Deflate)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if string(decoded) != payload {
				t.Errorf("got %q, want %q", decoded, payload)
			}
		})
	}
}

func TestDecodeErrorUnwraps(t *testing.T) {
	notGzip := bytes.NewBufferString("this is not gzip data at all, it is plain text")

	_, err := compressutil.Decode(notGzip, compressutil.Gzip)
	if err == nil {
		t.Fatal("expected an error decoding a body that is not gzip")
	}

	var readerError *compressutil.ReaderError
	if !errors.As(err, &readerError) {
		t.Errorf("got %T, want a *compressutil.ReaderError", err)
	}

	if !errors.Is(err, gzip.ErrHeader) {
		t.Errorf("got %v, want it to match gzip.ErrHeader", err)
	}
}

func TestDecodeTruncatedGzipErrorUnwraps(t *testing.T) {
	// A truncated response gets past the gzip header and then ends, so the
	// failure comes from the read rather than the header. Callers still have to
	// be able to recognize it as a ReaderError.
	truncated := bytes.NewBuffer(gzippedBytes[:len(gzippedBytes)-8])

	_, err := compressutil.Decode(truncated, compressutil.Gzip)
	if err == nil {
		t.Fatal("expected an error decoding a truncated gzip body")
	}

	var readerError *compressutil.ReaderError
	if !errors.As(err, &readerError) {
		t.Errorf("got %T, want a *compressutil.ReaderError", err)
	}
}

func TestDecode(t *testing.T) {
	tests := []TestStruct{
		{
			desc:        "should support identity",
			input:       normalBytes,
			expected:    normalBytes,
			encoding:    compressutil.Identity,
			shouldMatch: true,
		},
		{
			desc:        "should support gzip",
			input:       gzippedBytes,
			expected:    normalBytes,
			encoding:    compressutil.Gzip,
			shouldMatch: false,
		},
		{
			desc:        "should support deflate",
			input:       zlibBytes,
			expected:    normalBytes,
			encoding:    compressutil.Deflate,
			shouldMatch: false,
		},
		{
			desc:        "should NOT support brotli",
			input:       normalBytes,
			expected:    normalBytes,
			encoding:    "br",
			shouldMatch: true,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			output, err := compressutil.Decode(bytes.NewBuffer(test.input), test.encoding)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			isBad := !bytes.Equal(test.expected, output)

			if isBad {
				t.Errorf("got body: %v\n wanted: %v", output, test.expected)
			}

			if test.shouldMatch {
				isBad = !bytes.Equal(test.input, output)
			} else {
				isBad = bytes.Equal(test.input, output)
			}

			if isBad {
				t.Errorf("match error got body: %s\n wanted: %s", output, test.input)
			}
		})
	}
}
