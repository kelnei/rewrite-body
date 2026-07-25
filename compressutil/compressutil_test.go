package compressutil_test

import (
	"bytes"
	"compress/gzip"
	"errors"
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

func TestEncode(t *testing.T) {
	var (
		deflatedBytes = zlibBytes
		gzippedBytes  = []byte{
			31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 74, 203, 207, 87, 200, 44, 86, 40, 201, 72, 85,
			200, 75, 45, 87, 72, 74, 44, 2, 4, 0, 0, 255, 255, 251, 28, 166, 187, 18, 0, 0, 0,
		}
		normalBytes = []byte("foo is the new bar")
	)

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
			input:       normalBytes,
			expected:    gzippedBytes,
			encoding:    compressutil.Gzip,
			shouldMatch: false,
		},
		{
			desc:        "should support deflate",
			input:       normalBytes,
			expected:    deflatedBytes,
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
			output, err := compressutil.Encode(test.input, test.encoding)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			isBad := !bytes.Equal(test.expected, output)

			if isBad {
				t.Errorf("expected error got body: %v\n wanted: %v", output, test.expected)
			}

			if test.shouldMatch {
				isBad = !bytes.Equal(test.input, output)
			} else {
				isBad = bytes.Equal(test.input, output)
			}

			if isBad {
				t.Errorf("match error got body: %v\n wanted: %v", output, test.input)
			}
		})
	}
}

func TestDecodeDeflateAcceptsBothWireFormats(t *testing.T) {
	const expected = "foo is the new bar"

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

			if string(decoded) != expected {
				t.Errorf("got %q, want %q", decoded, expected)
			}
		})
	}
}

func TestEncodeDeflateRoundTrips(t *testing.T) {
	const expected = "foo is the new bar"

	encoded, err := compressutil.Encode([]byte(expected), compressutil.Deflate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Anything reading Content-Encoding: deflate to the letter expects the zlib
	// header, so the encoder has to produce it.
	if len(encoded) < 2 || encoded[0] != 120 {
		t.Errorf("got % v, want output starting with a zlib header", encoded)
	}

	decoded, err := compressutil.Decode(bytes.NewBuffer(encoded), compressutil.Deflate)
	if err != nil {
		t.Fatalf("unexpected error decoding: %v", err)
	}

	if string(decoded) != expected {
		t.Errorf("got %q, want %q", decoded, expected)
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

func TestDecode(t *testing.T) {
	var (
		deflatedBytes = zlibBytes
		gzippedBytes  = []byte{
			31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 74, 203, 207, 87, 200, 44, 86, 40, 201, 72, 85,
			200, 75, 45, 87, 72, 74, 44, 2, 4, 0, 0, 255, 255, 251, 28, 166, 187, 18, 0, 0, 0,
		}
		normalBytes = []byte("foo is the new bar")
	)

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
			input:       deflatedBytes,
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
				t.Errorf("expected error got body: %v\n wanted: %v", output, test.expected)
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
