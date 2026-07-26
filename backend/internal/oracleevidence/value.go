package oracleevidence

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
)

func objectValue(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}

func arrayValue(value any) ([]any, bool) {
	result, ok := value.([]any)
	return result, ok
}

func stringValue(value any) (string, bool) {
	result, ok := value.(string)
	return result, ok
}

func boolValue(value any) (bool, bool) {
	result, ok := value.(bool)
	return result, ok
}

func int64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		return parsed, err == nil && parsed >= -maxSafeInteger && parsed <= maxSafeInteger
	case int64:
		return typed, typed >= -maxSafeInteger && typed <= maxSafeInteger
	case uint64:
		if typed > maxSafeInteger {
			return 0, false
		}
		return int64(typed), true
	case float64:
		if math.Trunc(typed) != typed || typed < -maxSafeInteger || typed > maxSafeInteger {
			return 0, false
		}
		return int64(typed), true
	default:
		return 0, false
	}
}

func uint64Value(value any) (uint64, bool) {
	parsed, ok := int64Value(value)
	if !ok || parsed < 0 {
		return 0, false
	}
	return uint64(parsed), true
}

func exactKeys(value map[string]any, required ...string) bool {
	if len(value) != len(required) {
		return false
	}
	want := append([]string(nil), required...)
	sort.Strings(want)
	actual := make([]string, 0, len(value))
	for key := range value {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	for index := range want {
		if want[index] != actual[index] {
			return false
		}
	}
	return true
}

func stringArray(value any, maximum int) ([]string, bool) {
	raw, ok := arrayValue(value)
	if !ok || len(raw) > maximum {
		return nil, false
	}
	result := make([]string, len(raw))
	seen := make(map[string]bool, len(raw))
	for index, item := range raw {
		text, textOK := stringValue(item)
		if !textOK || seen[text] {
			return nil, false
		}
		seen[text] = true
		result[index] = text
	}
	return result, true
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func decisionFromError(err error, fallback string) Decision {
	if contract, ok := err.(*ContractError); ok {
		return Decision{Code: contract.Code, Detail: boundedDetail(contract.Detail)}
	}
	return Decision{Code: fallback}
}
