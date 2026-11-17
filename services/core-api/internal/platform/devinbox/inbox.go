// Package devinbox provides a process-local, expiring OTP inbox for local tests.
package devinbox

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

type Message struct {
	ChallengeID string    `json:"challenge_id"`
	Purpose     string    `json:"purpose"`
	Channel     string    `json:"channel"`
	Destination string    `json:"destination"`
	Code        string    `json:"code"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Logs must never acquire the inbox's sensitive contents, even accidentally.
func (Message) LogValue() slog.Value { return slog.StringValue("[redacted auth message]") }
func (Message) String() string       { return "[redacted auth message]" }

type Inbox struct {
	mu       sync.Mutex
	messages []Message
	capacity int
	ttl      time.Duration
	stop     chan struct{}
	done     chan struct{}
	closed   bool
}

func New(environment string, capacity int, ttl time.Duration) (*Inbox, error) {
	if environment != "development" && environment != "test" {
		return nil, errors.New("auth inbox is restricted to development and test")
	}
	if capacity < 1 || capacity > 10000 || ttl <= 0 || ttl > 10*time.Minute {
		return nil, errors.New("invalid auth inbox limits")
	}
	inbox := &Inbox{capacity: capacity, ttl: ttl, stop: make(chan struct{}), done: make(chan struct{})}
	go inbox.sweep()
	return inbox, nil
}

func (inbox *Inbox) Add(message Message) error {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	if inbox.closed {
		return errors.New("auth inbox is closed")
	}
	now := time.Now().UTC()
	inbox.prune(now)
	if !message.ExpiresAt.After(now) {
		return errors.New("auth message has expired")
	}
	if maximum := now.Add(inbox.ttl); message.ExpiresAt.After(maximum) {
		message.ExpiresAt = maximum
	}
	if len(inbox.messages) == inbox.capacity {
		copy(inbox.messages, inbox.messages[1:])
		inbox.messages[len(inbox.messages)-1] = Message{}
		inbox.messages = inbox.messages[:len(inbox.messages)-1]
	}
	inbox.messages = append(inbox.messages, message)
	return nil
}

func (inbox *Inbox) List() []Message {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	inbox.prune(time.Now().UTC())
	result := make([]Message, len(inbox.messages))
	copy(result, inbox.messages)
	return result
}

func (inbox *Inbox) Close() {
	inbox.mu.Lock()
	if !inbox.closed {
		inbox.closed = true
		clear(inbox.messages)
		inbox.messages = nil
		close(inbox.stop)
	}
	inbox.mu.Unlock()
	<-inbox.done
}

func (inbox *Inbox) prune(now time.Time) {
	kept := 0
	for _, message := range inbox.messages {
		if message.ExpiresAt.After(now) {
			inbox.messages[kept] = message
			kept++
		}
	}
	clear(inbox.messages[kept:])
	inbox.messages = inbox.messages[:kept]
}

func (inbox *Inbox) sweep() {
	defer close(inbox.done)
	interval := min(inbox.ttl, time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-inbox.stop:
			return
		case now := <-ticker.C:
			inbox.mu.Lock()
			inbox.prune(now)
			inbox.mu.Unlock()
		}
	}
}
