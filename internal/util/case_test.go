package util

import "testing"

func TestToUpperSnakeCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple camel case",
			input:    "myAwesomeVariable",
			expected: "MY_AWESOME_VARIABLE",
		},
		{
			name:     "leading acronym",
			input:    "HTTPClient",
			expected: "HTTP_CLIENT",
		},
		{
			name:     "embedded acronym",
			input:    "myHTTPClient",
			expected: "MY_HTTP_CLIENT",
		},
		{
			name:     "number in name",
			input:    "version2API",
			expected: "VERSION2_API",
		},
		{
			name:     "single word",
			input:    "variable",
			expected: "VARIABLE",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "already snake case",
			input:    "already_snake",
			expected: "ALREADY_SNAKE",
		},
		{
			name:     "mixed case with acronym",
			input:    "aJSONParser",
			expected: "A_JSON_PARSER",
		},
		{
			name:     "mixed snake and camel case",
			input:    "someValue_and_anotherValue",
			expected: "SOME_VALUE_AND_ANOTHER_VALUE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CamelCaseToUpperSnakeCase(tt.input); got != tt.expected {
				t.Errorf("CamelCaseToUpperSnakeCase() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestKebabToUpperSnakeCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple kebab case",
			input:    "my-awesome-variable",
			expected: "MY_AWESOME_VARIABLE",
		},
		{
			name:     "single word",
			input:    "variable",
			expected: "VARIABLE",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "already contains underscores",
			input:    "already_snake-case",
			expected: "ALREADY_SNAKE_CASE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KebabCaseToUpperSnakeCase(tt.input); got != tt.expected {
				t.Errorf("KebabCaseToUpperSnakeCase() = %v, want %v", got, tt.expected)
			}
		})
	}
}
