package client

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tendermint/go-rpc/client"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
	"github.com/tendermint/tendermint/types"
)

func CommitTx(addr string, txBytes []byte) error {
	wsc := rpcclient.NewWSClient("ws://" + addr + "/websocket")
	if _, err := wsc.Start(); err != nil {
		return err
	}

	if err := wsc.Subscribe(types.EventStringNewBlock()); err != nil {
		return err
	}

	defer func() {
		wsc.Unsubscribe(types.EventStringNewBlock())
		wsc.Stop()
	}()

	c := rpcclient.NewClientURI("http://" + addr)
	params := map[string]interface{}{
		"tx": hex.EncodeToString(txBytes),
	}
	var result ctypes.TMResult
	_, err := c.Call("broadcast_tx", params, &result)
	if err != nil {
		return err
	}

	timeout := time.After(time.Second * 5)
	for {
		select {
		case data := <-wsc.ResultsCh:
			if data == nil {
				return fmt.Errorf("Websocket channel closed")
			}
			_, edata, err := ctypes.UnmarshalEvent(data)
			if err != nil {
				return fmt.Errorf("Error unmarshalling websocket result: %v", err)
			}

			block := edata.(types.EventDataNewBlock).Block
			for _, tx := range block.Data.Txs {
				if bytes.Equal(tx, txBytes) {
					return nil
				}
			}
		case <-timeout:
			return fmt.Errorf("Timed out waiting for tx to commit (%x)", txBytes)
		}
	}
	return nil
}
