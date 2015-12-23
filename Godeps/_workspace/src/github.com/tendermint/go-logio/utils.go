package logio

import (
	"fmt"
	"io"
	"os"

	. "github.com/tendermint/tendermint/Godeps/_workspace/src/github.com/tendermint/go-common"
	"github.com/tendermint/tendermint/Godeps/_workspace/src/github.com/tendermint/go-wire"
)

func PrintFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		Exit(err.Error())
	}
	PrintReader(file)
}

func PrintReader(reader io.Reader) {
	for {
		var entry LogEntry
		var n int
		var err error
		wire.ReadBinaryPtr(&entry, reader, 0, &n, &err)
		if err == io.EOF {
			return
		} else if err != nil {
			Exit(err.Error())
		}

		switch entry.Type {
		case LogEntryTypeRead:
			fmt.Println(Fmt("%v: %v %v",
				entry.Time,
				Cyan(Fmt("%X", entry.Bytes)),
				Red(entry.Error),
			))
		case LogEntryTypeWrite:
			fmt.Println(Fmt("%v: %v %v",
				entry.Time,
				Yellow(Fmt("%X", entry.Bytes)),
				Red(entry.Error),
			))
		default:
			Exit(Fmt("Unknown log entry type %X", entry.Type))
		}
	}
}
