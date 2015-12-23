package logio

import (
	"github.com/tendermint/tendermint/Godeps/_workspace/src/github.com/tendermint/go-wire"
	"io"
	"net"
	"sync"
)

type LoggedConn struct {
	net.Conn
	mtx sync.Mutex
	Log io.Writer
}

func NewLoggedConn(conn net.Conn, log io.Writer) *LoggedConn {
	return &LoggedConn{
		Conn: conn,
		Log:  log,
	}
}

func (lc *LoggedConn) Read(b []byte) (n int, err error) {
	// Read from base reader
	n, err = lc.Conn.Read(b)

	// Log read
	var entry LogEntry
	if err == nil {
		entry = NewReadLogEntry(b[:n], "")
	} else {
		entry = NewReadLogEntry(b[:n], err.Error())
	}
	entryBytes := wire.BinaryBytes(entry)
	_, err = lc.Log.Write(entryBytes)
	return
}

func (lc *LoggedConn) Write(b []byte) (n int, err error) {
	// Write to base writer
	n, err = lc.Conn.Write(b)

	// Log write
	var entry LogEntry
	if err == nil {
		entry = NewWriteLogEntry(b[:n], "")
	} else {
		entry = NewWriteLogEntry(b[:n], err.Error())
	}
	entryBytes := wire.BinaryBytes(entry)
	_, err = lc.Log.Write(entryBytes)
	return
}
