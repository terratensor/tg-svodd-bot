# Telegram Message Filter

**Пакет для фильтрации Telegram-упоминаний и ссылок в сообщениях**

## Назначение

Пакет обеспечивает безопасную публикацию сообщений в Telegram-каналах, предотвращая нежелательные кликабельные ссылки.

## Правила фильтрации

1. Заменяются ВСЕ символы `@` на `_` в словах, начинающихся с `@`
2. Обрабатываются все случаи, включая:
- Обычные упоминания (`@channel` → `_channel`)
- Составные упоминания (`@test@123` → `_test_123`)
- Специальные случаи (`@_invalid` → `__invalid`)
3. Добавляется `_` перед Telegram-ссылками:
- `t.me/channel` → `_t.me/channel`
- `telegram.me/chat` → `_telegram.me/chat`
4. Исключения:
- Упоминание `@svoddru` остается без изменений
- Ссылки на `t.me/svoddru` и `telegram.me/svoddru` не изменяются
5. Не затрагиваются:
- Email-адреса (`email@domain.com` остается без изменений)
- Обычные URL с http/https
- Слова, не начинающиеся с `@`

## Примеры обработки

| Входное сообщение | Результат фильтрации |
|-------------------|----------------------|
| `@channel @test` | `_channel _test` |
| `@test@channel` | `_test_channel` |
| `@_invalid` | `__invalid` |
| `t.me/private` | `_t.me/private` |
| `telegram.me/chat` | `_telegram.me/chat` |
| `@svoddru официальный` | `@svoddru официальный` |
| `t.me/svoddru новости` | `t.me/svoddru новости` |
| `email@domain.com` | `email@domain.com` |
| `https://site.com` | `https://site.com` |
| `@multi@part@test` | `_multi_part_test` |
| `Сообщение с @test@123 и t.me/link` | `Сообщение с _test_123 и _t.me/link` |
| `@_test @invalid_ @double__under` | `__test _invalid_ _double__under` |

## Принцип работы

Фильтр использует строгие правила:
1. **Для упоминаний**:
- Все `@` заменяются на `_` в словах, начинающихся с `@`
- Исключение только для `@svoddru`
- Гарантирует невозможность обхода фильтрации через конструкции типа `@real_channel@something`

2. **Для ссылок**:
- Добавляется `_` перед `t.me/...` и `telegram.me/...`
- Исключение для ссылок на `svoddru`

3. **Многоэтапная обработка**:
```go
text = filterMentions(text) // Сначала упоминания
text = filterLinks(text) // Затем ссылки
```

4. **Гарантии**:
- Полное предотвращение кликабельных упоминаний и ссылок
- Сохранение читаемости сообщений
- Невозможность обхода фильтрации
- Минимальное воздействие на исходное форматирование

## Интеграция

```go
import "github.com/terratensor/tg-svodd-bot/consumer/internal/lib/telegramfilter"

func prepareMessage(text string) string {
return telegramfilter.FilterMessage(text)
}

// Пример использования
func sendToChannel(text string) {
safeText := telegramfilter.FilterMessage(text)
// Отправка safeText в Telegram канал
// ...
}
```

## Дополнительные примеры из тестов

```go
// Тест для составных упоминаний
input: "Сообщение с @multi@part@mention"
expected: "Сообщение с _multi_part_mention"

// Тест для email-адресов
input: "Мой email: email@example.com"
expected: "Мой email: email@example.com"

// Тест для сложного случая
input: "@test @abc@def @xyz@123@test Проверка email@domain.com"
expected: "_test _abc_def _xyz_123_test Проверка email@domain.com"

// Тест для ссылок с исключениями
input: "Официальный @svoddru и t.me/svoddru/123"
expected: "Официальный @svoddru и t.me/svoddru/123"
```