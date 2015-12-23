package logio

import (
	"github.com/tendermint/tendermint/Godeps/_workspace/src/github.com/tendermint/go-wire"
	"io"
	"sync"
	"time"
)

type LoggedReader struct {
	io.Reader
	Log io.Writer
}

func (r LoggedReader) Read(b []byte) (n int, err error) {
	// Read from base reader
	n, err = r.Reader.Read(b)

	// Log read
	var entry LogEntry
	if err == nil {
		entry = NewReadLogEntry(b[:n], "")
	} else {
		entry = NewReadLogEntry(b[:n], err.Error())
	}
	entryBytes := wire.BinaryBytes(entry)
	_, err = r.Log.Write(entryBytes)
	return
}

type LoggedWriter struct {
	io.Writer
	Log io.Writer
}

func (w LoggedWriter) Write(b []byte) (n int, err error) {
	// Write to base writer
	n, err = w.Writer.Write(b)

	// Log write
	var entry LogEntry
	if err == nil {
		entry = NewWriteLogEntry(b[:n], "")
	} else {
		entry = NewWriteLogEntry(b[:n], err.Error())
	}
	entryBytes := wire.BinaryBytes(entry)
	_, err = w.Log.Write(entryBytes)
	return
}

//----------------------------------------

type CWriter struct {
	mtx sync.Mutex
	io.Writer
}

func (cw *CWriter) Write(p []byte) (n int, err error) {
	cw.mtx.Lock()
	defer cw.mtx.Unlock()
	return cw.Write(p)
}

//----------------------------------------

const (
	LogEntryTypeRead  = byte(0x01)
	LogEntryTypeWrite = byte(0x02)
)

type LogEntry struct {
	Type  byte
	Time  time.Time
	Bytes []byte
	Error string
}

func NewReadLogEntry(b []byte, e string) LogEntry {
	return LogEntry{LogEntryTypeRead, time.Now(), b, e}
}

func NewWriteLogEntry(b []byte, e string) LogEntry {
	return LogEntry{LogEntryTypeWrite, time.Now(), b, e}
}
