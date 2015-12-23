package main

import (
	"fmt"

	"github.com/tendermint/tendermint/Godeps/_workspace/src/github.com/tendermint/go-wire"
	"github.com/tendermint/tendermint/types"
)

func show_validator() {
	privValidatorFile := config.GetString("priv_validator_file")
	privValidator := types.LoadOrGenPrivValidator(privValidatorFile)
	fmt.Println(string(wire.JSONBytes(privValidator.PubKey)))
}
