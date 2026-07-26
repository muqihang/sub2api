package oracleevidence

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"unicode/utf8"
)

const (
	maxJSONBytes   = 1 << 20
	maxJSONDepth   = 256
	maxJSONMembers = 65_536
	maxJSONString  = 8 << 10
	maxSafeInteger = 9_007_199_254_740_991
)

type strictJSONParser struct {
	data    []byte
	offset  int
	depth   int
	members int
}

func parseStrictJSONImpl(input []byte) (any, error) {
	if len(input) == 0 || len(input) > maxJSONBytes {
		return nil, jsonContractError(CodeJSONInvalid, "input size")
	}
	if !utf8.Valid(input) {
		return nil, jsonContractError("json_invalid_utf8", "input encoding")
	}
	if len(input) >= 3 && input[0] == 0xef && input[1] == 0xbb && input[2] == 0xbf {
		return nil, jsonContractError(CodeJSONInvalid, "BOM")
	}
	p := strictJSONParser{data: input}
	p.skipSpace()
	value, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.offset != len(p.data) {
		return nil, jsonContractError("json_trailing_data", "trailing value")
	}
	return value, nil
}

func validateJSONValueImpl(value any) error {
	count := 0
	return validateJSONAt(value, 0, &count)
}

func validateJSONAt(value any, depth int, count *int) error {
	if depth > maxJSONDepth {
		return jsonContractError(CodeJSONInvalid, "nesting")
	}
	switch typed := value.(type) {
	case nil, bool:
		return nil
	case string:
		if !utf8.ValidString(typed) {
			return jsonContractError("json_invalid_utf8", "string")
		}
		if hasLoneSurrogateString(typed) {
			return jsonContractError("json_lone_surrogate", "string")
		}
		if len([]byte(typed)) > maxJSONString {
			return jsonContractError(CodeJSONInvalid, "string length")
		}
		return nil
	case json.Number:
		_, err := parseJSONNumber(string(typed))
		return err
	case float64:
		return validateFloat(typed)
	case float32:
		return validateFloat(float64(typed))
	case int:
		return validateSignedInteger(int64(typed))
	case int8:
		return validateSignedInteger(int64(typed))
	case int16:
		return validateSignedInteger(int64(typed))
	case int32:
		return validateSignedInteger(int64(typed))
	case int64:
		return validateSignedInteger(typed)
	case uint:
		return validateUnsignedInteger(uint64(typed))
	case uint8:
		return validateUnsignedInteger(uint64(typed))
	case uint16:
		return validateUnsignedInteger(uint64(typed))
	case uint32:
		return validateUnsignedInteger(uint64(typed))
	case uint64:
		return validateUnsignedInteger(typed)
	case []any:
		*count += len(typed)
		if *count > maxJSONMembers {
			return jsonContractError(CodeJSONInvalid, "aggregate members")
		}
		for _, item := range typed {
			if err := validateJSONAt(item, depth+1, count); err != nil {
				return err
			}
		}
		return nil
	case []string:
		*count += len(typed)
		if *count > maxJSONMembers {
			return jsonContractError(CodeJSONInvalid, "aggregate members")
		}
		for _, item := range typed {
			if err := validateJSONAt(item, depth+1, count); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		*count += len(typed)
		if *count > maxJSONMembers {
			return jsonContractError(CodeJSONInvalid, "aggregate members")
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if !utf8.ValidString(key) {
				return jsonContractError("json_invalid_utf8", "object key")
			}
			if hasLoneSurrogateString(key) {
				return jsonContractError("json_lone_surrogate", "object key")
			}
			if len([]byte(key)) > maxJSONString {
				return jsonContractError(CodeJSONInvalid, "object key length")
			}
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		for _, key := range keys {
			if err := validateJSONAt(typed[key], depth+1, count); err != nil {
				return err
			}
		}
		return nil
	default:
		return jsonContractError(CodeJSONTypeInvalid, "unsupported Go value")
	}
}

func validateSignedInteger(value int64) error {
	if value < -maxSafeInteger || value > maxSafeInteger {
		return jsonContractError("json_number_unsafe", "integer range")
	}
	return nil
}

func validateUnsignedInteger(value uint64) error {
	if value > maxSafeInteger {
		return jsonContractError("json_number_unsafe", "integer range")
	}
	return nil
}

func validateFloat(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return jsonContractError("json_number_invalid", "non-finite")
	}
	if value == 0 && math.Signbit(value) {
		return jsonContractError("json_negative_zero", "negative zero")
	}
	if math.Trunc(value) == value && math.Abs(value) > maxSafeInteger {
		return jsonContractError("json_number_unsafe", "integer range")
	}
	return nil
}

func jsonContractError(code, detail string) error {
	return &ContractError{Code: code, Detail: boundedDetail(detail)}
}

func boundedDetail(detail string) string {
	if len(detail) > 200 {
		return detail[:200]
	}
	return detail
}

func (p *strictJSONParser) parseValue() (any, error) {
	if p.offset >= len(p.data) {
		return nil, jsonContractError(CodeJSONInvalid, "unexpected end")
	}
	switch p.data[p.offset] {
	case '{':
		return p.parseObject()
	case '[':
		return p.parseArray()
	case '"':
		return p.parseString()
	case 't':
		if p.consumeLiteral("true") {
			return true, nil
		}
	case 'f':
		if p.consumeLiteral("false") {
			return false, nil
		}
	case 'n':
		if p.consumeLiteral("null") {
			return nil, nil
		}
	default:
		if p.data[p.offset] == '-' || (p.data[p.offset] >= '0' && p.data[p.offset] <= '9') {
			return p.parseNumber()
		}
	}
	return nil, jsonContractError(CodeJSONInvalid, "invalid token")
}

func (p *strictJSONParser) parseObject() (any, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	p.offset++
	p.skipSpace()
	result := make(map[string]any)
	if p.take('}') {
		return result, nil
	}
	for {
		if p.offset >= len(p.data) || p.data[p.offset] != '"' {
			return nil, jsonContractError(CodeJSONInvalid, "object key")
		}
		keyValue, err := p.parseString()
		if err != nil {
			return nil, err
		}
		key := keyValue.(string)
		if _, exists := result[key]; exists {
			return nil, jsonContractError("json_duplicate_key", "duplicate object key")
		}
		p.skipSpace()
		if !p.take(':') {
			return nil, jsonContractError(CodeJSONInvalid, "object separator")
		}
		p.skipSpace()
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		result[key] = value
		p.members++
		if p.members > maxJSONMembers {
			return nil, jsonContractError(CodeJSONInvalid, "aggregate members")
		}
		p.skipSpace()
		if p.take('}') {
			return result, nil
		}
		if !p.take(',') {
			return nil, jsonContractError(CodeJSONInvalid, "object comma")
		}
		p.skipSpace()
	}
}

func (p *strictJSONParser) parseArray() (any, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	p.offset++
	p.skipSpace()
	result := make([]any, 0)
	if p.take(']') {
		return result, nil
	}
	for {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		result = append(result, value)
		p.members++
		if p.members > maxJSONMembers {
			return nil, jsonContractError(CodeJSONInvalid, "aggregate members")
		}
		p.skipSpace()
		if p.take(']') {
			return result, nil
		}
		if !p.take(',') {
			return nil, jsonContractError(CodeJSONInvalid, "array comma")
		}
		p.skipSpace()
	}
}

func (p *strictJSONParser) parseString() (any, error) {
	start := p.offset
	p.offset++
	for p.offset < len(p.data) {
		current := p.data[p.offset]
		if current == '"' {
			p.offset++
			raw := p.data[start:p.offset]
			if err := validateSurrogateEscapes(raw); err != nil {
				return nil, err
			}
			var decoded string
			if err := json.Unmarshal(raw, &decoded); err != nil {
				return nil, jsonContractError(CodeJSONInvalid, "string escape")
			}
			if len([]byte(decoded)) > maxJSONString {
				return nil, jsonContractError(CodeJSONInvalid, "string length")
			}
			return decoded, nil
		}
		if current < 0x20 {
			return nil, jsonContractError(CodeJSONInvalid, "control in string")
		}
		if current == '\\' {
			p.offset++
			if p.offset >= len(p.data) {
				return nil, jsonContractError(CodeJSONInvalid, "truncated escape")
			}
			if p.data[p.offset] == 'u' {
				if p.offset+4 >= len(p.data) {
					return nil, jsonContractError(CodeJSONInvalid, "truncated unicode escape")
				}
				p.offset += 5
				continue
			}
			if !isSimpleJSONEscape(p.data[p.offset]) {
				return nil, jsonContractError(CodeJSONInvalid, "invalid escape")
			}
		}
		p.offset++
	}
	return nil, jsonContractError(CodeJSONInvalid, "unterminated string")
}

func validateSurrogateEscapes(raw []byte) error {
	for i := 1; i+1 < len(raw); i++ {
		if raw[i] != '\\' || raw[i+1] != 'u' {
			continue
		}
		preceding := 0
		for position := i - 1; position >= 0 && raw[position] == '\\'; position-- {
			preceding++
		}
		if preceding%2 == 1 {
			continue
		}
		if i+6 > len(raw) {
			return jsonContractError(CodeJSONInvalid, "unicode escape")
		}
		first, ok := parseHex4(raw[i+2 : i+6])
		if !ok {
			return jsonContractError(CodeJSONInvalid, "unicode escape")
		}
		if first >= 0xd800 && first <= 0xdbff {
			if i+12 > len(raw) || raw[i+6] != '\\' || raw[i+7] != 'u' {
				return jsonContractError("json_lone_surrogate", "high surrogate")
			}
			second, secondOK := parseHex4(raw[i+8 : i+12])
			if !secondOK || second < 0xdc00 || second > 0xdfff {
				return jsonContractError("json_lone_surrogate", "high surrogate")
			}
			i += 11
			continue
		}
		if first >= 0xdc00 && first <= 0xdfff {
			return jsonContractError("json_lone_surrogate", "low surrogate")
		}
		i += 5
	}
	return nil
}

func parseHex4(raw []byte) (uint16, bool) {
	if len(raw) != 4 {
		return 0, false
	}
	var value uint16
	for _, current := range raw {
		value <<= 4
		switch {
		case current >= '0' && current <= '9':
			value += uint16(current - '0')
		case current >= 'a' && current <= 'f':
			value += uint16(current-'a') + 10
		case current >= 'A' && current <= 'F':
			value += uint16(current-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func (p *strictJSONParser) parseNumber() (any, error) {
	start := p.offset
	if p.take('-') && p.offset >= len(p.data) {
		return nil, jsonContractError(CodeJSONInvalid, "number")
	}
	if p.take('0') {
		if p.offset < len(p.data) && p.data[p.offset] >= '0' && p.data[p.offset] <= '9' {
			return nil, jsonContractError(CodeJSONInvalid, "leading zero")
		}
	} else {
		if p.offset >= len(p.data) || p.data[p.offset] < '1' || p.data[p.offset] > '9' {
			return nil, jsonContractError(CodeJSONInvalid, "number")
		}
		for p.offset < len(p.data) && p.data[p.offset] >= '0' && p.data[p.offset] <= '9' {
			p.offset++
		}
	}
	if p.take('.') {
		if p.offset >= len(p.data) || p.data[p.offset] < '0' || p.data[p.offset] > '9' {
			return nil, jsonContractError(CodeJSONInvalid, "fraction")
		}
		for p.offset < len(p.data) && p.data[p.offset] >= '0' && p.data[p.offset] <= '9' {
			p.offset++
		}
	}
	if p.offset < len(p.data) && (p.data[p.offset] == 'e' || p.data[p.offset] == 'E') {
		p.offset++
		if p.offset < len(p.data) && (p.data[p.offset] == '+' || p.data[p.offset] == '-') {
			p.offset++
		}
		if p.offset >= len(p.data) || p.data[p.offset] < '0' || p.data[p.offset] > '9' {
			return nil, jsonContractError(CodeJSONInvalid, "exponent")
		}
		for p.offset < len(p.data) && p.data[p.offset] >= '0' && p.data[p.offset] <= '9' {
			p.offset++
		}
	}
	return parseJSONNumber(string(p.data[start:p.offset]))
}

func parseJSONNumber(token string) (json.Number, error) {
	value, err := strconv.ParseFloat(token, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", jsonContractError("json_number_invalid", "number domain")
	}
	if value == 0 && len(token) > 0 && token[0] == '-' {
		return "", jsonContractError("json_negative_zero", "negative zero")
	}
	if math.Trunc(value) == value && math.Abs(value) > maxSafeInteger {
		return "", jsonContractError("json_number_unsafe", "integer range")
	}
	return json.Number(token), nil
}

func (p *strictJSONParser) consumeLiteral(literal string) bool {
	if len(p.data)-p.offset < len(literal) || string(p.data[p.offset:p.offset+len(literal)]) != literal {
		return false
	}
	p.offset += len(literal)
	return true
}

func (p *strictJSONParser) skipSpace() {
	for p.offset < len(p.data) {
		switch p.data[p.offset] {
		case ' ', '\t', '\r', '\n':
			p.offset++
		default:
			return
		}
	}
}

func (p *strictJSONParser) take(value byte) bool {
	if p.offset < len(p.data) && p.data[p.offset] == value {
		p.offset++
		return true
	}
	return false
}

func (p *strictJSONParser) enter() error {
	p.depth++
	if p.depth > maxJSONDepth {
		return jsonContractError(CodeJSONInvalid, "nesting")
	}
	return nil
}

func (p *strictJSONParser) leave() { p.depth-- }

func isSimpleJSONEscape(value byte) bool {
	switch value {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return true
	default:
		return false
	}
}

func hasLoneSurrogateString(value string) bool {
	for _, current := range value {
		if current >= 0xd800 && current <= 0xdfff {
			return true
		}
	}
	return false
}
