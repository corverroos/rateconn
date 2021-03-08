package rateconn

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	KB100 = 1024 * 100
	KB10  = 1024 * 10
)

// TODO(corver): Use a fake clock to speed up tests and make more robust.

func TestConns(t *testing.T) {
	tests := []struct {
		Name        string
		BytesPerSec int
		TotalBytes  int
		Duration    time.Duration
	}{
		{
			Name:        "100KB",
			BytesPerSec: KB100,
			TotalBytes:  KB100 * 3,
			Duration:    time.Second * 2,
		}, {
			Name:        "10KB",
			BytesPerSec: KB10,
			TotalBytes:  KB10 * 1.5,
			Duration:    time.Millisecond * 500,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			t0 := time.Now()
			err := run(test.TotalBytes, func(conn net.Conn) *Conn {
				return NewConn(conn, Bps(test.BytesPerSec))
			})
			if err != nil {
				t.Fatal(err)
			}
			actual := time.Since(t0).Seconds()
			expected := test.Duration.Seconds()
			if actual > (expected * 1.05) {
				t.Fatalf("duration too long, expected=%f, actual=%f", expected, actual)
			} else if actual < (expected * 0.95) {
				t.Fatalf("duration too short, expected=%f, actual=%f", expected, actual)
			}
		})
	}
}

func TestPool(t *testing.T) {
	pool := NewPool(Bps(KB100))

	KB50 := KB10 * 5
	t0 := time.Now()
	for i := 0; i < 4; i++ {
		// Individual connections should complete immediately, but pool delays the 2 of the 4.
		err := run(KB50, func(conn net.Conn) *Conn {
			return pool.NewConn(conn, Bps(KB50))
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	actual := time.Since(t0).Seconds()
	expected := time.Second.Seconds()
	if actual > (expected * 1.05) {
		t.Fatalf("duration too long, expected=%f, actual=%f", expected, actual)
	} else if actual < (expected * 0.95) {
		t.Fatalf("duration too short, expected=%f, actual=%f", expected, actual)
	}
}

func run(bytes int, get func(conn net.Conn) *Conn) error {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return err
	}

	var eg errgroup.Group
	eg.Go(func() error {
		t0 := time.Now()
		conn, err := l.Accept()
		if err != nil {
			return err
		}

		for {
			b, err := ioutil.ReadAll(conn)
			if err != nil && err != io.EOF {
				return err
			}
			kbps := float64(len(b)) / 1024 / time.Since(t0).Seconds()
			fmt.Printf("Read %d kB at %f kbps\n", len(b)/1024, kbps)
			return nil
		}

	})

	eg.Go(func() error {
		conn, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			return err
		}
		rconn := get(conn)

		for i := 0; i < 100; i++ {
			_, err := rconn.Write(make([]byte, bytes/100))
			if err != nil {
				return err
			}
		}

		return conn.Close()
	})

	return eg.Wait()
}
