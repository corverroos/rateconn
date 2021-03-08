package rateconn

import (
	"context"
	"net"

	"golang.org/x/time/rate"
)

// Bps is a convenience function returning a bytes per second rate limiter.
func Bps(bytesPerSec int) *rate.Limiter {
	return rate.NewLimiter(rate.Limit(bytesPerSec), bytesPerSec)
}

// KBps is a convenience function returning a kilobytes per second rate limiter.
func KBps(kiloBytesPerSec float64) *rate.Limiter {
	return Bps(int(kiloBytesPerSec * 1024))
}

// MBps is a convenience function returning a megabytes per second rate limiter.
func MBps(megaBytesPerSecond float64) *rate.Limiter {
	return KBps(megaBytesPerSecond * 1024)
}

// NewConn returns a new rate limited network connection. The rate limiters need to be configured in bytes per second, see Bps.
func NewConn(conn net.Conn, limiters ...*rate.Limiter) *Conn {
	return &Conn{
		Conn:     conn,
		limiters: limiters,
		ctx:      context.Background(),
	}
}

// Conn provides a rate limited network connection.
type Conn struct {
	net.Conn
	limiters []*rate.Limiter
	ctx      context.Context
}

// Write writes data to the connection but blocks while it exceeds any of internal rate limits.
func (c *Conn) Write(b []byte) (n int, err error) {
	// Use minimum per second rate limit
	var perSec int
	for i, limiter := range c.limiters {
		temp := int(limiter.Limit())
		if i == 0 || perSec < temp {
			perSec = temp
		}
	}

	div := len(b) / perSec
	mod := len(b) % perSec

	for i := 0; i < div; i++ {
		for _, limiter := range c.limiters {
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

	for _, limiter := range c.limiters {
		err := limiter.WaitN(c.ctx, mod)
		if err != nil {
			return n, err
		}
	}

	m, err := c.Conn.Write(b[div*perSec:])
	n += m
	return n, err
}

// NewPool returns a pool providing rate limited connections with an overall pool limit.
func NewPool(poolLimiter *rate.Limiter) *Pool {
	return &Pool{
		poolLimiter: poolLimiter,
	}
}

// Pool provides rate limited connections with an overall pool limit.
type Pool struct {
	poolLimiter *rate.Limiter
}

// NewConn returns a rate limited connection with the provided rate limit. It will also adhere to the overall pool limit.
func (p *Pool) NewConn(conn net.Conn, connLimiter *rate.Limiter) *Conn {
	return NewConn(conn, connLimiter, p.poolLimiter)
}
