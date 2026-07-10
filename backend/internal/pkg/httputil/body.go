package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	requestBodyReadInitCap    = 512
	requestBodyReadMaxInitCap = 1 << 20
	jsonUTF8BOMLen            = 3
	// maxDecompressedBodySize limits the decompressed request body to 64 MB
	// to prevent decompression bomb attacks.
	maxDecompressedBodySize = 64 << 20
)

var (
	ErrRequestBodyTooLarge            = errors.New("decompressed request body exceeds limit")
	ErrUnsupportedContentEncoding     = errors.New("unsupported Content-Encoding")
	ErrMalformedCompressedRequestBody = errors.New("malformed compressed request body")
)

type RequestBodyTooLargeError struct {
	Limit int64
}

func (e *RequestBodyTooLargeError) Error() string {
	return fmt.Sprintf("%v (limit=%d bytes)", ErrRequestBodyTooLarge, e.Limit)
}

func (e *RequestBodyTooLargeError) Is(target error) bool {
	return target == ErrRequestBodyTooLarge
}

type UnsupportedContentEncodingError struct {
	Encoding string
}

func (e *UnsupportedContentEncodingError) Error() string {
	return fmt.Sprintf("%v: %q", ErrUnsupportedContentEncoding, e.Encoding)
}

func (e *UnsupportedContentEncodingError) Is(target error) bool {
	return target == ErrUnsupportedContentEncoding
}

type MalformedCompressedRequestBodyError struct {
	Encoding string
	Err      error
}

func (e *MalformedCompressedRequestBodyError) Error() string {
	return fmt.Sprintf("%v for %q: %v", ErrMalformedCompressedRequestBody, e.Encoding, e.Err)
}

func (e *MalformedCompressedRequestBodyError) Unwrap() error {
	return e.Err
}

func (e *MalformedCompressedRequestBodyError) Is(target error) bool {
	return target == ErrMalformedCompressedRequestBody
}

// ReadRequestBodyWithPrealloc reads request body with preallocated buffer based
// on content length, transparently decoding any Content-Encoding the upstream
// client used to compress the body (zstd, gzip, deflate).
func ReadRequestBodyWithPrealloc(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}

	capHint := requestBodyReadInitCap
	if req.ContentLength > 0 {
		switch {
		case req.ContentLength < int64(requestBodyReadInitCap):
			capHint = requestBodyReadInitCap
		case req.ContentLength > int64(requestBodyReadMaxInitCap):
			capHint = requestBodyReadMaxInitCap
		default:
			capHint = int(req.ContentLength)
		}
	}

	buf := bytes.NewBuffer(make([]byte, 0, capHint))
	if _, err := io.Copy(buf, req.Body); err != nil {
		return nil, err
	}
	raw := buf.Bytes()

	enc := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Encoding")))
	if enc == "" || enc == "identity" {
		return raw, nil
	}

	decoded, err := decompressRequestBody(enc, raw)
	if err != nil {
		return nil, err
	}

	req.Header.Del("Content-Encoding")
	req.Header.Del("Content-Length")
	req.ContentLength = int64(len(decoded))

	return decoded, nil
}

// ReadLenientJSONRequestBodyWithPrealloc reads a request body and normalizes
// JSON string control bytes before strict validation.
func ReadLenientJSONRequestBodyWithPrealloc(req *http.Request, maxNormalizedBytes int64) ([]byte, error) {
	body, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		return nil, err
	}
	return NormalizeLenientJSONRequestBody(body, maxNormalizedBytes)
}

func decompressRequestBody(encoding string, raw []byte) ([]byte, error) {
	switch encoding {
	case "zstd":
		dec, err := zstd.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, &MalformedCompressedRequestBodyError{Encoding: encoding, Err: err}
		}
		defer dec.Close()
		return readLimitedDecodedBody(encoding, dec)
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, &MalformedCompressedRequestBodyError{Encoding: encoding, Err: err}
		}
		defer func() { _ = gr.Close() }()
		return readLimitedDecodedBody(encoding, gr)
	case "deflate":
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, &MalformedCompressedRequestBodyError{Encoding: encoding, Err: err}
		}
		defer func() { _ = zr.Close() }()
		return readLimitedDecodedBody(encoding, zr)
	default:
		return nil, &UnsupportedContentEncodingError{Encoding: encoding}
	}
}

func readLimitedDecodedBody(encoding string, r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxDecompressedBodySize+1))
	if err != nil {
		return nil, &MalformedCompressedRequestBodyError{Encoding: encoding, Err: err}
	}
	if int64(len(body)) > maxDecompressedBodySize {
		return nil, &RequestBodyTooLargeError{Limit: maxDecompressedBodySize}
	}
	return body, nil
}

// NormalizeLenientJSONRequestBody escapes raw control bytes that broken
// OpenAI-compatible clients sometimes place inside JSON strings.
func NormalizeLenientJSONRequestBody(body []byte, maxNormalizedBytes int64) ([]byte, error) {
	if maxNormalizedBytes <= 0 {
		maxNormalizedBytes = maxDecompressedBodySize
	}

	body = trimUTF8BOM(body)
	if len(body) == 0 {
		return body, nil
	}
	if int64(len(body)) > maxNormalizedBytes {
		return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
	}

	var out []byte
	inString := false
	escaped := false
	for i, b := range body {
		if inString && isJSONControlByte(b) {
			if out == nil {
				capHint := len(body) + 6
				if int64(capHint) > maxNormalizedBytes {
					capHint = int(maxNormalizedBytes)
				}
				out = make([]byte, 0, capHint)
				out = append(out, body[:i]...)
			}
			if int64(len(out)+6) > maxNormalizedBytes {
				return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
			}
			out = appendJSONUnicodeEscape(out, b)
			escaped = false
			continue
		}

		switch {
		case escaped:
			escaped = false
		case inString && b == '\\':
			escaped = true
		case b == '"':
			inString = !inString
		}

		if out != nil {
			if int64(len(out)+1) > maxNormalizedBytes {
				return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
			}
			out = append(out, b)
		}
	}
	if out != nil {
		return out, nil
	}
	return body, nil
}

func trimUTF8BOM(body []byte) []byte {
	if len(body) >= jsonUTF8BOMLen && body[0] == 0xef && body[1] == 0xbb && body[2] == 0xbf {
		return body[jsonUTF8BOMLen:]
	}
	return body
}

func isJSONControlByte(b byte) bool {
	return b < 0x20 || b == 0x7f
}

func appendJSONUnicodeEscape(dst []byte, b byte) []byte {
	const hex = "0123456789abcdef"
	return append(dst, '\\', 'u', '0', '0', hex[b>>4], hex[b&0x0f])
}
