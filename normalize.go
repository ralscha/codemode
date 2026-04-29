package codemode

import (
	"strings"
	"unicode"
)

func normalizeCode(code string) string {
	source := stripCodeFences(strings.TrimSpace(code))
	if strings.TrimSpace(source) == "" {
		return ""
	}

	if inner, ok := strings.CutPrefix(source, "export default "); ok {
		return normalizeCode(inner)
	}
	if strings.HasPrefix(source, "(") && strings.HasSuffix(source, ")") && matchingOuterParens(source) {
		return normalizeCode(source[1 : len(source)-1])
	}

	if body, ok := unwrapArrowFunction(source); ok {
		return body
	}

	return insertReturnForFinalExpression(source)
}

func stripCodeFences(source string) string {
	if !strings.HasPrefix(source, "```") {
		return source
	}

	_, after, ok := strings.Cut(source, "\n")
	if !ok {
		return source
	}

	body := after
	body = strings.TrimSpace(body)
	if !strings.HasSuffix(body, "```") {
		return source
	}

	return strings.TrimSpace(strings.TrimSuffix(body, "```"))
}

func unwrapArrowFunction(source string) (string, bool) {
	arrow := findTopLevelArrow(source)
	if arrow < 0 {
		return "", false
	}

	head := strings.TrimSpace(source[:arrow])
	if strings.HasPrefix(head, "async ") {
		head = strings.TrimSpace(strings.TrimPrefix(head, "async"))
	}
	if !isArrowParameters(head) {
		return "", false
	}

	body := strings.TrimSpace(source[arrow+2:])
	if strings.HasPrefix(body, "{") && strings.HasSuffix(body, "}") && matchingOuterBraces(body) {
		return strings.TrimSpace(body[1 : len(body)-1]), true
	}

	return "return (" + strings.TrimSuffix(body, ";") + ");", true
}

func findTopLevelArrow(source string) int {
	scanner := codeScanner{}
	for index := 0; index < len(source)-1; index++ {
		scanner.scan(source, index)
		if scanner.inCode() && scanner.depth == 0 && source[index:index+2] == "=>" {
			return index
		}
	}
	return -1
}

func isArrowParameters(source string) bool {
	if source == "" {
		return false
	}
	if isIdentifier(source) {
		return true
	}
	if strings.HasPrefix(source, "(") && strings.HasSuffix(source, ")") {
		return matchingOuterParens(source)
	}
	return false
}

func insertReturnForFinalExpression(source string) string {
	trimmed := strings.TrimSpace(source)
	withoutTrailingSemicolon := strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	start := lastTopLevelStatementStart(withoutTrailingSemicolon)
	if start < 0 || start >= len(withoutTrailingSemicolon) {
		return source
	}

	before := withoutTrailingSemicolon[:start]
	last := strings.TrimSpace(withoutTrailingSemicolon[start:])
	if last == "" || startsWithStatementKeyword(last) {
		return source
	}

	return before + "return (" + last + ");"
}

func lastTopLevelStatementStart(source string) int {
	scanner := codeScanner{}
	start := 0
	for index := 0; index < len(source); index++ {
		scanner.scan(source, index)
		if scanner.inCode() && scanner.depth == 0 && source[index] == ';' {
			start = index + 1
		}
	}
	return start
}

func startsWithStatementKeyword(source string) bool {
	keyword := firstWord(source)
	switch keyword {
	case "break", "class", "const", "continue", "debugger", "do", "export", "for", "function", "if", "import", "let", "return", "switch", "throw", "try", "var", "while", "with":
		return true
	default:
		return false
	}
}

func firstWord(source string) string {
	for index, r := range source {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
			return source[:index]
		}
	}
	return source
}

func isIdentifier(source string) bool {
	for index, r := range source {
		if index == 0 {
			if !unicode.IsLetter(r) && r != '_' && r != '$' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
			return false
		}
	}
	return true
}

func matchingOuterBraces(source string) bool {
	return matchingOuterDelimiters(source, '{', '}')
}

func matchingOuterParens(source string) bool {
	return matchingOuterDelimiters(source, '(', ')')
}

func matchingOuterDelimiters(source string, open byte, close byte) bool {
	depth := 0
	scanner := codeScanner{}
	for index := 0; index < len(source); index++ {
		scanner.scan(source, index)
		if !scanner.inCode() {
			continue
		}
		if source[index] == open {
			depth++
		}
		if source[index] == close {
			depth--
			if depth == 0 && index != len(source)-1 {
				return false
			}
		}
	}
	return depth == 0
}

type codeScanner struct {
	depth        int
	quote        byte
	lineComment  bool
	blockComment bool
	escaped      bool
}

func (scanner *codeScanner) inCode() bool {
	return scanner.quote == 0 && !scanner.lineComment && !scanner.blockComment
}

func (scanner *codeScanner) scan(source string, index int) {
	current := source[index]
	next := byte(0)
	if index+1 < len(source) {
		next = source[index+1]
	}

	if scanner.lineComment {
		if current == '\n' || current == '\r' {
			scanner.lineComment = false
		}
		return
	}
	if scanner.blockComment {
		if current == '*' && next == '/' {
			scanner.blockComment = false
		}
		return
	}
	if scanner.quote != 0 {
		if scanner.escaped {
			scanner.escaped = false
			return
		}
		if current == '\\' {
			scanner.escaped = true
			return
		}
		if current == scanner.quote {
			scanner.quote = 0
		}
		return
	}

	if current == '/' && next == '/' {
		scanner.lineComment = true
		return
	}
	if current == '/' && next == '*' {
		scanner.blockComment = true
		return
	}
	if current == '\'' || current == '"' || current == '`' {
		scanner.quote = current
		return
	}

	switch current {
	case '(', '[', '{':
		scanner.depth++
	case ')', ']', '}':
		if scanner.depth > 0 {
			scanner.depth--
		}
	}
}
