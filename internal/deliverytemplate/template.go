// Package deliverytemplate parses and renders reusable delivery messages.
package deliverytemplate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var cardVariablePattern = regexp.MustCompile(`\{\{delivery\.cards\.([A-Za-z0-9_-]+)\}\}`)
var anyDoubleBracePattern = regexp.MustCompile(`\{\{|\}\}`)

// Parsed contains a validated message list and keys in first-use order.
type Parsed struct {
	Messages []string
	Keys     []string
}

// Parse validates messages and extracts delivery card keys. Keys are opaque and
// case-sensitive; their names have no built-in business meaning.
func Parse(messages []string) (Parsed, error) {
	if len(messages) == 0 {
		return Parsed{}, fmt.Errorf("发货模板至少需要一条消息")
	}
	out := Parsed{Messages: make([]string, len(messages))}
	seen := map[string]bool{}
	for i, message := range messages {
		if strings.TrimSpace(message) == "" {
			return Parsed{}, fmt.Errorf("发货模板第 %d 条消息不能为空", i+1)
		}
		out.Messages[i] = message
		for _, match := range cardVariablePattern.FindAllStringSubmatch(message, -1) {
			key := match[1]
			if !seen[key] {
				seen[key] = true
				out.Keys = append(out.Keys, key)
			}
		}
		for _, match := range anyDoubleBracePattern.FindAllStringIndex(message, -1) {
			start := match[0]
			if start+2 <= len(message) && message[start:start+2] == "{{" {
				end := strings.Index(message[start+2:], "}}")
				if end < 0 {
					return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含非法变量", i+1)
				}
				token := message[start : start+2+end+2]
				if !cardVariablePattern.MatchString(token) {
					return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含非法变量", i+1)
				}
			}
		}
	}
	return out, nil
}

// ReplaceCards replaces only the delivery card variables. Values are supplied
// by key and are deliberately not recursively rendered.
func ReplaceCards(message string, values map[string]string) string {
	return cardVariablePattern.ReplaceAllStringFunc(message, func(token string) string {
		match := cardVariablePattern.FindStringSubmatch(token)
		if value, ok := values[match[1]]; ok {
			return value
		}
		return token
	})
}

// SortedKeys is useful when validating or processing binding maps.
func SortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
