package kernel

import (
	"sync"
	"time"
)

// Clock es la única fuente de tiempo del sistema (CLAUDE.md regla 5). Siempre devuelve UTC.
type Clock interface {
	Now() time.Time
}

// SystemClock usa el reloj del sistema.
type SystemClock struct{}

// Now devuelve el instante actual en UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// FakeClock es un reloj controlable para pruebas. Seguro para uso concurrente.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock crea un reloj detenido en t.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t.UTC()}
}

// Now devuelve el instante fijado.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set fija el instante.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t.UTC()
}

// Advance adelanta el reloj d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
