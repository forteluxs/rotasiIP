package dashboard

import "sync"

// Broadcaster fans out string messages to all SSE subscribers.
type Broadcaster struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

// LogBroadcaster is the package-level instance used by the proxy server.
var LogBroadcaster = &Broadcaster{clients: make(map[chan string]struct{})}

func (b *Broadcaster) Subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	close(ch)
	b.mu.Unlock()
}

func (b *Broadcaster) Publish(msg string) {
	b.mu.Lock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
		}
	}
	b.mu.Unlock()
}
