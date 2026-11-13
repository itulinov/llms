package main

import (
	"sync"
	"time"
)

type attempt struct {
	id uint64
	at time.Time
}
type loginLimiter struct {
	mu     sync.Mutex
	next   uint64
	ips    map[string][]attempt
	logins map[string][]attempt
}

func newLimiter() *loginLimiter {
	return &loginLimiter{ips: map[string][]attempt{}, logins: map[string][]attempt{}}
}
func prune(m map[string][]attempt, now time.Time) {
	for k, v := range m {
		keep := v[:0]
		for _, a := range v {
			if now.Sub(a.at) < 5*time.Minute {
				keep = append(keep, a)
			}
		}
		if len(keep) == 0 {
			delete(m, k)
		} else {
			m[k] = keep
		}
	}
}
func (l *loginLimiter) reserve(ip, login string) (uint64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	prune(l.ips, now)
	prune(l.logins, now)
	if len(l.ips[ip]) >= 30 || len(l.logins[login]) >= 5 {
		return 0, false
	}
	if len(l.ips[ip]) == 0 && len(l.ips) >= 10000 || len(l.logins[login]) == 0 && len(l.logins) >= 10000 {
		return 0, false
	}
	l.next++
	a := attempt{l.next, now}
	l.ips[ip] = append(l.ips[ip], a)
	l.logins[login] = append(l.logins[login], a)
	return a.id, true
}
func (l *loginLimiter) release(login string, id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.logins[login]
	for i, a := range v {
		if a.id == id {
			v = append(v[:i], v[i+1:]...)
			break
		}
	}
	if len(v) == 0 {
		delete(l.logins, login)
	} else {
		l.logins[login] = v
	}
}
