package codemode

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolIndex struct {
	documents      []toolSearchDocument
	documentCount  float64
	averageLength  float64
	documentCounts map[string]int
}

type ToolSearchResult struct {
	Definition ToolDefinition
	Score      float64
	Matches    []string
}

type ToolSearchOption func(*toolSearchOptions)

type toolSearchOptions struct {
	limit    int
	minScore float64
}

func WithToolSearchLimit(limit int) ToolSearchOption {
	return func(opts *toolSearchOptions) {
		if limit > 0 {
			opts.limit = limit
		}
	}
}

func WithToolSearchMinScore(score float64) ToolSearchOption {
	return func(opts *toolSearchOptions) {
		if score > 0 {
			opts.minScore = score
		}
	}
}

func NewToolIndex[T ToolIndexDefinition](definitions []T) (*ToolIndex, error) {
	documents := make([]toolSearchDocument, 0, len(definitions))
	documentCounts := make(map[string]int)
	totalLength := 0

	for index, item := range definitions {
		definition, err := toolDefinitionFromSupported(item)
		if err != nil {
			return nil, fmt.Errorf("convert tool at index %d: %w", index, err)
		}

		if strings.TrimSpace(definition.Name) == "" {
			return nil, fmt.Errorf("tool at index %d has an empty name", index)
		}

		document := newToolSearchDocument(definition)
		documents = append(documents, document)
		totalLength += document.length

		for token := range document.termFrequency {
			documentCounts[token]++
		}
	}

	averageLength := 0.0
	if len(documents) > 0 {
		averageLength = float64(totalLength) / float64(len(documents))
	}

	return &ToolIndex{
		documents:      documents,
		documentCount:  float64(len(documents)),
		averageLength:  averageLength,
		documentCounts: documentCounts,
	}, nil
}

func toolDefinitionFromSupported[T ToolIndexDefinition](item T) (ToolDefinition, error) {
	switch value := any(item).(type) {
	case ToolDefinition:
		return value, nil
	case mcp.Tool:
		return definitionFromMCPTool(value)
	default:
		return ToolDefinition{}, fmt.Errorf("unsupported tool definition type %T", item)
	}
}

func (index *ToolIndex) Query(query string, opts ...ToolSearchOption) []ToolSearchResult {
	if index == nil {
		return nil
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	options := resolveToolSearchOptions(opts)
	queryTokens := tokenizeToolSearchText(query)
	if len(queryTokens) == 0 {
		return nil
	}

	results := make([]ToolSearchResult, 0, len(index.documents))
	for _, document := range index.documents {
		score := index.bm25Score(document, queryTokens)
		score += nameMatchBoost(document, query, queryTokens)
		if score < options.minScore {
			continue
		}

		results = append(results, ToolSearchResult{
			Definition: document.definition,
			Score:      score,
			Matches:    document.matches(queryTokens, 5),
		})
	}

	sortToolSearchResults(results)
	return trimToolSearchResults(results, options.limit)
}

func (index *ToolIndex) QueryRegex(pattern string, opts ...ToolSearchOption) ([]ToolSearchResult, error) {
	if index == nil {
		return nil, nil
	}

	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, nil
	}

	expression, err := regexp.Compile("(?i:" + pattern + ")")
	if err != nil {
		return nil, err
	}

	options := resolveToolSearchOptions(opts)
	results := make([]ToolSearchResult, 0, len(index.documents))
	for _, document := range index.documents {
		matches := document.regexMatches(expression, 5)
		if len(matches) == 0 {
			continue
		}

		results = append(results, ToolSearchResult{
			Definition: document.definition,
			Score:      float64(len(matches)),
			Matches:    matches,
		})
	}

	sortToolSearchResults(results)
	return trimToolSearchResults(results, options.limit), nil
}

func (index *ToolIndex) bm25Score(document toolSearchDocument, queryTokens []string) float64 {
	if index.documentCount == 0 || index.averageLength == 0 || document.length == 0 {
		return 0
	}

	const k1 = 1.4
	const b = 0.75
	score := 0.0
	seen := make(map[string]bool)
	for _, token := range queryTokens {
		if seen[token] {
			continue
		}
		seen[token] = true

		frequency := document.termFrequency[token]
		if frequency == 0 {
			continue
		}

		documentFrequency := float64(index.documentCounts[token])
		idf := math.Log(1 + (index.documentCount-documentFrequency+0.5)/(documentFrequency+0.5))
		denominator := frequency + k1*(1-b+b*(float64(document.length)/index.averageLength))
		score += idf * (frequency * (k1 + 1) / denominator)
	}
	return score
}

type toolSearchDocument struct {
	definition    ToolDefinition
	normalized    []string
	termFrequency map[string]float64
	length        int
	nameValues    []string
}

func newToolSearchDocument(definition ToolDefinition) toolSearchDocument {
	normalized := make([]string, 0, 16)
	termFrequency := make(map[string]float64)
	length := 0

	addField := func(value string, weight float64) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}

		normalized = append(normalized, strings.ToLower(value))
		for _, token := range tokenizeToolSearchText(value) {
			termFrequency[token] += weight
			length++
		}
	}

	nameValues := []string{definition.Name, sanitizeIdentifier(definition.Name)}
	addField(definition.Name, 5)
	addField(sanitizeIdentifier(definition.Name), 4)
	addField(definition.Description, 3)

	for _, field := range schemaSearchFields(definition.InputSchema) {
		addField(field.value, field.weight)
	}
	for _, field := range schemaSearchFields(definition.OutputSchema) {
		addField(field.value, field.weight)
	}

	return toolSearchDocument{
		definition:    definition,
		normalized:    normalized,
		termFrequency: termFrequency,
		length:        length,
		nameValues:    nameValues,
	}
}

func (document toolSearchDocument) matches(queryTokens []string, limit int) []string {
	matches := make([]string, 0, limit)
	seen := make(map[string]bool)
	for _, value := range document.normalized {
		for _, token := range queryTokens {
			if !strings.Contains(value, token) {
				continue
			}
			if seen[value] {
				continue
			}
			seen[value] = true
			matches = append(matches, value)
			if len(matches) >= limit {
				return matches
			}
		}
	}
	return matches
}

func (document toolSearchDocument) regexMatches(expression *regexp.Regexp, limit int) []string {
	matches := make([]string, 0, limit)
	seen := make(map[string]bool)
	for _, value := range document.normalized {
		if !expression.MatchString(value) || seen[value] {
			continue
		}
		seen[value] = true
		matches = append(matches, value)
		if len(matches) >= limit {
			return matches
		}
	}
	return matches
}

type schemaSearchField struct {
	value  string
	weight float64
}

func schemaSearchFields(schemaValue any) []schemaSearchField {
	normalized := normalizeSchemaValue(schemaValue)
	if normalized == nil {
		return nil
	}

	fields := make([]schemaSearchField, 0, 16)
	collectSchemaSearchFields(normalized, &fields)
	if encoded, err := json.Marshal(normalized); err == nil {
		fields = append(fields, schemaSearchField{value: string(encoded), weight: 0.25})
	}
	return fields
}

func collectSchemaSearchFields(value any, fields *[]schemaSearchField) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		slices.Sort(keys)

		for _, key := range keys {
			child := typed[key]
			switch key {
			case "properties":
				collectSchemaProperties(child, fields)
			case "description", "title":
				if text, ok := child.(string); ok {
					*fields = append(*fields, schemaSearchField{value: text, weight: 2})
				}
			case "type", "format":
				if text, ok := child.(string); ok {
					*fields = append(*fields, schemaSearchField{value: text, weight: 0.75})
				}
			case "enum":
				collectSchemaEnumValues(child, fields)
			}
			collectSchemaSearchFields(child, fields)
		}
	case []any:
		for _, item := range typed {
			collectSchemaSearchFields(item, fields)
		}
	}
}

func collectSchemaProperties(value any, fields *[]schemaSearchField) {
	properties, ok := value.(map[string]any)
	if !ok {
		return
	}

	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		*fields = append(*fields, schemaSearchField{value: key, weight: 2.5})
	}
}

func collectSchemaEnumValues(value any, fields *[]schemaSearchField) {
	values, ok := value.([]any)
	if !ok {
		return
	}

	for _, item := range values {
		switch typed := item.(type) {
		case string:
			*fields = append(*fields, schemaSearchField{value: typed, weight: 1.5})
		case float64, bool:
			*fields = append(*fields, schemaSearchField{value: fmt.Sprint(typed), weight: 0.75})
		}
	}
}

func nameMatchBoost(document toolSearchDocument, query string, queryTokens []string) float64 {
	queryName := normalizeToolSearchName(query)
	if queryName == "" {
		return 0
	}

	best := 0.0
	for _, name := range document.nameValues {
		normalizedName := normalizeToolSearchName(name)
		if normalizedName == "" {
			continue
		}

		score := 0.0
		switch {
		case normalizedName == queryName:
			score += 25
		case strings.Contains(normalizedName, queryName):
			score += 10
		}

		nameTokens := tokenizeToolSearchText(name)
		if containsAllTokens(nameTokens, queryTokens) {
			score += 6
		}

		distance := levenshteinDistance(normalizedName, queryName)
		limit := max(len([]rune(normalizedName)), len([]rune(queryName)))
		if limit > 0 {
			similarity := 1 - float64(distance)/float64(limit)
			if similarity >= 0.72 {
				score += similarity * 5
			}
		}

		best = max(best, score)
	}
	return best
}

func tokenizeToolSearchText(value string) []string {
	value = splitCamelCase(value)
	parts := strings.FieldsFunc(strings.ToLower(value), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsDigit(char)
	})

	tokens := make([]string, 0, len(parts))
	seen := make(map[string]bool)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		tokens = append(tokens, part)
	}
	return tokens
}

func splitCamelCase(value string) string {
	var builder strings.Builder
	var previous rune
	for index, char := range value {
		if index > 0 && unicode.IsLower(previous) && unicode.IsUpper(char) {
			builder.WriteByte(' ')
		}
		builder.WriteRune(char)
		previous = char
	}
	return builder.String()
}

func normalizeToolSearchName(value string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(value) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func containsAllTokens(values []string, queryTokens []string) bool {
	if len(values) == 0 || len(queryTokens) == 0 {
		return false
	}

	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	for _, token := range queryTokens {
		if !set[token] {
			return false
		}
	}
	return true
}

func resolveToolSearchOptions(opts []ToolSearchOption) toolSearchOptions {
	options := toolSearchOptions{limit: 10}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	return options
}

func sortToolSearchResults(results []ToolSearchResult) {
	slices.SortFunc(results, func(left, right ToolSearchResult) int {
		if left.Score > right.Score {
			return -1
		}
		if left.Score < right.Score {
			return 1
		}
		return strings.Compare(left.Definition.Name, right.Definition.Name)
	})
}

func trimToolSearchResults(results []ToolSearchResult, limit int) []ToolSearchResult {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

func levenshteinDistance(left string, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if len(leftRunes) == 0 {
		return len(rightRunes)
	}
	if len(rightRunes) == 0 {
		return len(leftRunes)
	}

	previous := make([]int, len(rightRunes)+1)
	current := make([]int, len(rightRunes)+1)
	for index := range previous {
		previous[index] = index
	}

	for leftIndex, leftRune := range leftRunes {
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range rightRunes {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[rightIndex+1] = min(
				current[rightIndex]+1,
				previous[rightIndex+1]+1,
				previous[rightIndex]+cost,
			)
		}
		previous, current = current, previous
	}
	return previous[len(rightRunes)]
}
