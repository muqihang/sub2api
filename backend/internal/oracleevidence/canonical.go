package oracleevidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func canonicalizeJSONImpl(input []byte) ([]byte, error) {
	value, err := parseStrictJSONImpl(input)
	if err != nil {
		return nil, err
	}
	return canonicalizeValueImpl(value)
}

func canonicalizeValueImpl(value any) ([]byte, error) {
	if err := validateJSONValueImpl(value); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := appendCanonicalJSON(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appendCanonicalJSON(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		appendJSONString(output, typed)
	case json.Number:
		formatted, err := canonicalNumber(string(typed))
		if err != nil {
			return err
		}
		output.WriteString(formatted)
	case float64:
		formatted, err := canonicalFloat(typed)
		if err != nil {
			return err
		}
		output.WriteString(formatted)
	case float32:
		formatted, err := canonicalFloat(float64(typed))
		if err != nil {
			return err
		}
		output.WriteString(formatted)
	case int:
		output.WriteString(strconv.FormatInt(int64(typed), 10))
	case int8:
		output.WriteString(strconv.FormatInt(int64(typed), 10))
	case int16:
		output.WriteString(strconv.FormatInt(int64(typed), 10))
	case int32:
		output.WriteString(strconv.FormatInt(int64(typed), 10))
	case int64:
		output.WriteString(strconv.FormatInt(typed, 10))
	case uint:
		output.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint8:
		output.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint16:
		output.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint32:
		output.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint64:
		output.WriteString(strconv.FormatUint(typed, 10))
	case []any:
		output.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalJSON(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case []string:
		items := make([]any, len(typed))
		for index := range typed {
			items[index] = typed[index]
		}
		return appendCanonicalJSON(output, items)
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			appendJSONString(output, key)
			output.WriteByte(':')
			if err := appendCanonicalJSON(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return jsonContractError(CodeJSONTypeInvalid, "unsupported Go value")
	}
	return nil
}

func appendJSONString(output *bytes.Buffer, value string) {
	const hexDigits = "0123456789abcdef"
	output.WriteByte('"')
	for _, current := range []byte(value) {
		switch current {
		case '"', '\\':
			output.WriteByte('\\')
			output.WriteByte(current)
		case '\b':
			output.WriteString(`\b`)
		case '\t':
			output.WriteString(`\t`)
		case '\n':
			output.WriteString(`\n`)
		case '\f':
			output.WriteString(`\f`)
		case '\r':
			output.WriteString(`\r`)
		default:
			if current < 0x20 {
				output.WriteString(`\u00`)
				output.WriteByte(hexDigits[current>>4])
				output.WriteByte(hexDigits[current&0x0f])
			} else {
				output.WriteByte(current)
			}
		}
	}
	output.WriteByte('"')
}

func utf16Less(left, right string) bool {
	l := utf16.Encode([]rune(left))
	r := utf16.Encode([]rune(right))
	for index := 0; index < len(l) && index < len(r); index++ {
		if l[index] != r[index] {
			return l[index] < r[index]
		}
	}
	return len(l) < len(r)
}

func canonicalNumber(token string) (string, error) {
	value, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return "", jsonContractError("json_number_invalid", "number domain")
	}
	return canonicalFloat(value)
}

func canonicalFloat(value float64) (string, error) {
	if err := validateFloat(value); err != nil {
		return "", err
	}
	if value == 0 {
		return "0", nil
	}
	negative := value < 0
	absolute := math.Abs(value)
	scientific := strconv.FormatFloat(absolute, 'e', -1, 64)
	parts := strings.Split(scientific, "e")
	if len(parts) != 2 {
		return "", jsonContractError("json_canonicalization_failed", "number formatting")
	}
	exponent, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", jsonContractError("json_canonicalization_failed", "number exponent")
	}
	digits := strings.ReplaceAll(parts[0], ".", "")
	var encoded string
	if absolute >= 1e-6 && absolute < 1e21 {
		decimalPosition := exponent + 1
		switch {
		case decimalPosition <= 0:
			encoded = "0." + strings.Repeat("0", -decimalPosition) + digits
		case decimalPosition >= len(digits):
			encoded = digits + strings.Repeat("0", decimalPosition-len(digits))
		default:
			encoded = digits[:decimalPosition] + "." + digits[decimalPosition:]
		}
	} else {
		encoded = digits[:1]
		if len(digits) > 1 {
			encoded += "." + digits[1:]
		}
		if exponent >= 0 {
			encoded += "e+" + strconv.Itoa(exponent)
		} else {
			encoded += "e" + strconv.Itoa(exponent)
		}
	}
	if negative {
		return "-" + encoded, nil
	}
	return encoded, nil
}

func normalizePathQueryImpl(pathname string, pairs [][2]string) (string, error) {
	if pathname == "" || !strings.HasPrefix(pathname, "/") || !utf8.ValidString(pathname) {
		return "", contractErr("url_path_invalid")
	}
	for _, current := range []byte(pathname) {
		if current < 0x20 || current == 0x7f || current == '?' || current == '#' {
			return "", contractErr("url_path_invalid")
		}
	}
	type queryPair struct {
		key   string
		value string
		index int
	}
	ordered := make([]queryPair, len(pairs))
	for index, pair := range pairs {
		if !utf8.ValidString(pair[0]) || !utf8.ValidString(pair[1]) {
			return "", contractErr("url_path_invalid")
		}
		ordered[index] = queryPair{key: pair[0], value: pair[1], index: index}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		comparison := bytes.Compare([]byte(ordered[i].key), []byte(ordered[j].key))
		if comparison != 0 {
			return comparison < 0
		}
		return ordered[i].index < ordered[j].index
	})
	if len(ordered) == 0 {
		return pathname, nil
	}
	var output strings.Builder
	output.WriteString(pathname)
	output.WriteByte('?')
	for index, pair := range ordered {
		if index > 0 {
			output.WriteByte('&')
		}
		output.WriteString(percentEncodeComponent(pair.key))
		output.WriteByte('=')
		output.WriteString(percentEncodeComponent(pair.value))
	}
	return output.String(), nil
}

func percentEncodeComponent(value string) string {
	const upperHex = "0123456789ABCDEF"
	var output strings.Builder
	for _, current := range []byte(value) {
		if (current >= 'a' && current <= 'z') || (current >= 'A' && current <= 'Z') ||
			(current >= '0' && current <= '9') || current == '-' || current == '_' || current == '.' || current == '~' {
			output.WriteByte(current)
			continue
		}
		output.WriteByte('%')
		output.WriteByte(upperHex[current>>4])
		output.WriteByte(upperHex[current&0x0f])
	}
	return output.String()
}

func parseAuthorityPortImpl(raw RawPort) (uint16, error) {
	value := string(raw)
	if len(value) == 0 || len(value) > 5 || value[0] == '0' {
		return 0, contractErr(CodeURLPortInvalid)
	}
	var parsed uint32
	for _, current := range []byte(value) {
		if current < '0' || current > '9' {
			return 0, contractErr(CodeURLPortInvalid)
		}
		parsed = parsed*10 + uint32(current-'0')
		if parsed > 65_535 {
			return 0, contractErr(CodeURLPortInvalid)
		}
	}
	return uint16(parsed), nil
}

func formatAuthorityImpl(host string, rawPort RawPort) (string, error) {
	port, err := parseAuthorityPortImpl(rawPort)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(host) || host == "" {
		return "", contractErr(CodeURLHostInvalid)
	}
	parsed := net.ParseIP(host)
	if parsed != nil {
		if strings.Contains(host, ":") {
			return "[" + strings.ToLower(host) + "]:" + strconv.Itoa(int(port)), nil
		}
		return strings.ToLower(host) + ":" + strconv.Itoa(int(port)), nil
	}
	for _, current := range []byte(host) {
		if !((current >= 'a' && current <= 'z') || (current >= 'A' && current <= 'Z') ||
			(current >= '0' && current <= '9') || current == '.' || current == '-') {
			return "", contractErr(CodeURLHostInvalid)
		}
	}
	return strings.ToLower(host) + ":" + strconv.Itoa(int(port)), nil
}

func sha256HexImpl(input []byte) string {
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:])
}
