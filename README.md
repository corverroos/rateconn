# rateconn

rateconn is a Golang library providing rate limited network connections. 
It wraps `net.Conn` and applies rate limiting using [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate).

```go
// Create a clamp with overall limit of 1MB/s and per connection limit of 50KB/s
clamp := rateconn.Clamp(rateconn.MBps, rateconn.KBps*50)

listener, _ := clamp.Listen("tcp", "localhost:0")
defer listener.Close()
go func() {
  conn, _ := listener.Accept
  _, _ = io.ReadAll(conn) // Read at max 50KB/ps
}()

// Dial a rate limited connection
conn, _ := clamp.Dial("tcp", listener.Addr().String())
defer conn.Close() 
for {
  rconn.Write(make([]byte, 1024)) // Write 0's at 50KB/s
}
```

### Features
- `Conn` is a rate limited network connection and is the basic building block of this package. 
- `Conn` wraps a `net.Conn` and applies rx and tx rate limits.
- `Wrap` creates a new `Conn` configured via functional options.
- Options include multiple `TXLimit`s and `RXLimit`s.
- Convenience functions for defining limits: `Bps`, `KBps`, `MBps`
- Convenience functions `Listen` and `Dial` call the equivalent `net` package functions and return a wrapped `Conn`. 
- `Clamp` groups multiple rate limited connections, an applies an additional overall clamp limit.
- `Clamp` only supports symmetrical rx and tx limits.
- Connections can be added to a `Clamp` via `Wrap`, `Dial`, `Listen` methods. 
- Modifying existing limits is supported via `SetClampLimit` and `SetConnLimit`.

### TODO
- Figure out a nice API for defining different `Clamp` rx and tx limits.
- Align single connection configuration and clamp configuration.
- Use a fake clock for deterministic and more performant testing.
