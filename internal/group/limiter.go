package group

import (
	"sync"
	"time"
)

// attemptLimiter ограничивает число неудачных вводов кода приглашения на пользователя.
// Состояние в памяти процесса (бэкенд один; при перезапуске счётчики сбрасываются).
// Код из 4 цифр иначе подбирается за минуты, поэтому лимит обязателен.
type attemptLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	failures map[string][]time.Time
	now      func() time.Time
}

func newAttemptLimiter(max int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{max: max, window: window, failures: map[string][]time.Time{}, now: time.Now}
}

// prune оставляет только неудачи внутри окна. Вызывать под мьютексом.
func (l *attemptLimiter) prune(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	list := l.failures[key]
	i := 0
	for i < len(list) && !list[i].After(cutoff) {
		i++
	}
	list = list[i:]
	if len(list) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = list
	return list
}

// blocked: true, если пользователь уже исчерпал лимит неудач в текущем окне.
func (l *attemptLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key)) >= l.max
}

func (l *attemptLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	l.failures[key] = append(l.failures[key], l.now())
}
