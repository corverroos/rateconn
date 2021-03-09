# rateconn

rateconn is a Golang library providing rate limited network connections. 
It wraps net.Conn and net.Listen and applies rate limiting using golang.org/x/time/rate.

```
// Create a receive listener pool with overall limit of 1MB/s and per connection limit of 50KB/s
listener, _ := rateconn.Listen("tcp", "localhost:0", rateconn.MBps, rateconn.KBps*50)
defer listener.Close()
go func() {
  conn, _ := listener.Accept
  _, _ = io.ReadAll(conn) // Read at 50KB/ps
}()

// Create a transmit pool with overall limit of 10MB/s and per connection limit of 100KB/s
pool := rateconn.NewPool(rateconn.MBps*10, rateconn.KBps*100) 

// Dial a rate limited connection
conn, _ := pool.Dial("tcp", listener.Addr().String())
defer rconn.Close() 
for {
  rconn.Write(make([]byte, 1024)) // Write 0's at max 100KB/s (but 50KB/s due to rx limit)
}
```

## Features
- Rate limiting is implemented using [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate)
- `NewConn` wraps the provided `net.Conn` and applies the provided options.
- Options include multiple `TXLimit`s and `RXLimit`s.
- Convenience functions for defining limits: `Bps`, `KBps`, `MBps`
- `Pool` groups multiple connections, applying an additional overall pool rate limit that applies to all active connections.
- `Listen` is similar to `net.Listen` and applies a connection and pool limit all accepted connections.
- Connections can be added to a pool by `NewConn` or `Dial`.
- Modifying existing limits is supported via `SetPoolLimit` and `SetConnLimit`.
