package p2p

import (
	"math/rand"
	"net"
	"time"
)

const MaxDelayMilliseconds int = 3000

func randomDuration() time.Duration {
	return time.Millisecond * time.Duration(rand.Int()%MaxDelayMilliseconds)
}

func FuzzConn(conn net.Conn) net.Conn {
	return &FuzzedConnection{
		conn:  conn,
		start: time.After(time.Second * 10),
	}
}

type FuzzedConnection struct {
	conn  net.Conn
	fuzz  bool
	start <-chan time.Time
}

func (fc *FuzzedConnection) shouldFuzz() bool {
	if fc.fuzz {
		return true
	}

	select {
	case <-fc.start:
		fc.fuzz = true
	default:
	}
	return false
}

func (fc *FuzzedConnection) Read(data []byte) (n int, err error) {
	if fc.shouldFuzz() {
		time.Sleep(randomDuration())
	}
	return fc.conn.Read(data)
}

func (fc *FuzzedConnection) Write(data []byte) (n int, err error) {
	if fc.shouldFuzz() {
		time.Sleep(randomDuration())
	}
	return fc.conn.Write(data)
}

// Implements net.Conn
func (fc *FuzzedConnection) Close() error                  { return fc.conn.Close() }
func (fc *FuzzedConnection) LocalAddr() net.Addr           { return fc.conn.LocalAddr() }
func (fc *FuzzedConnection) RemoteAddr() net.Addr          { return fc.conn.RemoteAddr() }
func (fc *FuzzedConnection) SetDeadline(t time.Time) error { return fc.conn.SetDeadline(t) }
func (fc *FuzzedConnection) SetReadDeadline(t time.Time) error {
	return fc.conn.SetReadDeadline(t)
}
func (fc *FuzzedConnection) SetWriteDeadline(t time.Time) error {
	return fc.conn.SetWriteDeadline(t)
}
