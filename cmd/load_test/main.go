package main

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"time"

	acm "github.com/tendermint/tendermint/account"
	. "github.com/tendermint/tendermint/common"
	cfg "github.com/tendermint/tendermint/config"
	tmcfg "github.com/tendermint/tendermint/config/tendermint"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
)

// Create genesis with one validator
// Boot node
// Create N transactions
// Add them all to mempool
// Time adding to mempool
// Time clearing

var DataDir string
var LoadTestRepo = path.Join(GoPath, "src/github.com/tendermint/tendermint/cmd/load_test")
var RootDir = os.TempDir()

func copyFile(s string) {
	contents := MustReadFile(path.Join(LoadTestRepo, s))
	MustWriteFile(path.Join(RootDir, s), contents)
}

func init() {
	os.Setenv("TMROOT", RootDir)
	copyFile("genesis.json")
	copyFile("config.toml")
	copyFile("priv_validator.json")

	config := tmcfg.GetConfig("")
	cfg.ApplyConfig(config) // Notify modules of new config
}

func main() {
	if len(os.Args) < 2 {
		Exit("How many txs to send?")
	}
	N, _ := strconv.Atoi(os.Args[1])
	BenchmarkClearingMempool(N)
	//BenchmarkExecBlock(N)
	//BenchmarkExecTxSend(N)
}

func BenchmarkClearingMempool(nTxs int) {
	n := node.NewNode()
	go func() {
		err := n.Start()
		if err != nil {
			Exit(Fmt("Failed to start node: %v", err))
		}
	}()

	privValidatorFile := config.GetString("priv_validator_file")
	privVal := types.LoadPrivValidator(privValidatorFile)
	privAcc := &acm.PrivAccount{privVal.Address, privVal.PubKey, privVal.PrivKey}

	fmt.Println("Generating txs ...")
	txs := make([]types.Tx, nTxs)
	for i := 0; i < nTxs; i++ {
		sendTx := types.NewSendTx()
		sendTx.AddInputWithNonce(privVal.PubKey, 1, i+1)
		sendTx.AddOutput(LeftPadBytes([]byte{byte(i)}, 20), 1)
		sendTx.SignInput(config.GetString("chain_id"), 0, privAcc)
		txs[i] = sendTx
	}

	// subscribe to new block events
	newBlockCh := make(chan *types.Block, 1)
	evsw := n.EventSwitch()
	evsw.AddListenerForEvent("0", types.EventStringNewBlock(), func(data types.EventData) {
		newBlockCh <- data.(types.EventDataNewBlock).Block
	})

	consensusState := n.ConsensusState()
	mempool := n.MempoolReactor().Mempool
	var errs []error

	var firingStartTime, firingDoneTime, commitDoneTime time.Time
	go func() {
		// Fire away!
		fmt.Println("Firing ...")
		firingStartTime = time.Now()
		for _, tx := range txs {
			if err := mempool.AddTx(tx); err != nil {
				errs = append(errs, err)
			}
		}
		firingDoneTime = time.Now()
		mempoolAddDuration := firingDoneTime.Sub(firingStartTime)

		if len(errs) > 0 {
			fmt.Printf("Encountered %d errors:\n", len(errs))
			for _, err := range errs {
				fmt.Printf("\t%v\n", err)
			}
			os.Exit(1)
		}

		fmt.Println("Firing took", mempoolAddDuration)
		fmt.Printf("Time per transaction: %f ms\n", (float64(mempoolAddDuration.Nanoseconds())/float64(nTxs))/float64(1000000))

	}()

LOOP:
	for {
		select {
		case b := <-newBlockCh:
			st := consensusState.GetState()
			acc := st.GetAccount(privAcc.Address)
			fmt.Printf("Txs %d, Sequence %d\n", b.Header.NumTxs, acc.Sequence)
			if acc.Sequence == nTxs {
				commitDoneTime = time.Now()
				break LOOP
			}
		}
	}
	fmt.Println("Committing took:", commitDoneTime.Sub(firingDoneTime))
	os.RemoveAll(RootDir)
}

func makeBlock(state *state.State, validation *types.Validation, txs []types.Tx) (*types.Block, error) {
	if validation == nil {
		validation = &types.Validation{}
	}
	block := &types.Block{
		Header: &types.Header{
			ChainID:        state.ChainID,
			Height:         state.LastBlockHeight + 1,
			Time:           state.LastBlockTime.Add(time.Minute),
			Fees:           0,
			NumTxs:         len(txs),
			LastBlockHash:  state.LastBlockHash,
			LastBlockParts: state.LastBlockParts,
			StateHash:      nil,
		},
		LastValidation: validation,
		Data: &types.Data{
			Txs: txs,
		},
	}
	// Fill in block StateHash
	if err := state.ComputeBlockStateHash(block); err != nil {
		return nil, fmt.Errorf("Error appending initial block: %v", err)
	}
	if len(block.Header.StateHash) == 0 {
		return nil, fmt.Errorf("Expected StateHash but got nothing.")
	}
	return block, nil
}

func BenchmarkExecBlock(N int) {

	// Generate a state, save & load it.
	s0, privAccounts, _ := state.RandGenesisState(1, false, 100000, 1, false, 1000)
	privAcc := privAccounts[0]

	fmt.Println("Generating txs...")
	// make lots of txs
	txs := make([]types.Tx, N)
	for i := 0; i < N; i++ {
		sendTx := types.NewSendTx()
		sendTx.AddInputWithNonce(privAcc.PubKey, 1, i+1)
		sendTx.AddOutput(LeftPadBytes([]byte{byte(i)}, 20), 1)
		sendTx.SignInput(s0.ChainID, 0, privAcc)
		txs[i] = sendTx
	}

	// Make complete block and blockParts
	block0, err := makeBlock(s0, nil, txs)
	if err != nil {
		Exit(err.Error())
	}
	block0Parts := block0.MakePartSet()

	fmt.Println("Benchmarking ...")
	ntrials := 10
	var timers int64
	for i := 0; i < ntrials; i++ {
		s := s0.Copy()
		startTime := time.Now()
		// Now append the block to s0.
		err := state.ExecBlock(s, block0, block0Parts.Header())
		if err != nil {
			Exit(fmt.Sprintln("Error appending initial block:", err))
		}
		duration := time.Since(startTime)
		timers += duration.Nanoseconds()
	}

	avgTime := float64(timers) / float64(ntrials)
	fmt.Printf("Exec block with %d txs takes %f s\n", N, avgTime/float64(1000000000))
}

func BenchmarkExecTxSend(N int) {
	// Generate a state, save & load it.
	s0, privAccounts, _ := state.RandGenesisState(1, false, 100000, 1, false, 1000)
	privAcc := privAccounts[0]

	fmt.Println("Generating txs...")
	// make lots of txs
	txs := make([]types.Tx, N)
	for i := 0; i < N; i++ {
		sendTx := types.NewSendTx()
		sendTx.AddInputWithNonce(privAcc.PubKey, 1, i+1)
		sendTx.AddOutput(LeftPadBytes([]byte{byte(i)}, 20), 1)
		sendTx.SignInput(s0.ChainID, 0, privAcc)
		txs[i] = sendTx
	}

	blockCache := state.NewBlockCache(s0)

	fmt.Println("Benchmarking ...")
	ntrials := N
	var timers int64
	for i := 0; i < ntrials; i++ {
		startTime := time.Now()
		// Now append the block to s0.
		err := state.ExecTx(blockCache, txs[i], true, nil)
		if err != nil {
			Exit(fmt.Sprintln("Error appending initial block:", err))
		}
		duration := time.Since(startTime)
		timers += duration.Nanoseconds()
	}

	avgTime := float64(timers) / float64(ntrials)
	fmt.Printf("Can push %d tx/s (SendTx) through ExecTx \n", int(1/(avgTime/float64(1000000000))))
}
