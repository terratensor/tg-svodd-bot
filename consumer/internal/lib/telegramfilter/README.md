# Telegram Mention Filter

Внутренний пакет для фильтрации Telegram-упоминаний в сообщениях перед публикацией в канале.

## Функциональность

Заменяет все символы `@` на `_` в потенциальных Telegram-упоминаниях, чтобы:
- Полностью предотвратить автоматическое создание кликабельных ссылок Telegram
- Сохранить читаемость упоминаний для пользователей
- Обеспечить консистентную обработку всех случаев

## Использование

```go
import "github.com/terratensor/tg-svodd-bot/consumer/internal/lib/telegramfilter"

func main() {
message := "Присоединяйтесь к @our_channel и @test@123"
filtered := telegramfilter.FilterMessage(message)
// Результат: "Присоединяйтесь к _our_channel и _test_123"
}
```

## Правила фильтрации

1. Заменяются ВСЕ символы `@` на `_` в словах, начинающихся с `@`
2. Обрабатываются все случаи, включая:
- Обычные упоминания (`@channel` → `_channel`)
- Составные упоминания (`@test@123` → `_test_123`)
- Специальные случаи (`@_invalid` → `__invalid`)
3. Не затрагиваются:
- Email-адреса (`email@domain.com` остается без изменений)
- Слова, не начинающиеся с `@`

## Примеры обработки

| Вход | Выход |
|--------------------------------|-------------------------------|
| "Привет @channel" | "Привет _channel" |
| "Email: test@example.com" | "Email: test@example.com" |
| "@valid @invalid@name" | "_valid _invalid_name" |
| "@under_score @double__under" | "_under_score _double__under"|
| "@_invalid @test@" | "__invalid _test_" |
| "Сообщение @multi@part@test" | "Сообщение _multi_part_test" |

## Принцип работы

Фильтр использует простое правило: **все @ в словах, начинающихся с @, заменяются на _**.
Это гарантирует:
- Невозможность обхода фильтрации, исключены случаи маскировки реального канала за последовательностью сообщений со знаком `@`: @real_channel@something_wrong
- Простоту и надежность реализации
- Сохранение читаемости сообщений

## Интеграция

```go
func sendToTelegram(text string) {
safeText := telegramfilter.FilterMessage(text)
// Отправляем safeText в Telegram канал
}
```