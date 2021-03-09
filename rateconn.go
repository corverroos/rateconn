// Package rateconn provides rate limited network connections by wrapping net.Conn and net.Listen and
// applying rate limiting using golang.org/x/time/rate.
//
// Limit is a convenient way to define a rate.Limiter in bytes per second.
// Conn is a basic rate limited network connection. It is configured using function options.
// Pool groups multiple connections, applying an additional overall pool rate limit that applies to all active connections.
// Listen is similar to net.Listen and applies a connection and pool limit all accepted connections.
package rateconn

import (
	"context"
	"math"
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limit defines a rate limit in bytes per second.
type Limit float64

// Make returns an empty rate limiter for the limit.
func (l Limit) Make() *rate.Limiter {
	limiter := rate.NewLimiter(rate.Limit(l), int(l))
	limiter.ReserveN(time.Now(), limiter.Burst()) // Start limiter empty to avoid initial bursts.
	return limiter
}

const (
	Bps Limit = 1
	KBps Limit = 1 << 10
	MBps Limit = 1 << 20
	Inf Limit = math.MaxFloat64
)

// WithRXLimiter returns an option to add an additional rx limiter to a connection.
func WithRXLimiter(l *rate.Limiter) func(*Conn) {
	return func(conn *Conn) {
		conn.rxlimiters = append(conn.rxlimiters, l)
	}
}

// WithTXLimiter returns an option to add an additional tx limiter to a connection.
func WithTXLimiter(l *rate.Limiter) func(*Conn) {
	return func(conn *Conn) {
		conn.txlimiters = append(conn.txlimiters, l)
	}
}

// WithCloseFunc returns an option to add a close callback to a connection.
func WithCloseFunc(fn func()) func(*Conn) {
	return func(conn *Conn) {
		conn.closeFunc = fn
	}
}

// NewConn returns a new optional rate limited network connection.
func NewConn(conn net.Conn, opts ...func(*Conn)) *Conn {
	c := &Conn{
		Conn:     conn,
		ctx:      context.Background(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Conn provides a rate limited network connection. It implements net.Conn.
type Conn struct {
	net.Conn
	rxlimiters []*rate.Limiter
	txlimiters []*rate.Limiter
	closeFunc func()
	ctx      context.Context
}

// Write writes data to the connection but blocks while it exceeds any of internal tx rate limits.
func (c *Conn) Write(b []byte) (n int, err error) {
	if len(c.txlimiters) == 0 {
		return c.Conn.Write(b)
	}

	// Use minimum per second rate limit
	perSec := math.MaxInt64
	for _, limiter := range c.txlimiters {
		if float64(limiter.Limit()) > float64(perSec) {
			continue
		}
		perSec = int(limiter.Limit())
	}

	div := len(b) / perSec
	mod := len(b) % perSec

	for i := 0; i < div; i++ {
		for _, limiter := range c.txlimiters {
			err := limiter.WaitN(c.ctx, perSec)
			if err != nil {
				return n, err
			}
		}

		m, err := c.Conn.Write(b[i*perSec : i+1*perSec])
		n += m
		if err != nil {
			return n, err
		}
	}

	for _, limiter := range c.txlimiters {
		err := limiter.WaitN(c.ctx, mod)
		if err != nil {
			return n, err
		}
	}

	m, err := c.Conn.Write(b[div*perSec:])
	n += m
	return n, err
}


// Read reads data from the connection but blocks while it exceeds any of internal rx rate limits.
func (c *Conn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err != nil {
		return n, err
	} else if len(c.rxlimiters) == 0 {
		return n, nil
	}

	// Use minimum per second rate limit
	perSec := math.MaxInt64
	for _, limiter := range c.rxlimiters {
		if float64(limiter.Limit()) > float64(perSec) {
			continue
		}
		perSec = int(limiter.Limit())
	}

	div := n / perSec
	mod := n % perSec

	for i := 0; i < div; i++ {
		for _, limiter := range c.rxlimiters {
			err := limiter.WaitN(c.ctx, perSec)
			if err != nil {
				return n, err
			}
		}
	}

	for _, limiter := range c.rxlimiters {
		err := limiter.WaitN(c.ctx, mod)
		if err != nil {
			return n, err
		}
	}

	return n, err
}

func (c *Conn) Close() error {
	if c.closeFunc != nil {
		defer c.closeFunc()
	}
	return c.Conn.Close()
}


// NewPool returns a pool providing rate limited connections with an overall pool limit.
// Both pool and connection limits are applied to rx and tx separately.
func NewPool(poolLimit, connLimit Limit) *Pool {
	return &Pool{
		poolRXLimiter: poolLimit.Make(),
		poolTXLimiter: poolLimit.Make(),
		connLimit: connLimit,
		connLimiters: make(map[int64]*rate.Limiter),
	}
}

// Pool provides rate limited connections with an overall pool limit.
// Connections can be added to pool via NewConn or Dial.
type Pool struct {
	mu sync.Mutex
	poolRXLimiter *rate.Limiter
	poolTXLimiter *rate.Limiter
	connLimit Limit
	connLimiters map[int64]*rate.Limiter
}

// NewConn returns a rate limited connection. It will also adhere to the overall pool and per connection limits.
func (p *Pool) NewConn(conn net.Conn) *Conn {
	p.mu.Lock()
	defer p.mu.Unlock()

	rxIdx := time.Now().UnixNano()
	txIdx := time.Now().UnixNano()
	connRXLimiter := p.connLimit.Make()
	connTXLimiter := p.connLimit.Make()

	p.connLimiters[rxIdx] = connRXLimiter
	p.connLimiters[txIdx] = connTXLimiter

	return NewConn(conn,
		WithRXLimiter(p.poolRXLimiter),
		WithRXLimiter(connRXLimiter),
		WithTXLimiter(p.poolTXLimiter),
		WithTXLimiter(connTXLimiter),
		WithCloseFunc(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			delete(p.connLimiters,rxIdx)
			delete(p.connLimiters,txIdx)
		}),
	)
}

// NewConn returns a rate limited connection. It will also adhere to the overall pool and per connection limits.
func (p *Pool) Dial(network, address string) (*Conn, error) {
	c, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	return p.NewConn(c), nil
}

// SetPoolLimit sets a new overall pool rate limit.
func (p *Pool) SetPoolLimit(limit Limit) {
	p.mu.Lock()
	defer p.mu.Unlock()

	limiter := limit.Make()
	p.poolTXLimiter.SetLimit(limiter.Limit())
	p.poolTXLimiter.SetBurst(limiter.Burst())
	p.poolRXLimiter.SetLimit(limiter.Limit())
	p.poolRXLimiter.SetBurst(limiter.Burst())
}

// SetConnLimit sets a new per connection rate limit and a new rate limit for all active connections.
func (p *Pool) SetConnLimit(limit Limit) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.connLimit = limit

	limiter := limit.Make()
	for _, connLimiter := range p.connLimiters {
		connLimiter.SetLimit(limiter.Limit())
		connLimiter.SetBurst(limiter.Burst())
	}
}

// Listen returns a Listener for the provided network and address and rate limits.
func Listen(network, address string, poolLimit, connLimit Limit) (*Listener, error) {
	l, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}

	return &Listener{
		Listener: l,
		pool: NewPool(poolLimit, connLimit),
	}, nil
}

// Listener wraps net.Listener and Pool and returns rate limited connections on Accept.
// It supports an overall pool rate limit for all active connections
// and a per connection rate limit for all accepted connections.
type Listener struct {
	net.Listener
	pool *Pool
}

func (l *Listener) Accept() (net.Conn, error){
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	return l.pool.NewConn(c), nil
}

func (l *Listener) SetPoolLimit(limit Limit){
	l.pool.SetPoolLimit(limit)
}

func (l *Listener) SetConnLimit(limit Limit){
	l.pool.SetConnLimit(limit)
}

var (
	_ net.Conn = (*Conn)(nil)
	_ net.Listener = (*Listener)(nil)
)
