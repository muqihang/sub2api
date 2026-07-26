package oracleevidence

import (
	_ "embed"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const contractSchemaSHA256 = "380c7f3db80baa2d288838f3a550c3588abd19de11627d34ae90f5d3a0add4fe"

//go:embed testdata/oracle_lab_contract/v1/contract.schema.json
var embeddedContractSchema []byte

var (
	defaultSchemasOnce sync.Once
	defaultSchemas     *SchemaSet
	defaultSchemasErr  error
)

func loadContractSchemaImpl(bundleRoot string) (*SchemaSet, error) {
	if bundleRoot == "" {
		return nil, contractErr(CodeContractBundle)
	}
	path := filepath.Join(bundleRoot, "contract.schema.json")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, contractErr(CodeContractBundle)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, contractErr("contract_symlink")
	}
	if info.Size() < 1 || info.Size() > maxJSONBytes {
		return nil, contractErr("contract_file_digest_mismatch")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, contractErr(CodeContractBundle)
	}
	if sha256HexImpl(data) != contractSchemaSHA256 {
		return nil, contractErr("contract_file_digest_mismatch")
	}
	return schemaSetFromBytes(bundleRoot, data)
}

func defaultSchemaSet() (*SchemaSet, error) {
	defaultSchemasOnce.Do(func() {
		if sha256HexImpl(embeddedContractSchema) != contractSchemaSHA256 {
			defaultSchemasErr = contractErr("contract_file_digest_mismatch")
			return
		}
		defaultSchemas, defaultSchemasErr = schemaSetFromBytes("embedded", embeddedContractSchema)
	})
	return defaultSchemas, defaultSchemasErr
}

func schemaSetFromBytes(bundleRoot string, data []byte) (*SchemaSet, error) {
	value, err := parseStrictJSONImpl(data)
	if err != nil {
		return nil, contractErr("contract_json_invalid")
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, contractErr("contract_schema_invalid")
	}
	nodes := 0
	if err := inspectSchemaNode(root, &nodes); err != nil {
		return nil, err
	}
	return &SchemaSet{bundleRoot: bundleRoot, authoritySHA256: sha256HexImpl(data), root: root}, nil
}

var supportedSchemaKeywords = map[string]bool{
	"$schema": true, "$id": true, "$defs": true, "$ref": true, "title": true, "description": true,
	"oneOf": true, "type": true, "const": true, "enum": true, "required": true,
	"properties": true, "additionalProperties": true, "items": true, "minItems": true,
	"maxItems": true, "uniqueItems": true, "minLength": true, "maxLength": true,
	"pattern": true, "minimum": true, "maximum": true,
}

func inspectSchemaNode(node map[string]any, count *int) error {
	*count++
	if *count > 4_096 {
		return contractErr("contract_schema_keyword_unsupported")
	}
	for key, raw := range node {
		if !supportedSchemaKeywords[key] {
			return contractErr("contract_schema_keyword_unsupported")
		}
		switch key {
		case "$ref":
			ref, ok := raw.(string)
			if !ok || !strings.HasPrefix(ref, "#/$defs/") || strings.Contains(ref, "~") || strings.Contains(ref, "..") {
				return contractErr("contract_schema_keyword_unsupported")
			}
		case "$defs", "properties":
			children, ok := raw.(map[string]any)
			if !ok {
				return contractErr("contract_schema_invalid")
			}
			for _, child := range children {
				childNode, childOK := child.(map[string]any)
				if !childOK {
					return contractErr("contract_schema_invalid")
				}
				if err := inspectSchemaNode(childNode, count); err != nil {
					return err
				}
			}
		case "items":
			child, ok := raw.(map[string]any)
			if !ok {
				return contractErr("contract_schema_invalid")
			}
			if err := inspectSchemaNode(child, count); err != nil {
				return err
			}
		case "oneOf":
			children, ok := raw.([]any)
			if !ok || len(children) == 0 {
				return contractErr("contract_schema_invalid")
			}
			for _, child := range children {
				childNode, childOK := child.(map[string]any)
				if !childOK {
					return contractErr("contract_schema_invalid")
				}
				if err := inspectSchemaNode(childNode, count); err != nil {
					return err
				}
			}
		case "pattern":
			pattern, ok := raw.(string)
			if !ok {
				return contractErr("contract_schema_invalid")
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return contractErr("contract_schema_keyword_unsupported")
			}
		}
	}
	return nil
}

func validateContractObjectImpl(schemas *SchemaSet, definition string, input []byte) Decision {
	if schemas == nil || schemas.root == nil || schemas.authoritySHA256 != contractSchemaSHA256 {
		return Decision{Code: "contract_schema_invalid"}
	}
	value, err := parseStrictJSONImpl(input)
	if err != nil {
		return Decision{Code: "contract_json_invalid"}
	}
	definitions, ok := schemas.root["$defs"].(map[string]any)
	if !ok {
		return Decision{Code: "contract_schema_invalid"}
	}
	schema, ok := definitions[definition].(map[string]any)
	if !ok {
		return Decision{Code: "contract_schema_invalid"}
	}
	if validateSchemaValue(schemas, schema, value, 0) != nil {
		return Decision{Code: "contract_schema_invalid"}
	}
	return Decision{Allowed: true}
}

func validateSchemaValue(schemas *SchemaSet, schema map[string]any, value any, depth int) error {
	if depth > maxJSONDepth {
		return contractErr("contract_schema_invalid")
	}
	if ref, ok := schema["$ref"].(string); ok {
		const prefix = "#/$defs/"
		if !strings.HasPrefix(ref, prefix) {
			return contractErr("contract_schema_keyword_unsupported")
		}
		definitions, _ := schemas.root["$defs"].(map[string]any)
		resolved, found := definitions[strings.TrimPrefix(ref, prefix)].(map[string]any)
		if !found {
			return contractErr("contract_schema_keyword_unsupported")
		}
		return validateSchemaValue(schemas, resolved, value, depth+1)
	}
	if alternatives, ok := schema["oneOf"].([]any); ok {
		matches := 0
		for _, rawAlternative := range alternatives {
			alternative, _ := rawAlternative.(map[string]any)
			if alternative != nil && validateSchemaValue(schemas, alternative, value, depth+1) == nil {
				matches++
			}
		}
		if matches != 1 {
			return contractErr("contract_schema_invalid")
		}
	}
	if rawType, ok := schema["type"]; ok && !schemaTypeMatches(rawType, value) {
		return contractErr("contract_schema_invalid")
	}
	if expected, ok := schema["const"]; ok && !jsonValuesEqual(expected, value) {
		return contractErr("contract_schema_invalid")
	}
	if rawEnum, ok := schema["enum"].([]any); ok {
		found := false
		for _, expected := range rawEnum {
			if jsonValuesEqual(expected, value) {
				found = true
				break
			}
		}
		if !found {
			return contractErr("contract_schema_invalid")
		}
	}
	switch typed := value.(type) {
	case map[string]any:
		properties, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]any); ok {
			for _, rawKey := range required {
				key, keyOK := rawKey.(string)
				if !keyOK {
					return contractErr("contract_schema_invalid")
				}
				if _, present := typed[key]; !present {
					return contractErr("contract_schema_invalid")
				}
			}
		}
		for key, item := range typed {
			rawProperty, present := properties[key]
			if !present {
				if additional, exists := schema["additionalProperties"]; exists && additional == false {
					return contractErr("contract_schema_invalid")
				}
				continue
			}
			property, ok := rawProperty.(map[string]any)
			if !ok || validateSchemaValue(schemas, property, item, depth+1) != nil {
				return contractErr("contract_schema_invalid")
			}
		}
	case []any:
		if minimum, ok := schemaInteger(schema["minItems"]); ok && len(typed) < minimum {
			return contractErr("contract_schema_invalid")
		}
		if maximum, ok := schemaInteger(schema["maxItems"]); ok && len(typed) > maximum {
			return contractErr("contract_schema_invalid")
		}
		if schema["uniqueItems"] == true {
			seen := make(map[string]bool, len(typed))
			for _, item := range typed {
				encoded, err := canonicalizeValueImpl(item)
				if err != nil || seen[string(encoded)] {
					return contractErr("contract_schema_invalid")
				}
				seen[string(encoded)] = true
			}
		}
		if rawItems, ok := schema["items"].(map[string]any); ok {
			for _, item := range typed {
				if validateSchemaValue(schemas, rawItems, item, depth+1) != nil {
					return contractErr("contract_schema_invalid")
				}
			}
		}
	case string:
		if minimum, ok := schemaInteger(schema["minLength"]); ok && len([]rune(typed)) < minimum {
			return contractErr("contract_schema_invalid")
		}
		if maximum, ok := schemaInteger(schema["maxLength"]); ok && len([]rune(typed)) > maximum {
			return contractErr("contract_schema_invalid")
		}
		if pattern, ok := schema["pattern"].(string); ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil || !compiled.MatchString(typed) {
				return contractErr("contract_schema_invalid")
			}
		}
	default:
		if number, ok := numericValue(value); ok {
			if minimum, exists := numericValue(schema["minimum"]); exists && number < minimum {
				return contractErr("contract_schema_invalid")
			}
			if maximum, exists := numericValue(schema["maximum"]); exists && number > maximum {
				return contractErr("contract_schema_invalid")
			}
		}
	}
	return nil
}

func schemaTypeMatches(rawType any, value any) bool {
	want, ok := rawType.(string)
	if !ok {
		return false
	}
	switch want {
	case "object":
		_, ok = value.(map[string]any)
	case "array":
		_, ok = value.([]any)
	case "string":
		_, ok = value.(string)
	case "boolean":
		_, ok = value.(bool)
	case "null":
		ok = value == nil
	case "number":
		_, ok = numericValue(value)
	case "integer":
		var number float64
		number, ok = numericValue(value)
		ok = ok && math.Trunc(number) == number
	default:
		ok = false
	}
	return ok
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.ParseFloat(string(typed), 64)
		return parsed, err == nil
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func schemaInteger(value any) (int, bool) {
	number, ok := numericValue(value)
	if !ok || math.Trunc(number) != number || number < 0 || number > math.MaxInt32 {
		return 0, false
	}
	return int(number), true
}

func jsonValuesEqual(left, right any) bool {
	leftBytes, leftErr := canonicalizeValueImpl(left)
	rightBytes, rightErr := canonicalizeValueImpl(right)
	return leftErr == nil && rightErr == nil && string(leftBytes) == string(rightBytes)
}
