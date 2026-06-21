package codemode

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const unknownType = "unknown"

func schemaToTypeDeclaration(schemaValue any) string {
	if schemaValue == nil {
		return unknownType
	}

	root, ok := normalizeSchemaValue(schemaValue).(map[string]any)
	if !ok {
		return unknownType
	}

	return schemaToTypeExpr(root, root, map[string]bool{}, "")
}

func schemaToTypeExpr(schemaValue any, root map[string]any, seenRefs map[string]bool, indent string) string {
	schemaMap, ok := schemaValue.(map[string]any)
	if !ok || len(schemaMap) == 0 {
		return unknownType
	}

	if ref, _ := schemaMap["$ref"].(string); ref != "" {
		if seenRefs[ref] {
			return unknownType
		}
		resolved, ok := resolveRef(root, ref)
		if !ok {
			return unknownType
		}
		seenRefs[ref] = true
		resolvedType := schemaToTypeExpr(resolved, root, seenRefs, indent)
		delete(seenRefs, ref)
		return resolvedType
	}

	if enumValues, ok := schemaMap["enum"].([]any); ok && len(enumValues) > 0 {
		return joinTypes(enumToTypes(enumValues))
	}

	if constValue, ok := schemaMap["const"]; ok {
		return literalType(constValue)
	}

	if oneOf, ok := schemaList(schemaMap, "oneOf"); ok {
		return maybeNullable(joinTypes(renderSchemaList(oneOf, root, seenRefs, indent)), schemaMap)
	}

	if anyOf, ok := schemaList(schemaMap, "anyOf"); ok {
		return maybeNullable(joinTypes(renderSchemaList(anyOf, root, seenRefs, indent)), schemaMap)
	}

	if allOf, ok := schemaList(schemaMap, "allOf"); ok {
		return maybeNullable(joinIntersections(renderSchemaList(allOf, root, seenRefs, indent)), schemaMap)
	}

	types := normalizeTypes(schemaMap["type"])
	if len(types) == 0 {
		switch {
		case hasProperties(schemaMap):
			types = []string{"object"}
		case schemaMap["items"] != nil:
			types = []string{"array"}
		default:
			return maybeNullable(unknownType, schemaMap)
		}
	}

	results := make([]string, 0, len(types))
	for _, schemaType := range types {
		switch schemaType {
		case "object":
			results = append(results, objectType(schemaMap, root, seenRefs, indent))
		case "array":
			results = append(results, arrayType(schemaMap, root, seenRefs, indent))
		case "string":
			results = append(results, "string")
		case "integer", "number":
			results = append(results, "number")
		case "boolean":
			results = append(results, "boolean")
		case "null":
			results = append(results, "null")
		default:
			results = append(results, unknownType)
		}
	}

	return maybeNullable(joinTypes(results), schemaMap)
}

func objectType(schemaMap map[string]any, root map[string]any, seenRefs map[string]bool, indent string) string {
	properties, _ := schemaMap["properties"].(map[string]any)
	required := requiredSet(schemaMap["required"])
	additionalProperties, hasAdditionalProperties := schemaMap["additionalProperties"]

	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	if len(keys) == 0 {
		switch additional := additionalProperties.(type) {
		case bool:
			if additional {
				return "Record<string, unknown>"
			}
			return "Record<string, never>"
		case nil:
			return "Record<string, unknown>"
		default:
			valueType := schemaToTypeExpr(additional, root, seenRefs, indent+"  ")
			return fmt.Sprintf("Record<string, %s>", valueType)
		}
	}

	nextIndent := indent + "  "
	lines := make([]string, 0, len(keys)+2)
	lines = append(lines, "{")
	for _, key := range keys {
		valueType := schemaToPropertyTypeExpr(properties[key], root, seenRefs, nextIndent, required[key])
		if needsParensForArrayUnion(valueType) {
			valueType = "(" + valueType + ")"
		}
		optional := "?"
		if required[key] {
			optional = ""
		}
		lines = append(lines, fmt.Sprintf("%s%s%s: %s;", nextIndent, propertyKey(key), optional, valueType))
	}
	lines = append(lines, indent+"}")

	base := strings.Join(lines, "\n")
	if !hasAdditionalProperties {
		return base
	}

	switch additional := additionalProperties.(type) {
	case bool:
		if additional {
			return base + " & Record<string, unknown>"
		}
		return base
	case nil:
		return base
	default:
		valueType := schemaToTypeExpr(additional, root, seenRefs, indent+"  ")
		return base + fmt.Sprintf(" & Record<string, %s>", valueType)
	}
}

func arrayType(schemaMap map[string]any, root map[string]any, seenRefs map[string]bool, indent string) string {
	if tupleItems, ok := schemaMap["items"].([]any); ok {
		parts := renderSchemaList(tupleItems, root, seenRefs, indent)
		return "[" + strings.Join(parts, ", ") + "]"
	}

	itemType := unknownType
	if itemSchema, ok := schemaMap["items"]; ok {
		itemType = schemaToTypeExpr(itemSchema, root, seenRefs, indent)
	}
	if needsParensForArrayUnion(itemType) {
		return "(" + itemType + ")[]"
	}
	return itemType + "[]"
}

func schemaToPropertyTypeExpr(schemaValue any, root map[string]any, seenRefs map[string]bool, indent string, required bool) string {
	if !required {
		return schemaToTypeExpr(schemaValue, root, seenRefs, indent)
	}

	schemaMap, ok := schemaValue.(map[string]any)
	if !ok {
		return schemaToTypeExpr(schemaValue, root, seenRefs, indent)
	}

	adjusted, ok := requiredArraySchemaWithoutNull(schemaMap)
	if !ok {
		return schemaToTypeExpr(schemaValue, root, seenRefs, indent)
	}

	return schemaToTypeExpr(adjusted, root, seenRefs, indent)
}

func requiredArraySchemaWithoutNull(schemaMap map[string]any) (map[string]any, bool) {
	types := normalizeTypes(schemaMap["type"])
	if len(types) < 2 || !slices.Contains(types, "array") || !slices.Contains(types, "null") {
		return nil, false
	}

	trimmed := make([]string, 0, len(types)-1)
	for _, schemaType := range types {
		if schemaType != "null" {
			trimmed = append(trimmed, schemaType)
		}
	}
	if len(trimmed) == len(types) || len(trimmed) == 0 {
		return nil, false
	}

	adjusted := make(map[string]any, len(schemaMap))
	maps.Copy(adjusted, schemaMap)
	if len(trimmed) == 1 {
		adjusted["type"] = trimmed[0]
	} else {
		union := make([]any, 0, len(trimmed))
		for _, schemaType := range trimmed {
			union = append(union, schemaType)
		}
		adjusted["type"] = union
	}

	return adjusted, true
}

func renderSchemaList(items []any, root map[string]any, seenRefs map[string]bool, indent string) []string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, schemaToTypeExpr(item, root, seenRefs, indent))
	}
	return parts
}

func schemaList(schemaMap map[string]any, key string) ([]any, bool) {
	items, ok := schemaMap[key].([]any)
	return items, ok && len(items) > 0
}

func resolveRef(root map[string]any, ref string) (map[string]any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}

	current := any(root)
	for segment := range strings.SplitSeq(strings.TrimPrefix(ref, "#/"), "/") {
		segment = strings.ReplaceAll(segment, "~1", "/")
		segment = strings.ReplaceAll(segment, "~0", "~")

		switch node := current.(type) {
		case map[string]any:
			current = node[segment]
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}

	resolved, ok := current.(map[string]any)
	return resolved, ok
}

func normalizeTypes(raw any) []string {
	switch value := raw.(type) {
	case string:
		return []string{value}
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

func requiredSet(raw any) map[string]bool {
	set := make(map[string]bool)
	values, ok := raw.([]any)
	if !ok {
		return set
	}
	for _, value := range values {
		text, ok := value.(string)
		if ok {
			set[text] = true
		}
	}
	return set
}

func hasProperties(schemaMap map[string]any) bool {
	properties, ok := schemaMap["properties"].(map[string]any)
	return ok && len(properties) > 0
}

func propertyKey(name string) string {
	if isTSIdentifier(name) && !tsKeywords[name] {
		return name
	}
	encoded, _ := json.Marshal(name)
	return string(encoded)
}

func isTSIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if index == 0 {
			if !unicode.IsLetter(char) && char != '_' && char != '$' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_' && char != '$' {
			return false
		}
	}
	return true
}

func enumToTypes(values []any) []string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, literalType(value))
	}
	return parts
}

func literalType(value any) string {
	switch typed := value.(type) {
	case string:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		return unknownType
	}
}

func joinTypes(types []string) string {
	return joinUniqueTypes(types, " | ")
}

func joinIntersections(types []string) string {
	return joinUniqueTypes(types, " & ")
}

func joinUniqueTypes(types []string, separator string) string {
	seen := make(map[string]bool)
	unique := make([]string, 0, len(types))
	for _, value := range types {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	if len(unique) == 0 {
		return unknownType
	}
	if len(unique) == 1 {
		return unique[0]
	}
	return strings.Join(unique, separator)
}

func maybeNullable(rendered string, schemaMap map[string]any) string {
	if nullable, _ := schemaMap["nullable"].(bool); nullable {
		return joinTypes([]string{rendered, "null"})
	}
	return rendered
}

func needsParensForArrayUnion(value string) bool {
	return strings.Contains(value, " | ") || strings.Contains(value, " & ")
}
