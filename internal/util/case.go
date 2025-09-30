package util

import (
	"regexp"
	"strings"
)

var matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
var matchAllCap = regexp.MustCompile("([a-z0-9])([A-Z])")

// CamelCaseToUpperSnakeCase converts a camelCase string to UPPER_SNAKE_CASE.
// For example, "myAPIClient" becomes "MY_API_CLIENT".
func CamelCaseToUpperSnakeCase(s string) string {
	snake := matchFirstCap.ReplaceAllString(s, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToUpper(snake)
}

// KebabCaseToUpperSnakeCase converts a kebab-case string to UPPER_SNAKE_CASE.
// For example, "my-kebab-variable" becomes "MY_KEBAB_VARIABLE".
func KebabCaseToUpperSnakeCase(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
}
