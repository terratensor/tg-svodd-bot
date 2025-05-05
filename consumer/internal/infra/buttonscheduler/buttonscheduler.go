package buttonscheduler

import (
	"log"
	"math/rand"
	"sync"
	"time"
)

// ButtonScheduler управляет показом кнопки через случайные интервалы
type ButtonScheduler struct {
	counterMutex sync.Mutex // Мьютекс для безопасного доступа к счетчику
	messageCount int        // Счетчик сообщений без кнопки
	nextButtonAt int        // Номер сообщения, на котором будет показана кнопка
	rng          *rand.Rand // Локальный генератор случайных чисел
}

// NewButtonScheduler создает новый ButtonScheduler
func NewButtonScheduler() *ButtonScheduler {
	return &ButtonScheduler{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// resetButtonInterval генерирует случайное число от 3 до 10 и устанавливает nextButtonAt
func (bs *ButtonScheduler) resetButtonInterval() {
	bs.counterMutex.Lock()
	defer bs.counterMutex.Unlock()
	bs.nextButtonAt = bs.rng.Intn(8) + 3 // Случайное число от 3 до 10
	bs.messageCount = 0                  // Сбрасываем счетчик сообщений
	log.Printf("🚩🚩🚩 Следующая кнопка будет показана через %d сообщений", bs.nextButtonAt)
}

// ShouldShowButton проверяет, нужно ли показывать кнопку
func (bs *ButtonScheduler) ShouldShowButton() bool {
	bs.counterMutex.Lock()
	defer bs.counterMutex.Unlock()
	bs.messageCount++
	return bs.messageCount >= bs.nextButtonAt
}

// Reset сбрасывает счетчик и задает новый интервал
func (bs *ButtonScheduler) Reset() {
	bs.resetButtonInterval()
}
