# rateconn

rateconn is a Golang library providing rate limited network connections.

```
conn, _ := net.Dial("tcp", address)
rconn := rateconn.NewConn(conn, rateconn.KBps(100))
defer rconn.Close() // Closes underlying connection as well.
for {
  rconn.Write(make([]byte, 1024)) // Write 0's at 100KB/s
}
```

## Features
- Rate limiting is implemented using [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate)
- `NewConn` wraps the provided `net.Conn` and applies the provided `*rate.Limiter`
- Only outgoing (`Write`) rate limiting is supported.
- Convenience functions for defining limits: `Bps`, `KBps`, `MBps`
- Modifying existing connection limits is supported by `*rate.Limiter.SetLimit`
- `NewPool` maintains an overall pool rate limit for all connections returned by `pool.NewConn`.
