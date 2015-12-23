package types

import (
	"github.com/tendermint/tendermint/Godeps/_workspace/src/github.com/tendermint/go-wire"
)

// Functions to generate eventId strings

// Reserved
func EventStringBond() string    { return "Bond" }
func EventStringUnbond() string  { return "Unbond" }
func EventStringRebond() string  { return "Rebond" }
func EventStringDupeout() string { return "Dupeout" }
func EventStringFork() string    { return "Fork" }

func EventStringNewBlock() string         { return "NewBlock" }
func EventStringNewRound() string         { return "NewRound" }
func EventStringNewRoundStep() string     { return "NewRoundStep" }
func EventStringTimeoutPropose() string   { return "TimeoutPropose" }
func EventStringCompleteProposal() string { return "CompleteProposal" }
func EventStringPolka() string            { return "Polka" }
func EventStringUnlock() string           { return "Unlock" }
func EventStringLock() string             { return "Lock" }
func EventStringRelock() string           { return "Relock" }
func EventStringTimeoutWait() string      { return "TimeoutWait" }
func EventStringVote() string             { return "Vote" }
func EventStringApp() string              { return "App" }

//----------------------------------------

const (
	EventDataTypeNewBlock = byte(0x01)
	EventDataTypeFork     = byte(0x02)
	EventDataTypeTx       = byte(0x03)
	EventDataTypeApp      = byte(0x04) // Custom app event

	EventDataTypeRoundState = byte(0x11)
	EventDataTypeVote       = byte(0x12)
)

type EventData interface {
	AssertIsEventData()
}

var _ = wire.RegisterInterface(
	struct{ EventData }{},
	wire.ConcreteType{EventDataNewBlock{}, EventDataTypeNewBlock},
	// wire.ConcreteType{EventDataFork{}, EventDataTypeFork },
	wire.ConcreteType{EventDataTx{}, EventDataTypeTx},
	wire.ConcreteType{EventDataApp{}, EventDataTypeApp},
	wire.ConcreteType{&EventDataRoundState{}, EventDataTypeRoundState}, // a pointer because we use it internally
	wire.ConcreteType{EventDataVote{}, EventDataTypeVote},
)

// Most event messages are basic types (a block, a transaction)
// but some (an input to a call tx or a receive) are more exotic

type EventDataNewBlock struct {
	Block *Block `json:"block"`
}

// All txs fire EventDataTx
type EventDataTx struct {
	Tx        Tx     `json:"tx"`
	Return    []byte `json:"return"`
	Exception string `json:"exception"`
}

type EventDataApp struct {
	Key  string `json:"key"`
	Data []byte `json:"bytes"`
}

type EventDataRoundState struct {
	Height int    `json:"height"`
	Round  int    `json:"round"`
	Step   string `json:"step"`

	// private, not exposed to websockets
	rs interface{}
}

func (edrs *EventDataRoundState) RoundState() interface{} {
	return edrs.rs
}

func (edrs *EventDataRoundState) SetRoundState(rs interface{}) {
	edrs.rs = rs
}

type EventDataVote struct {
	Index   int
	Address []byte
	Vote    *Vote
}

func (_ EventDataNewBlock) AssertIsEventData()   {}
func (_ EventDataTx) AssertIsEventData()         {}
func (_ EventDataApp) AssertIsEventData()        {}
func (_ EventDataRoundState) AssertIsEventData() {}
func (_ EventDataVote) AssertIsEventData()       {}
