package linkprocessor

import "testing"

func TestTgLinkClipper(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Telegram link without exception",
			input:    "https://t.me/somechannel",
			expected: "_t.me/somechannel",
		},
		{
			name:     "Telegram link with exception",
			input:    "https://t.me/svoddru",
			expected: "https://t.me/svoddru",
		},
		{
			name:     "Yandex Dzen link (HTTPS)",
			input:    "https://dzen.ru/somepage",
			expected: "_dzen.ru/somepage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TgLinkClipper(tt.input)
			if result != tt.expected {
				t.Errorf("TgLinkClipper(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}
