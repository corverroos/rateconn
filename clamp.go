package rateconn

import (
	"golang.org/x/time/rate"
	"net"
	"sync"
	"time"
)

// NewClamp returns a clamp providing rate limited connections with an overall clamp limit.
// Both clamp and connection limits are applied to rx and tx separately.
func NewClamp(clampLimit, connLimit Limit) *Clamp {
	return &Clamp{
		clampRXLimiter: clampLimit.Make(),
		clampTXLimiter: clampLimit.Make(),
		connLimit:      connLimit,
		connLimiters:   make(map[int64]*rate.Limiter),
	}
}

// Clamp applies a per connection rate limit as well as an overall clamp rate limit to all active connections.
// Connections can be added via Wrap, Listen or Dial.
type Clamp struct {
	mu             sync.Mutex
	clampRXLimiter *rate.Limiter
	clampTXLimiter *rate.Limiter
	connLimit      Limit
	connLimiters   map[int64]*rate.Limiter
}

// Wrap returns a rate limited connection. It will adhere to the overall clamp and per connection limits.
func (c *Clamp) Wrap(conn net.Conn) *Conn {
	c.mu.Lock()
	defer c.mu.Unlock()

	rxIdx := time.Now().UnixNano()
	txIdx := time.Now().UnixNano()
	connRXLimiter := c.connLimit.Make()
	connTXLimiter := c.connLimit.Make()

	c.connLimiters[rxIdx] = connRXLimiter
	c.connLimiters[txIdx] = connTXLimiter

	return Wrap(conn,
		WithRXLimiter(c.clampRXLimiter),
		WithRXLimiter(connRXLimiter),
		WithTXLimiter(c.clampTXLimiter),
		WithTXLimiter(connTXLimiter),
		WithCloseFunc(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			delete(c.connLimiters, rxIdx)
			delete(c.connLimiters, txIdx)
		}),
	)
}

func (c *Clamp) Listen(network, address string) (*listener, error) {
	l, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}

	return &listener{
		Listener: l,
		wrapFunc: c.Wrap,
	}, nil
}

// Wrap returns a rate limited connection. It will adhere to the overall clamp and per connection limits.
func (c *Clamp) Dial(network, address string) (*Conn, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	return c.Wrap(conn), nil
}

// SetClampLimit sets a new overall clamp rate limit.
func (c *Clamp) SetClampLimit(limit Limit) {
	c.mu.Lock()
	defer c.mu.Unlock()

	limiter := limit.Make()
	c.clampTXLimiter.SetLimit(limiter.Limit())
	c.clampTXLimiter.SetBurst(limiter.Burst())
	c.clampRXLimiter.SetLimit(limiter.Limit())
	c.clampRXLimiter.SetBurst(limiter.Burst())
}

// SetConnLimit sets a new per connection rate limit and a new rate limit for all active connections.
func (c *Clamp) SetConnLimit(limit Limit) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connLimit = limit

	limiter := limit.Make()
	for _, connLimiter := range c.connLimiters {
		connLimiter.SetLimit(limiter.Limit())
		connLimiter.SetBurst(limiter.Burst())
	}
}
