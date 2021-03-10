package rateconn_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"sync"
	"testing"
	"time"

	"github.com/corverroos/rateconn"

	"golang.org/x/sync/errgroup"
)

const (
	Inf   = rateconn.Inf
	KB10  = rateconn.KBps * 10
	KB50  = rateconn.KBps * 50
	KB100 = rateconn.KBps * 100

	Sec0_5 = time.Millisecond * 500
	Sec1   = time.Second
	Sec2   = time.Second * 2
)

// TODO(corver): Use a fake clock to speed up tests and make more robust.

func TestRateConn(t *testing.T) {
	tests := []struct {
		Name        string
		RxClampLimit rateconn.Limit
		RxConnLimit rateconn.Limit
		TxLimit     rateconn.Limit
		TxBytes     rateconn.Limit
		Threads     int
		ExpDuration time.Duration
	}{
		{
			Name:        "1 Tx 100KB Rx Inf",
			RxClampLimit: Inf,
			RxConnLimit: Inf,
			TxLimit:     KB100,
			TxBytes:     KB50,
			Threads:     1,
			ExpDuration: Sec0_5,
		},
		{
			Name:        "1 Tx Inf Rx 100KB",
			RxClampLimit: Inf,
			RxConnLimit: KB100,
			TxLimit:     Inf,
			TxBytes:     KB50,
			Threads:     1,
			ExpDuration: Sec0_5, // Same as prev since tx limit just moved to rx.
		},
		{
			Name:        "4 Tx 100KB Rx Inf",
			RxClampLimit: Inf,
			RxConnLimit: Inf,
			TxLimit:     KB100,
			TxBytes:     KB50,
			Threads:     4,
			ExpDuration: Sec0_5, // Same as first since rx limit is inf
		},
		{
			Name:        "4 Tx 100KB Rx 100KB",
			RxClampLimit: Inf,
			RxConnLimit: KB100,
			TxLimit:     KB100,
			TxBytes:     KB50,
			Threads:     4,
			ExpDuration: Sec0_5, // Same as first since rx limit is sufficient
		}, {
			Name:        "4 Tx 100KB Rx 100KB Clamp 200 KB",
			RxClampLimit: KB100 * 2,
			RxConnLimit: KB100,
			TxLimit:     KB100,
			TxBytes:     KB50,
			Threads:     4,
			ExpDuration: Sec1, // Double prev, due to rx clamp limit
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			dur, err := run(int(test.TxBytes), test.TxLimit, test.RxClampLimit, test.RxConnLimit, test.Threads)
			if err != nil {
				t.Fatal(err)
			}
			actual := dur.Seconds()
			expected := test.ExpDuration.Seconds()
			if actual > (expected * 1.05) {
				t.Fatalf("duration too long, expected=%f, actual=%f", expected, actual)
			} else if actual < (expected * 0.95) {
				t.Fatalf("duration too short, expected=%f, actual=%f", expected, actual)
			}
		})
	}
}

func run(bytes int, txConnLimit, rxClampLimit, rxConnLimit rateconn.Limit, threads int) (time.Duration, error) {
	rxClamp := rateconn.NewClamp(rxClampLimit, rxConnLimit)
	l, err := rxClamp.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()

	var ioGroup errgroup.Group
	var connectGroup sync.WaitGroup
	durations := make(chan time.Duration, threads)

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			ioGroup.Go(func() error {
				defer func(t0 time.Time) {
					durations <- time.Since(t0)
				}(time.Now())

				b, err := ioutil.ReadAll(conn)
				if err != nil && err != io.EOF {
					return err
				}
				fmt.Printf("Read %d kB\n", len(b)/1024)
				return nil
			})
			connectGroup.Done()
		}
	}()

	txClamp := rateconn.NewClamp(Inf, txConnLimit)
	connectGroup.Add(threads)
	for i := 0; i < threads; i++ {
		ioGroup.Go(func() error {
			conn, err := txClamp.Dial("tcp", l.Addr().String())
			if err != nil {
				return err
			}

			for i := 0; i < 100; i++ {
				_, err := conn.Write(make([]byte, bytes/100))
				if err != nil {
					return err
				}
			}

			return conn.Close()
		})
	}
	connectGroup.Wait()
	if err := ioGroup.Wait(); err != nil {
		return 0, err
	}

	close(durations)
	var max time.Duration
	for d := range durations {
		if d > max {
			max = d
		}
	}
	return max, nil
}
