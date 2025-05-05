package telegramfilter

import (
	"testing"
)

func TestFilterMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No mentions",
			input:    "Это обычное сообщение без упоминаний",
			expected: "Это обычное сообщение без упоминаний",
		},
		{
			name:     "Simple mention",
			input:    "Проверка @testchannel упоминания",
			expected: "Проверка _testchannel упоминания",
		},
		{
			name:     "Multiple mentions",
			input:    "@channel1 и @channel2 должны быть заменены",
			expected: "_channel1 и _channel2 должны быть заменены",
		},
		{
			name:     "Mention with punctuation",
			input:    "Проверка @channel, с запятой и @channel2.",
			expected: "Проверка _channel, с запятой и _channel2.",
		},
		{
			name:     "Invalid mentions",
			input:    "@short @invalid@name @_invalid @invalid_ @double__underscore",
			expected: "_short _invalid_name __invalid _invalid_ _double__underscore",
		},
		{
			name:     "Multiple @ in one word",
			input:    "Сообщение с @multi@part@mention",
			expected: "Сообщение с _multi_part_mention",
		},
		{
			name:     "Email should not be filtered",
			input:    "Мой email: email@example.com",
			expected: "Мой email: email@example.com",
		},
		{
			name:     "Complex case",
			input:    "@test @abc@def @xyz@123@test Проверка email@domain.com",
			expected: "_test _abc_def _xyz_123_test Проверка email@domain.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterMessage(tt.input)
			if result != tt.expected {
				t.Errorf("FilterMessage() = %v, want %v", result, tt.expected)
			}
		})
	}
}
