package msgparser

import (
	"testing"
)

func TestTruncateText(t *testing.T) {
	p := &Parser{maxWords: 6, maxChars: 20}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "within max words limit",
			input:    "hello world this is a test",
			expected: "hello world this is a test",
		},
		{
			name:     "within max words limit utf-8",
			input:    "привет мир запускаем тест для строки",
			expected: "привет мир запускаем тест для строки",
		},
		{
			name:     "exceeds max words limit, within max chars limit",
			input:    "hello world this is a test with more words",
			expected: "hello world this is…",
		},
		{
			name:     "exceeds max words limit, within max chars limit utf-8",
			input:    "привет мир запускаем тест для строки, в которой еще больше слов",
			expected: "привет мир…",
		},
		{
			name:     "exceeds both max words and max chars limits",
			input:    "hello world this is a very long test with many words and characters",
			expected: "hello world this is…",
		},
		{
			name:     "empty input text",
			input:    "",
			expected: "",
		},
		{
			name:     "single-word input text",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "input text with multiple spaces between words",
			input:    "hello   world  this  is  a  test",
			expected: "hello   world  this…",
		},
		{
			name:     "input text with multiple spaces between words utf-8",
			input:    "привет   мир  запускаем  тест  для  строки",
			expected: "привет   мир…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := p.truncateText(tt.input)
			if actual != tt.expected {
				t.Errorf("truncateText(%q) = %q, want %q", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestModifyString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "Привет, мир ",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир.",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир,",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир:",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир;",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир…",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир-",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир–",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир—",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир=",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир+",
			expected: "Привет, мир…",
		},
		{
			input:    "Привет, мир",
			expected: "Привет, мир…",
		},
		{
			input:    "Hello, world ",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world.",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world,",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world:",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world;",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world…",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world-",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world–",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world—",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world=",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world+",
			expected: "Hello, world…",
		},
		{
			input:    "Hello, world",
			expected: "Hello, world…",
		},
	}

	for _, test := range tests {
		result := ModifyString(test.input)
		if result != test.expected {
			t.Errorf("ModifyString(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}
