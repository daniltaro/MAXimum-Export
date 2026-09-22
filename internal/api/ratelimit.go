package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter ограничивает число POST-запросов с одного адреса: не больше limit за окно window.
// Так один клиент не может завалить сервер расчётами или вытеснить чужие расчёты из памяти.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*bucket
	sweep  time.Time // когда последний раз чистили старые записи
}

type bucket struct {
	start time.Time // начало текущего окна
	n     int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string]*bucket{}}
}

// Allow — можно ли выполнить запрос с адреса ip в момент now.
func (l *rateLimiter) Allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.sweep) > l.window { // раз в окно удаляем адреса, которые давно не заходили
		for k, b := range l.hits {
			if now.Sub(b.start) > l.window {
				delete(l.hits, k)
			}
		}
		l.sweep = now
	}
	b := l.hits[ip]
	if b == nil || now.Sub(b.start) > l.window {
		l.hits[ip] = &bucket{start: now, n: 1}
		return true
	}
	if b.n >= l.limit {
		return false
	}
	b.n++
	return true
}

// clientIP — адрес клиента. За обратным прокси (TRUST_PROXY=true) берётся первый адрес
// из X-Forwarded-For; без прокси этот заголовок игнорируется — его легко подделать.
func (s *Server) clientIP(r *http.Request) string {
	if s.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
