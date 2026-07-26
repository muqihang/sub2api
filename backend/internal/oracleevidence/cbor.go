package oracleevidence

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"unicode/utf8"
)

const (
	maxCBORFrameBytes = 65_536
	maxCBORDepth      = 32
	maxCBORArrayItems = 4_096
	maxCBORMapPairs   = 1_024
)

type cborDecoder struct {
	data []byte
}

func canonicalizeCBORImpl(input []byte) ([]byte, error) {
	value, err := decodeDeterministicCBORImpl(input)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeDeterministicCBOR(value)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(encoded, input) {
		return nil, cborError("cbor_not_deterministic")
	}
	return encoded, nil
}

func decodeDeterministicCBORImpl(input []byte) (any, error) {
	if len(input) == 0 {
		return nil, cborError("cbor_truncated")
	}
	decoder := cborDecoder{data: input}
	value, offset, _, err := decoder.decodeItem(0, 0)
	if err != nil {
		return nil, err
	}
	if offset != len(input) {
		return nil, cborError("cbor_trailing_data")
	}
	return value, nil
}

func encodeCBORFrameImpl(value any) ([]byte, error) {
	payload, err := encodeDeterministicCBOR(value)
	if err != nil {
		return nil, err
	}
	return frameCBOR(payload)
}

func decodeCBORFrameImpl(input []byte) (any, error) {
	payload, err := unframeCBOR(input)
	if err != nil {
		return nil, err
	}
	return decodeDeterministicCBORImpl(payload)
}

func frameCBOR(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > maxCBORFrameBytes {
		return nil, cborError("cbor_frame_length")
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	return frame, nil
}

func unframeCBOR(frame []byte) ([]byte, error) {
	if len(frame) < 4 {
		return nil, cborError("cbor_frame_truncated")
	}
	length := uint64(binary.BigEndian.Uint32(frame[:4]))
	if length == 0 || length > maxCBORFrameBytes {
		return nil, cborError("cbor_frame_length")
	}
	if length > uint64(len(frame)-4) {
		return nil, cborError("cbor_frame_truncated")
	}
	if length != uint64(len(frame)-4) {
		return nil, cborError("cbor_trailing_data")
	}
	return append([]byte(nil), frame[4:]...), nil
}

func encodeDeterministicCBOR(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := appendDeterministicCBOR(&output, value, 0); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appendDeterministicCBOR(output *bytes.Buffer, value any, depth int) error {
	if depth > maxCBORDepth {
		return cborError("cbor_resource_limit")
	}
	switch typed := value.(type) {
	case nil:
		return output.WriteByte(0xf6)
	case bool:
		if typed {
			return output.WriteByte(0xf5)
		}
		return output.WriteByte(0xf4)
	case string:
		if !utf8.ValidString(typed) {
			return cborError("cbor_invalid_utf8")
		}
		if err := appendCBORArgument(output, 3, uint64(len([]byte(typed)))); err != nil {
			return err
		}
		_, err := output.WriteString(typed)
		return err
	case []byte:
		if err := appendCBORArgument(output, 2, uint64(len(typed))); err != nil {
			return err
		}
		_, err := output.Write(typed)
		return err
	case json.Number:
		integer, err := strconv.ParseInt(string(typed), 10, 64)
		if err != nil {
			return cborError("cbor_type_invalid")
		}
		return appendCBORInteger(output, integer)
	case int:
		return appendCBORInteger(output, int64(typed))
	case int8:
		return appendCBORInteger(output, int64(typed))
	case int16:
		return appendCBORInteger(output, int64(typed))
	case int32:
		return appendCBORInteger(output, int64(typed))
	case int64:
		return appendCBORInteger(output, typed)
	case uint:
		return appendCBORUnsigned(output, uint64(typed))
	case uint8:
		return appendCBORUnsigned(output, uint64(typed))
	case uint16:
		return appendCBORUnsigned(output, uint64(typed))
	case uint32:
		return appendCBORUnsigned(output, uint64(typed))
	case uint64:
		return appendCBORUnsigned(output, typed)
	case float32, float64:
		return cborError("cbor_float_forbidden")
	case []any:
		if len(typed) > maxCBORArrayItems {
			return cborError("cbor_resource_limit")
		}
		if err := appendCBORArgument(output, 4, uint64(len(typed))); err != nil {
			return err
		}
		for _, item := range typed {
			if err := appendDeterministicCBOR(output, item, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		if len(typed) > maxCBORMapPairs {
			return cborError("cbor_resource_limit")
		}
		type encodedEntry struct {
			key   []byte
			value any
		}
		entries := make([]encodedEntry, 0, len(typed))
		for key, item := range typed {
			var encodedKey bytes.Buffer
			if err := appendDeterministicCBOR(&encodedKey, key, depth+1); err != nil {
				return err
			}
			entries = append(entries, encodedEntry{key: encodedKey.Bytes(), value: item})
		}
		sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].key, entries[j].key) < 0 })
		if err := appendCBORArgument(output, 5, uint64(len(entries))); err != nil {
			return err
		}
		for _, entry := range entries {
			if _, err := output.Write(entry.key); err != nil {
				return err
			}
			if err := appendDeterministicCBOR(output, entry.value, depth+1); err != nil {
				return err
			}
		}
	default:
		return cborError("cbor_type_invalid")
	}
	return nil
}

func appendCBORInteger(output *bytes.Buffer, value int64) error {
	if value >= 0 {
		return appendCBORUnsigned(output, uint64(value))
	}
	magnitude := uint64(-(value + 1))
	if magnitude > maxSafeInteger {
		return cborError("cbor_integer_unsafe")
	}
	return appendCBORArgument(output, 1, magnitude)
}

func appendCBORUnsigned(output *bytes.Buffer, value uint64) error {
	if value > maxSafeInteger {
		return cborError("cbor_integer_unsafe")
	}
	return appendCBORArgument(output, 0, value)
}

func appendCBORArgument(output *bytes.Buffer, major byte, value uint64) error {
	prefix := major << 5
	switch {
	case value < 24:
		return output.WriteByte(prefix | byte(value))
	case value <= math.MaxUint8:
		if err := output.WriteByte(prefix | 24); err != nil {
			return err
		}
		return output.WriteByte(byte(value))
	case value <= math.MaxUint16:
		if err := output.WriteByte(prefix | 25); err != nil {
			return err
		}
		var buffer [2]byte
		binary.BigEndian.PutUint16(buffer[:], uint16(value))
		_, err := output.Write(buffer[:])
		return err
	case value <= math.MaxUint32:
		if err := output.WriteByte(prefix | 26); err != nil {
			return err
		}
		var buffer [4]byte
		binary.BigEndian.PutUint32(buffer[:], uint32(value))
		_, err := output.Write(buffer[:])
		return err
	default:
		if err := output.WriteByte(prefix | 27); err != nil {
			return err
		}
		var buffer [8]byte
		binary.BigEndian.PutUint64(buffer[:], value)
		_, err := output.Write(buffer[:])
		return err
	}
}

func (decoder cborDecoder) decodeItem(offset, depth int) (any, int, []byte, error) {
	if depth > maxCBORDepth {
		return nil, offset, nil, cborError("cbor_resource_limit")
	}
	if offset >= len(decoder.data) {
		return nil, offset, nil, cborError("cbor_truncated")
	}
	start := offset
	initial := decoder.data[offset]
	major := initial >> 5
	additional := initial & 0x1f
	offset++
	if major == 6 {
		return nil, offset, nil, cborError("cbor_tag_forbidden")
	}
	if major == 7 {
		switch additional {
		case 20:
			return false, offset, decoder.data[start:offset], nil
		case 21:
			return true, offset, decoder.data[start:offset], nil
		case 22:
			return nil, offset, decoder.data[start:offset], nil
		case 23:
			return nil, offset, nil, cborError("cbor_undefined_forbidden")
		case 25, 26, 27:
			return nil, offset, nil, cborError("cbor_float_forbidden")
		default:
			return nil, offset, nil, cborError("cbor_simple_forbidden")
		}
	}
	argument, next, err := decoder.readArgument(offset, additional)
	if err != nil {
		return nil, offset, nil, err
	}
	offset = next
	switch major {
	case 0:
		return argument, offset, decoder.data[start:offset], nil
	case 1:
		if argument > math.MaxInt64 {
			return nil, offset, nil, cborError("cbor_integer_unsafe")
		}
		return -1 - int64(argument), offset, decoder.data[start:offset], nil
	case 2, 3:
		if argument > uint64(len(decoder.data)-offset) {
			return nil, offset, nil, cborError("cbor_truncated")
		}
		end := offset + int(argument)
		content := decoder.data[offset:end]
		if major == 2 {
			return append([]byte(nil), content...), end, decoder.data[start:end], nil
		}
		if !utf8.Valid(content) {
			return nil, offset, nil, cborError("cbor_invalid_utf8")
		}
		return string(content), end, decoder.data[start:end], nil
	case 4:
		if argument > maxCBORArrayItems {
			return nil, offset, nil, cborError("cbor_resource_limit")
		}
		items := make([]any, 0, argument)
		for index := uint64(0); index < argument; index++ {
			item, following, _, itemErr := decoder.decodeItem(offset, depth+1)
			if itemErr != nil {
				return nil, offset, nil, itemErr
			}
			items = append(items, item)
			offset = following
		}
		return items, offset, decoder.data[start:offset], nil
	case 5:
		if argument > maxCBORMapPairs {
			return nil, offset, nil, cborError("cbor_resource_limit")
		}
		result := make(map[string]any, argument)
		var previous []byte
		for index := uint64(0); index < argument; index++ {
			keyValue, following, encodedKey, keyErr := decoder.decodeItem(offset, depth+1)
			if keyErr != nil {
				return nil, offset, nil, keyErr
			}
			key, ok := keyValue.(string)
			if !ok {
				return nil, offset, nil, cborError("cbor_map_key_invalid")
			}
			if _, exists := result[key]; exists {
				return nil, offset, nil, cborError("cbor_duplicate_key")
			}
			if previous != nil && bytes.Compare(previous, encodedKey) >= 0 {
				return nil, offset, nil, cborError("cbor_not_deterministic")
			}
			previous = append(previous[:0], encodedKey...)
			value, valueEnd, _, valueErr := decoder.decodeItem(following, depth+1)
			if valueErr != nil {
				return nil, offset, nil, valueErr
			}
			result[key] = value
			offset = valueEnd
		}
		return result, offset, decoder.data[start:offset], nil
	default:
		return nil, offset, nil, cborError(CodeCBORInvalid)
	}
}

func (decoder cborDecoder) readArgument(offset int, additional byte) (uint64, int, error) {
	if additional < 24 {
		return uint64(additional), offset, nil
	}
	if additional == 31 {
		return 0, offset, cborError("cbor_indefinite_length")
	}
	width := 0
	switch additional {
	case 24:
		width = 1
	case 25:
		width = 2
	case 26:
		width = 4
	case 27:
		width = 8
	default:
		return 0, offset, cborError(CodeCBORInvalid)
	}
	if width > len(decoder.data)-offset {
		return 0, offset, cborError("cbor_truncated")
	}
	var value uint64
	for index := 0; index < width; index++ {
		value = value<<8 | uint64(decoder.data[offset+index])
	}
	minimum := uint64(24)
	if width == 2 {
		minimum = 256
	} else if width == 4 {
		minimum = 65_536
	} else if width == 8 {
		minimum = 4_294_967_296
	}
	if value < minimum {
		return 0, offset, cborError("cbor_not_deterministic")
	}
	if value > maxSafeInteger {
		return 0, offset, cborError("cbor_integer_unsafe")
	}
	return value, offset + width, nil
}

func cborError(code string) error { return &ContractError{Code: code} }
