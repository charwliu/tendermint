package node

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	. "github.com/tendermint/go-common"
	"github.com/tendermint/go-crypto"
	dbm "github.com/tendermint/go-db"
	"github.com/tendermint/go-merkle"
	"github.com/tendermint/go-p2p"
	"github.com/tendermint/go-wire"
	bc "github.com/tendermint/tendermint/blockchain"
	"github.com/tendermint/tendermint/consensus"
	"github.com/tendermint/tendermint/events"
	mempl "github.com/tendermint/tendermint/mempool"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/rpc"
	"github.com/tendermint/tendermint/rpc/core"
	"github.com/tendermint/tendermint/rpc/server"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
)

import _ "net/http/pprof"

type Node struct {
	sw               *p2p.Switch
	evsw             *events.EventSwitch
	book             *p2p.AddrBook
	blockStore       *bc.BlockStore
	pexReactor       *p2p.PEXReactor
	bcReactor        *bc.BlockchainReactor
	mempoolReactor   *mempl.MempoolReactor
	consensusState   *consensus.ConsensusState
	consensusReactor *consensus.ConsensusReactor
	privValidator    *types.PrivValidator
	genesisDoc       *types.GenesisDoc
	privKey          crypto.PrivKeyEd25519
}

func NewNode() *Node {
	// Get BlockStore
	blockStoreDB := dbm.GetDB("blockstore")
	blockStore := bc.NewBlockStore(blockStoreDB)

	// Get State
	state := getState()

	// Create two proxyAppCtx connections,
	// one for the consensus and one for the mempool.
	proxyAddr := config.GetString("proxy_app")
	proxyAppCtxMempool := getProxyApp(proxyAddr, state.LastAppHash)
	proxyAppCtxConsensus := getProxyApp(proxyAddr, state.LastAppHash)

	// add the chainid to the global config
	config.Set("chain_id", state.ChainID)

	// Get PrivValidator
	privValidatorFile := config.GetString("priv_validator_file")
	privValidator := types.LoadOrGenPrivValidator(privValidatorFile)

	// Generate node PrivKey
	privKey := crypto.GenPrivKeyEd25519()

	// Make event switch
	eventSwitch := events.NewEventSwitch()
	_, err := eventSwitch.Start()
	if err != nil {
		Exit(Fmt("Failed to start switch: %v", err))
	}

	// Make PEXReactor
	book := p2p.NewAddrBook(config.GetString("addrbook_file"))
	pexReactor := p2p.NewPEXReactor(book)

	// Make BlockchainReactor
	bcReactor := bc.NewBlockchainReactor(state.Copy(), proxyAppCtxConsensus, blockStore, config.GetBool("fast_sync"))

	// Make MempoolReactor
	mempool := mempl.NewMempool(proxyAppCtxMempool)
	mempoolReactor := mempl.NewMempoolReactor(mempool)

	// Make ConsensusReactor
	consensusState := consensus.NewConsensusState(state.Copy(), proxyAppCtxConsensus, blockStore, mempool)
	consensusReactor := consensus.NewConsensusReactor(consensusState, blockStore, config.GetBool("fast_sync"))
	if privValidator != nil {
		consensusReactor.SetPrivValidator(privValidator)
	}

	// Make p2p network switch
	sw := p2p.NewSwitch()
	sw.AddReactor("PEX", pexReactor)
	sw.AddReactor("MEMPOOL", mempoolReactor)
	sw.AddReactor("BLOCKCHAIN", bcReactor)
	sw.AddReactor("CONSENSUS", consensusReactor)

	// add the event switch to all services
	// they should all satisfy events.Eventable
	SetFireable(eventSwitch, bcReactor, mempoolReactor, consensusReactor)

	// run the profile server
	profileHost := config.GetString("prof_laddr")
	if profileHost != "" {
		go func() {
			log.Warn("Profile server", "error", http.ListenAndServe(profileHost, nil))
		}()
	}

	dbHost := config.GetString("influxdb_host")
	dbName := config.GetString("influxdb_name")
	if dbHost != "" && dbName != "" {
		nodeID := fmt.Sprintf("%x", privValidator.PubKey[:])
		callback := logEventCallback(dbHost, dbName, nodeID)
		eventSwitch.AddListenerForEvent("logEvents", types.EventStringConsensusMessage(), callback)
	}

	return &Node{
		sw:               sw,
		evsw:             eventSwitch,
		book:             book,
		blockStore:       blockStore,
		pexReactor:       pexReactor,
		bcReactor:        bcReactor,
		mempoolReactor:   mempoolReactor,
		consensusState:   consensusState,
		consensusReactor: consensusReactor,
		privValidator:    privValidator,
		genesisDoc:       state.GenesisDoc,
		privKey:          privKey,
	}
}

// Call Start() after adding the listeners.
func (n *Node) Start() error {
	n.book.Start()
	n.sw.SetNodeInfo(makeNodeInfo(n.sw, n.privKey))
	n.sw.SetNodePrivKey(n.privKey)
	if _, err := n.sw.Start(); err != nil {
		return err
	}

	replayFile := config.GetString("replay_file")
	if replayFile != "" {
		msgChan := make(chan consensus.ConsensusMessage)
		go ConvertReplayFileToMessages(replayFile, msgChan)
		n.consensusReactor.ReplayMessages(msgChan)
	}
	return nil
}

func (n *Node) Stop() {
	log.Notice("Stopping Node")
	// TODO: gracefully disconnect from peers.
	n.sw.Stop()
	n.book.Stop()
}

// Add the event switch to reactors, mempool, etc.
func SetFireable(evsw *events.EventSwitch, eventables ...events.Eventable) {
	for _, e := range eventables {
		e.SetFireable(evsw)
	}
}

// Add a Listener to accept inbound peer connections.
// Add listeners before starting the Node.
// The first listener is the primary listener (in NodeInfo)
func (n *Node) AddListener(l p2p.Listener) {
	log.Notice(Fmt("Added %v", l))
	n.sw.AddListener(l)
	n.book.AddOurAddress(l.ExternalAddress())
}

// Dial a list of seeds in random order
// Spawns a go routine for each dial
func (n *Node) DialSeed() {
	// permute the list, dial them in random order.
	seeds := strings.Split(config.GetString("seeds"), ",")
	perm := rand.Perm(len(seeds))
	for i := 0; i < len(perm); i++ {
		go func(i int) {
			time.Sleep(time.Duration(rand.Int63n(3000)) * time.Millisecond)
			j := perm[i]
			addr := p2p.NewNetAddressString(seeds[j])
			n.dialSeed(addr)
		}(i)
	}
}

func (n *Node) dialSeed(addr *p2p.NetAddress) {
	peer, err := n.sw.DialPeerWithAddress(addr)
	if err != nil {
		log.Error("Error dialing seed", "error", err)
		//n.book.MarkAttempt(addr)
		return
	} else {
		log.Notice("Connected to seed", "peer", peer)
		n.book.AddAddress(addr, addr)
	}
}

func (n *Node) StartRPC() (net.Listener, error) {
	core.SetBlockStore(n.blockStore)
	core.SetConsensusState(n.consensusState)
	core.SetConsensusReactor(n.consensusReactor)
	core.SetMempoolReactor(n.mempoolReactor)
	core.SetSwitch(n.sw)
	core.SetPrivValidator(n.privValidator)
	core.SetGenesisDoc(n.genesisDoc)

	listenAddr := config.GetString("rpc_laddr")

	mux := http.NewServeMux()
	wm := rpcserver.NewWebsocketManager(core.Routes, n.evsw)
	mux.HandleFunc("/websocket", wm.WebsocketHandler)
	rpcserver.RegisterRPCFuncs(mux, core.Routes)
	return rpcserver.StartHTTPServer(listenAddr, mux)
}

func (n *Node) Switch() *p2p.Switch {
	return n.sw
}

func (n *Node) BlockStore() *bc.BlockStore {
	return n.blockStore
}

func (n *Node) ConsensusState() *consensus.ConsensusState {
	return n.consensusState
}

func (n *Node) MempoolReactor() *mempl.MempoolReactor {
	return n.mempoolReactor
}

func (n *Node) EventSwitch() *events.EventSwitch {
	return n.evsw
}

func makeNodeInfo(sw *p2p.Switch, privKey crypto.PrivKeyEd25519) *p2p.NodeInfo {

	nodeInfo := &p2p.NodeInfo{
		PubKey:  privKey.PubKey().(crypto.PubKeyEd25519),
		Moniker: config.GetString("moniker"),
		Network: config.GetString("chain_id"),
		Version: Version,
		Other: []string{
			Fmt("p2p_version=%v", p2p.Version),
			Fmt("rpc_version=%v", rpc.Version),
			Fmt("wire_version=%v", wire.Version),
		},
	}

	// include git hash in the nodeInfo if available
	if rev, err := ReadFile(config.GetString("revision_file")); err == nil {
		nodeInfo.Other = append(nodeInfo.Other, Fmt("revision=%v", string(rev)))
	}

	if !sw.IsListening() {
		return nodeInfo
	}

	p2pListener := sw.Listeners()[0]
	p2pHost := p2pListener.ExternalAddress().IP.String()
	p2pPort := p2pListener.ExternalAddress().Port
	rpcListenAddr := config.GetString("rpc_laddr")

	// We assume that the rpcListener has the same ExternalAddress.
	// This is probably true because both P2P and RPC listeners use UPnP,
	// except of course if the rpc is only bound to localhost
	nodeInfo.ListenAddr = Fmt("%v:%v", p2pHost, p2pPort)
	nodeInfo.Other = append(nodeInfo.Other, Fmt("rpc_addr=%v", rpcListenAddr))
	return nodeInfo
}

//------------------------------------------------------------------------------

func RunNode() {

	// Wait until the genesis doc becomes available
	genDocFile := config.GetString("genesis_file")
	if !FileExists(genDocFile) {
		log.Notice(Fmt("Waiting for genesis file %v...", genDocFile))
		for {
			time.Sleep(time.Second)
			if !FileExists(genDocFile) {
				continue
			}
			jsonBlob, err := ioutil.ReadFile(genDocFile)
			if err != nil {
				Exit(Fmt("Couldn't read GenesisDoc file: %v", err))
			}
			genDoc := types.GenesisDocFromJSON(jsonBlob)
			if genDoc.ChainID == "" {
				PanicSanity(Fmt("Genesis doc %v must include non-empty chain_id", genDocFile))
			}
			config.Set("chain_id", genDoc.ChainID)
			config.Set("genesis_doc", genDoc)
		}
	}

	// Create & start node
	n := NewNode()
	l := p2p.NewDefaultListener("tcp", config.GetString("node_laddr"), config.GetBool("skip_upnp"))
	n.AddListener(l)
	err := n.Start()
	if err != nil {
		Exit(Fmt("Failed to start node: %v", err))
	}

	log.Notice("Started node", "nodeInfo", n.sw.NodeInfo())

	// If seedNode is provided by config, dial out.
	if config.GetString("seeds") != "" {
		n.DialSeed()
	}

	// Run the RPC server.
	if config.GetString("rpc_laddr") != "" {
		_, err := n.StartRPC()
		if err != nil {
			PanicCrisis(err)
		}
	}

	// Sleep forever and then...
	TrapSignal(func() {
		n.Stop()
	})
}

// Load the most recent state from "state" db,
// or create a new one (and save) from genesis.
func getState() *sm.State {
	stateDB := dbm.GetDB("state")
	state := sm.LoadState(stateDB)
	if state == nil {
		state = sm.MakeGenesisStateFromFile(stateDB, config.GetString("genesis_file"))
		state.Save()
	}
	return state
}

// Get a connection to the proxyAppCtx addr.
// Check the current hash, and panic if it doesn't match.
func getProxyApp(addr string, hash []byte) proxy.AppContext {
	proxyConn, err := Connect(addr)
	if err != nil {
		Exit(Fmt("Failed to connect to proxy for mempool: %v", err))
	}
	proxyAppCtx := proxy.NewRemoteAppContext(proxyConn, 1024)

	proxyAppCtx.Start()

	// Check the hash
	currentHash, err := proxyAppCtx.GetHashSync()
	if err != nil {
		PanicCrisis(Fmt("Error in getting proxyAppCtx hash: %v", err))
	}
	if !bytes.Equal(hash, currentHash) {
		PanicCrisis(Fmt("ProxyApp hash does not match.  Expected %X, got %X", hash, currentHash))
	}

	return proxyAppCtx
}

//-------------------------------------------------

func logEventCallback(dbHost, dbName, nodeID string) func(data types.EventData) {
	return func(data types.EventData) {
		dataCM, ok := data.(*types.EventDataConsensusMessage)
		if !ok {
			return
		}

		dbPoint, err := influxDBPoint(dataCM, nodeID)
		if err != nil {
			log.Error("Couldn't get influxDBPoint", "error", err)
		}
		logEvent(dbHost, dbName, dbPoint)
	}
}

func influxDBPoint(dataCM *types.EventDataConsensusMessage, nodeID string) (point string, err error) {
	switch msg := dataCM.Message.(type) {
	case *consensus.ProposalMessage:
		point = fmt.Sprintf("event,type=proposal,node=%s,parts_hash=%x,signature=%x height=%d,round=%d,pol_round=%d,parts_total=%d", nodeID, msg.Proposal.BlockPartsHeader.Hash, msg.Proposal.Signature[:], msg.Proposal.Height, msg.Proposal.Round, msg.Proposal.POLRound, msg.Proposal.BlockPartsHeader.Total)
	case *consensus.BlockPartMessage:
		proof := wire.JSONBytes(msg.Part.Proof)
		point = fmt.Sprintf("event,type=block_part,node=%s,proof=%x,bytes=%x height=%d,round=%d", nodeID, proof, msg.Part.Bytes, msg.Height, msg.Round)
	case *consensus.VoteMessage:
		point = fmt.Sprintf("event,type=vote,node=%s,val_index=%d,hash=%x,parts_hash=%x,signature=%x height=%d,round=%d,vote_type=%d,parts_total=%d", nodeID, msg.ValidatorIndex, msg.Vote.BlockHash, msg.Vote.BlockPartsHeader.Hash, msg.Vote.Signature[:], msg.Vote.Height, msg.Vote.Round, msg.Vote.Type, msg.Vote.BlockPartsHeader.Total)
	default:
		err = fmt.Errorf("Unknown message type")

	}
	return
}

func logEvent(dbHost, dbName, dbPoint string) {
	buf := new(bytes.Buffer)
	buf.WriteString(dbPoint)
	resp, err := http.Post(fmt.Sprintf("http://%s/write?db=%s", dbHost, dbName), "", buf)
	if err != nil {
		log.Error("Error logging event to influxDB", "error", err)
	}

	if resp.StatusCode != 204 {
		if resp.StatusCode == 400 {
			log.Error("Bad syntax writing point to influxDB", "string", dbPoint)
		} else {
			log.Error("Writing to influxDB was unsuccessful", "status_code", resp.StatusCode)
		}
	}
}

func ConvertReplayFileToMessages(replayFile string, msgChan chan consensus.ConsensusMessage) {
	f, err := os.Open(replayFile)
	if err != nil {
		Exit(err.Error())
	}

	br := bufio.NewReader(f)
	line, err := br.ReadString('\n')
	if err != nil {
		Exit(err.Error())
	}
	fmt.Println(line)

	var lastTime int64
	for {
		line, err := br.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			Exit(err.Error())
		}

		line = line[:len(line)-1]

		spl := strings.Split(line, ",")

		newTime, err := strconv.ParseInt(spl[timeIndex], 10, 64)
		ifExit(err)

		typ := spl[typeIndex]
		var msg consensus.ConsensusMessage
		switch typ {
		case "vote":
			height, err := strconv.ParseInt(spl[heightIndex], 10, 64)
			ifExit(err)
			round, err := strconv.ParseInt(spl[roundIndex], 10, 64)
			ifExit(err)
			typ, err := strconv.ParseInt(spl[voteTypeIndex], 10, 64)
			ifExit(err)
			blockHash, err := hex.DecodeString(spl[hashIndex])
			ifExit(err)
			partsTotal, err := strconv.ParseInt(spl[partsTotalIndex], 10, 64)
			ifExit(err)
			partsHash, err := hex.DecodeString(spl[partsHashIndex])
			ifExit(err)
			sigSlice, err := hex.DecodeString(spl[signatureIndex])
			ifExit(err)
			valIndex, err := strconv.ParseInt(spl[valIndexIndex], 10, 64)
			ifExit(err)

			var sig crypto.SignatureEd25519
			copy(sig[:], sigSlice)

			vote := &types.Vote{
				Height:           int(height),
				Round:            int(round),
				Type:             byte(typ),
				BlockHash:        blockHash,
				BlockPartsHeader: types.PartSetHeader{int(partsTotal), partsHash},
				Signature:        sig,
			}
			msg = &consensus.VoteMessage{int(valIndex), vote}

		case "block_part":
			height, err := strconv.ParseInt(spl[heightIndex], 10, 64)
			ifExit(err)
			round, err := strconv.ParseInt(spl[roundIndex], 10, 64)
			ifExit(err)
			proofBytes, err := hex.DecodeString(spl[proofIndex])
			ifExit(err)
			bytesBytes, err := hex.DecodeString(spl[bytesIndex])
			ifExit(err)
			proof := new(merkle.SimpleProof)
			wire.ReadJSON(proof, proofBytes, &err)
			ifExit(err)
			msg = &consensus.BlockPartMessage{
				Height: int(height),
				Round:  int(round),
				Part:   &types.Part{Proof: *proof, Bytes: bytesBytes},
			}

		case "proposal":
			height, err := strconv.ParseInt(spl[heightIndex], 10, 64)
			ifExit(err)
			round, err := strconv.ParseInt(spl[roundIndex], 10, 64)
			ifExit(err)
			polRound, err := strconv.ParseInt(spl[polRoundIndex], 10, 64)
			ifExit(err)
			partsTotal, err := strconv.ParseInt(spl[partsTotalIndex], 10, 64)
			ifExit(err)
			partsHash, err := hex.DecodeString(spl[partsHashIndex])
			ifExit(err)
			sigSlice, err := hex.DecodeString(spl[signatureIndex])
			ifExit(err)

			var sig crypto.SignatureEd25519
			copy(sig[:], sigSlice)

			proposal := &types.Proposal{
				Height:           int(height),
				Round:            int(round),
				BlockPartsHeader: types.PartSetHeader{Total: int(partsTotal), Hash: partsHash},
				POLRound:         int(polRound),
				Signature:        sig,
			}
			msg = &consensus.ProposalMessage{proposal}
		default:
			Exit("Unknown type " + typ)
		}

		if lastTime != 0 {
			time.Sleep(time.Duration(newTime-lastTime) * time.Nanosecond)
		}
		msgChan <- msg
		lastTime = newTime

	}
	close(msgChan)

}

const (
	timeIndex int = iota + 1
	bytesIndex
	hashIndex
	heightIndex
	nodeIndex
	partsHashIndex
	partsTotalIndex
	polRoundIndex
	proofIndex
	roundIndex
	signatureIndex
	typeIndex
	valIndexIndex
	voteTypeIndex
)

func ifExit(err error) {
	if err != nil {
		Exit(err.Error())
	}

}
