// Package rateconn provides rate limited network connections by wrapping net.Conn and net.Listen and
// applying rate limiting using golang.org/x/time/rate.
//
// Limit is a convenient way to define a rate.Limiter in bytes per second.
// Conn is a basic rate limited network connection. It is configured using function options.
// Clamp groups multiple connections, applying an additional overall clamp rate limit that applies to all active connections.
// Listen is similar to net.Listen and applies a connection and pool limit all accepted connections.
package rateconn

import (
	"context"
	"math"
	"net"
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
	Bps  Limit = 1
	KBps Limit = 1 << 10
	MBps Limit = 1 << 20
	Inf  Limit = math.MaxFloat64
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

// Wrap returns a optional rate limited network connection.
func Wrap(conn net.Conn, opts ...func(*Conn)) *Conn {
	c := &Conn{
		Conn: conn,
		ctx:  context.Background(),
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
	closeFunc  func()
	ctx        context.Context
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

// Listen returns a listener that accepts optional rate limited network connections.
func Listen(network, address string, opts ...func(*Conn)) (net.Listener, error) {
	l, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}

	return &listener{
		Listener: l,
		wrapFunc: func(conn net.Conn) *Conn {
			return Wrap(conn, opts...)
		},
	}, nil
}

// listener wraps net.Listener and returns rate limited connections on Accept.
type listener struct {
	net.Listener
	wrapFunc func(conn net.Conn) *Conn
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	return l.wrapFunc(c), nil
}

// Dial returns a rate limited connection. It will adhere to the overall clamp and per connection limits.
func Dial(network, address string, opts ...func(*Conn)) (*Conn, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	return Wrap(conn, opts...), nil
}

var (
	_ net.Conn     = (*Conn)(nil)
	_ net.Listener = (*listener)(nil)
)
