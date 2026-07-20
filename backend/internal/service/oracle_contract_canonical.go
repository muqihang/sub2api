package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gowebpki/jcs"
)

const oracleJSONMaxSafeInteger = 9007199254740991

type OracleContractError struct {
	Code string
	Msg  string
}

func (e *OracleContractError) Error() string {
	return e.Code + ": " + e.Msg
}

func oracleContractError(code, format string, args ...any) error {
	return &OracleContractError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

func OracleContractErrorCode(err error) string {
	if typed, ok := err.(*OracleContractError); ok {
		return typed.Code
	}
	return ""
}

type OracleCanonicalJSON struct {
	Canonical []byte
	SHA256    string
}

func hex4(raw []byte) (uint16, bool) {
	if len(raw) < 4 {
		return 0, false
	}
	value, err := strconv.ParseUint(string(raw[:4]), 16, 16)
	return uint16(value), err == nil
}

func validateOracleJSONEscapes(raw []byte) error {
	inString := false
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || i+1 >= len(raw) {
				continue
			}
			i++
			if raw[i] != 'u' {
				continue
			}
			if i+4 >= len(raw) {
				return oracleContractError("json_invalid", "truncated unicode escape")
			}
			first, ok := hex4(raw[i+1 : i+5])
			if !ok {
				return oracleContractError("json_invalid", "invalid unicode escape")
			}
			i += 4
			if first >= 0xd800 && first <= 0xdbff {
				if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
					return oracleContractError("json_lone_surrogate", "high surrogate is not paired")
				}
				second, ok := hex4(raw[i+3 : i+7])
				if !ok || second < 0xdc00 || second > 0xdfff || utf16.DecodeRune(rune(first), rune(second)) == utf8.RuneError {
					return oracleContractError("json_lone_surrogate", "high surrogate is not paired with a low surrogate")
				}
				i += 6
			} else if first >= 0xdc00 && first <= 0xdfff {
				return oracleContractError("json_lone_surrogate", "low surrogate has no high surrogate")
			}
		}
	}
	return nil
}

func validateOracleJSONNumber(number json.Number) error {
	value, err := strconv.ParseFloat(string(number), 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return oracleContractError("json_number_invalid", "invalid JSON number %q", number)
	}
	if value == 0 && math.Signbit(value) {
		return oracleContractError("json_negative_zero", "negative zero is forbidden")
	}
	if math.Trunc(value) == value && math.Abs(value) > oracleJSONMaxSafeInteger {
		return oracleContractError("json_number_unsafe", "integer exceeds the I-JSON safe range")
	}
	return nil
}

func validateOracleJSONValue(decoder *json.Decoder, location string) error {
	token, err := decoder.Token()
	if err != nil {
		return oracleContractError("json_invalid", "decode %s: %v", location, err)
	}
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return oracleContractError("json_invalid", "decode object key: %v", err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return oracleContractError("json_invalid", "object key is not a string")
				}
				if _, exists := seen[key]; exists {
					return oracleContractError("json_duplicate_key", "duplicate key %q", key)
				}
				seen[key] = struct{}{}
				if err := validateOracleJSONValue(decoder, location+"."+key); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return oracleContractError("json_invalid", "object is not closed")
			}
		case '[':
			index := 0
			for decoder.More() {
				if err := validateOracleJSONValue(decoder, fmt.Sprintf("%s[%d]", location, index)); err != nil {
					return err
				}
				index++
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return oracleContractError("json_invalid", "array is not closed")
			}
		default:
			return oracleContractError("json_invalid", "unexpected delimiter")
		}
	case json.Number:
		return validateOracleJSONNumber(typed)
	case string, bool, nil:
		return nil
	default:
		return oracleContractError("json_invalid", "unsupported JSON token")
	}
	return nil
}

func CanonicalizeOracleJSON(raw []byte) (OracleCanonicalJSON, error) {
	if !utf8.Valid(raw) {
		return OracleCanonicalJSON{}, oracleContractError("json_invalid_utf8", "JSON input is not valid UTF-8")
	}
	if err := validateOracleJSONEscapes(raw); err != nil {
		return OracleCanonicalJSON{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := validateOracleJSONValue(decoder, "$"); err != nil {
		return OracleCanonicalJSON{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return OracleCanonicalJSON{}, oracleContractError("json_trailing_data", "JSON input contains trailing data")
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return OracleCanonicalJSON{}, oracleContractError("json_canonicalization_failed", "%v", err)
	}
	digest := sha256.Sum256(canonical)
	return OracleCanonicalJSON{Canonical: canonical, SHA256: hex.EncodeToString(digest[:])}, nil
}

func oracleQueryEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func NormalizeOraclePathQuery(pathname string, pairs [][2]string) string {
	type indexedPair struct {
		Pair  [2]string
		Index int
	}
	ordered := make([]indexedPair, len(pairs))
	for index, pair := range pairs {
		ordered[index] = indexedPair{Pair: pair, Index: index}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return bytes.Compare([]byte(ordered[i].Pair[0]), []byte(ordered[j].Pair[0])) < 0
	})
	if len(ordered) == 0 {
		return pathname
	}
	encoded := make([]string, len(ordered))
	for index, pair := range ordered {
		encoded[index] = oracleQueryEscape(pair.Pair[0]) + "=" + oracleQueryEscape(pair.Pair[1])
	}
	return pathname + "?" + strings.Join(encoded, "&")
}

func FormatOracleAuthority(host string, port int) string {
	if ip := net.ParseIP(host); ip != nil {
		return net.JoinHostPort(strings.ToLower(host), strconv.Itoa(port))
	}
	return strings.ToLower(host) + ":" + strconv.Itoa(port)
}
