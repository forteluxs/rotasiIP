package common

import (
	"sync"
	"sync/atomic"
	"time"
)

// ProxyStats holds live runtime metrics shared between the proxy server and dashboard.
type ProxyStats struct {
	mu           sync.RWMutex
	reqCount     int64
	rotateCount  int64
	errCount     int64
	currentProxy string
	startTime    time.Time
}

func NewProxyStats() *ProxyStats { return &ProxyStats{startTime: time.Now()} }

func (s *ProxyStats) IncrReq()    { atomic.AddInt64(&s.reqCount, 1) }
func (s *ProxyStats) IncrRotate() { atomic.AddInt64(&s.rotateCount, 1) }
func (s *ProxyStats) IncrErr()    { atomic.AddInt64(&s.errCount, 1) }

func (s *ProxyStats) SetCurrentProxy(proxy string) {
	s.mu.Lock()
	s.currentProxy = proxy
	s.mu.Unlock()
}

// Get returns a snapshot of all counters plus uptime in seconds.
func (s *ProxyStats) Get() (req, rotates, errs int64, proxy string, uptime float64) {
	req = atomic.LoadInt64(&s.reqCount)
	rotates = atomic.LoadInt64(&s.rotateCount)
	errs = atomic.LoadInt64(&s.errCount)
	s.mu.RLock()
	proxy = s.currentProxy
	s.mu.RUnlock()
	uptime = time.Since(s.startTime).Seconds()
	return
}
