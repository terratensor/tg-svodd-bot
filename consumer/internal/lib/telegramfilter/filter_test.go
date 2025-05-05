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
		// Новые тесты для ссылок
		{
			name:     "t.me link",
			input:    "Ссылка t.me/MariaVladimirovnaZakharova/10046",
			expected: "Ссылка _t.me/MariaVladimirovnaZakharova/10046",
		},
		{
			name:     "telegram.me link",
			input:    "Ссылка telegram.me/MariaVladimirovnaZakharova",
			expected: "Ссылка _telegram.me/MariaVladimirovnaZakharova",
		},
		{
			name:     "full text",
			input:    "в телеге Марии Владимировны, в сети попадается только в переводе. \nt.me/MariaVladimirovnaZakharova/10046        ",
			expected: "в телеге Марии Владимировны, в сети попадается только в переводе. \n_t.me/MariaVladimirovnaZakharova/10046        ",
		},
		{
			name:     "Multiple links",
			input:    "t.me/link1 и telegram.me/link2",
			expected: "_t.me/link1 и _telegram.me/link2",
		},
		{
			name:     "Excluded domain svoddru",
			input:    "Ссылка t.me/svoddru/123",
			expected: "Ссылка t.me/svoddru/123",
		},
		{
			name:     "Link with punctuation",
			input:    "Ссылка: t.me/test, проверка",
			expected: "Ссылка: _t.me/test, проверка",
		},
		{
			name:     "Mixed content",
			input:    "@mention и t.me/link плюс email@domain.com",
			expected: "_mention и _t.me/link плюс email@domain.com",
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
