package node

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Fantom-foundation/go-lachesis/src/common"
	"github.com/Fantom-foundation/go-lachesis/src/crypto"
	"github.com/Fantom-foundation/go-lachesis/src/dummy"
	"github.com/Fantom-foundation/go-lachesis/src/peers"
	"github.com/Fantom-foundation/go-lachesis/src/poset"
	"github.com/Fantom-foundation/go-lachesis/src/utils"
	"github.com/sirupsen/logrus"
)

func initPeers(n int, t testing.TB) ([]*ecdsa.PrivateKey, []string, *peers.Peers) {
	var keys []*ecdsa.PrivateKey
	addresses := utils.GetUnusedNetAddr(n, t)
	ps := peers.NewPeers()

	for i := 0; i < n; i++ {
		key, _ := crypto.GenerateECDSAKey()
		keys = append(keys, key)

		ps.AddPeer(peers.NewPeer(
			fmt.Sprintf("0x%X", crypto.FromECDSAPub(&keys[i].PublicKey)),
			addresses[i],
		))
	}

	return keys, addresses, ps
}

func TestProcessSync(t *testing.T) {
	keys, addresses, p := initPeers(2, t)
	testLogger := common.NewTestLogger(t)
	config := TestConfig(t)

	// Start two nodes

	ps := p.ToPeerSlice()

	peer0Trans, err := net.NewTCPTransport(addresses[0], nil, 2,
		time.Second, testLogger)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer peer0Trans.Close()

	node0 := NewNode(config, ps[0].ID, keys[0], p,
		poset.NewInmemStore(p, config.CacheSize, nil),
		peer0Trans,
		dummy.NewInmemDummyApp(testLogger))
	if err := node0.Init(); err != nil {
		t.Fatal(err)
	}

	node0.RunAsync(false)
	defer node0.Shutdown()

	peer1Trans, err := net.NewTCPTransport(addresses[1], nil, 2,
		time.Second, testLogger)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer peer1Trans.Close()

	node1 := NewNode(config, ps[1].ID, keys[1], p,
		poset.NewInmemStore(p, config.CacheSize, nil),
		peer1Trans,
		dummy.NewInmemDummyApp(testLogger))
	if err := node1.Init(); err != nil {
		t.Fatal(err)
	}

	node1.RunAsync(false)
	defer node1.Shutdown()

	// Manually prepare SyncRequest and expected SyncResponse

	node0KnownEvents := node0.core.KnownEvents()
	node1KnownEvents := node1.core.KnownEvents()

	unknownEvents, err := node1.core.EventDiff(node0KnownEvents)
	if err != nil {
		t.Fatal(err)
	}

	unknownWireEvents, err := node1.core.ToWire(unknownEvents)
	if err != nil {
		t.Fatal(err)
	}

	args := net.SyncRequest{
		FromID: node0.id,
		Known:  node0KnownEvents,
	}
	expectedResp := net.SyncResponse{
		FromID: node1.id,
		Events: unknownWireEvents,
		Known:  node1KnownEvents,
	}

	// Make actual SyncRequest and check SyncResponse

	testLogger.Println("SYNCING...")
	time.Sleep(2000 * time.Millisecond)
	var out net.SyncResponse
	if err := peer0Trans.Sync(peer1Trans.LocalAddr(), &args, &out); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Verify the response
	if expectedResp.FromID != out.FromID {
		t.Fatalf("SyncResponse.FromID should be %d, not %d",
			expectedResp.FromID, out.FromID)
	}

	if l := len(out.Events); l != len(expectedResp.Events) {
		t.Fatalf("SyncResponse.Events should contain %d items, not %d",
			len(expectedResp.Events), l)
	}

	for i, e := range expectedResp.Events {
		ex := out.Events[i]
		if !reflect.DeepEqual(e.Body, ex.Body) {
			t.Fatalf("SyncResponse.Events[%d] should be %v, not %v",
				i, e.Body, ex.Body)
		}
	}

	if !reflect.DeepEqual(expectedResp.Known, out.Known) {
		t.Fatalf("SyncResponse.KnownEvents should be %#v, not %#v",
			expectedResp.Known, out.Known)
	}

}

func TestProcessEagerSync(t *testing.T) {
	keys, addresses, p := initPeers(2, t)
	testLogger := common.NewTestLogger(t)
	config := TestConfig(t)

	// Start two nodes

	ps := p.ToPeerSlice()

	peer0Trans, err := net.NewTCPTransport(addresses[0], nil, 2,
		time.Second, testLogger)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer peer0Trans.Close()

	node0 := NewNode(config, ps[0].ID, keys[0], p,
		poset.NewInmemStore(p, config.CacheSize, nil),
		peer0Trans,
		dummy.NewInmemDummyApp(testLogger))
	if err := node0.Init(); err != nil {
		t.Fatal(err)
	}

	node0.RunAsync(false)
	defer node0.Shutdown()

	peer1Trans, err := net.NewTCPTransport(addresses[1], nil, 2,
		time.Second, testLogger)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer peer1Trans.Close()

	node1 := NewNode(config, ps[1].ID, keys[1], p,
		poset.NewInmemStore(p, config.CacheSize, nil),
		peer1Trans,
		dummy.NewInmemDummyApp(testLogger))
	if err := node1.Init(); err != nil {
		t.Fatal(err)
	}

	node1.RunAsync(false)
	defer node1.Shutdown()

	// Manually prepare EagerSyncRequest and expected EagerSyncResponse

	node1KnownEvents := node1.core.KnownEvents()

	unknownEvents, err := node0.core.EventDiff(node1KnownEvents)
	if err != nil {
		t.Fatal(err)
	}

	unknownWireEvents, err := node0.core.ToWire(unknownEvents)
	if err != nil {
		t.Fatal(err)
	}

	args := net.EagerSyncRequest{
		FromID: node0.id,
		Events: unknownWireEvents,
	}
	expectedResp := net.EagerSyncResponse{
		FromID:  node1.id,
		Success: true,
	}

	time.Sleep(2000 * time.Millisecond)
	// Make actual EagerSyncRequest and check EagerSyncResponse
	var out net.EagerSyncResponse
	if err := peer0Trans.EagerSync(
		peer1Trans.LocalAddr(), &args, &out); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Verify the response
	if expectedResp.Success != out.Success {
		t.Fatalf("EagerSyncResponse.Sucess should be %v, not %v",
			expectedResp.Success, out.Success)
	}
}

func TestAddTransaction(t *testing.T) {
	keys, addresses, p := initPeers(2, t)
	testLogger := common.NewTestLogger(t)
	config := TestConfig(t)

	// Start two nodes

	ps := p.ToPeerSlice()

	peer0Trans, err := net.NewTCPTransport(addresses[0], nil, 2,
		time.Second, common.NewTestLogger(t))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	peer0Proxy := dummy.NewInmemDummyApp(testLogger)
	defer peer0Trans.Close()

	node0 := NewNode(TestConfig(t), ps[0].ID, keys[0], p,
		poset.NewInmemStore(p, config.CacheSize, nil),
		peer0Trans,
		peer0Proxy)
	if err := node0.Init(); err != nil {
		t.Fatal(err)
	}

	node0.RunAsync(false)
	defer node0.Shutdown()

	peer1Trans, err := net.NewTCPTransport(addresses[1], nil, 2,
		time.Second, common.NewTestLogger(t))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	peer1Proxy := dummy.NewInmemDummyApp(testLogger)
	defer peer1Trans.Close()

	node1 := NewNode(TestConfig(t), ps[1].ID, keys[1], p,
		poset.NewInmemStore(p, config.CacheSize, nil),
		peer1Trans,
		peer1Proxy)
	if err := node1.Init(); err != nil {
		t.Fatal(err)
	}

	node1.RunAsync(false)
	defer node1.Shutdown()
	// Submit a Tx to node0

	time.Sleep(2000 * time.Millisecond)
	message := "Hello World!"
	peer0Proxy.SubmitCh() <- []byte(message)

	// simulate a SyncRequest from node0 to node1

	node0KnownEvents := node0.core.KnownEvents()
	args := net.SyncRequest{
		FromID: node0.id,
		Known:  node0KnownEvents,
	}

	peer1Trans.LocalAddr()
	var out net.SyncResponse
	if err := peer0Trans.Sync(peer1Trans.LocalAddr(), &args, &out); err != nil {
		t.Fatal(err)
	}

	if err := node0.sync([][]poset.WireEvent{out.Events}); err != nil {
		t.Fatal(err)
	}

	// check the Tx was removed from the transactionPool
	// and added to the new Head

	if l := len(node0.core.transactionPool); l > 0 {
		t.Fatalf("node0's transactionPool should have 0 elements, not %d\n", l)
	}

	node0Head, _ := node0.core.GetHead()
	if l := len(node0Head.Transactions()); l != 1 {
		t.Fatalf("node0's Head should have 1 element, not %d\n", l)
	}

	if m := string(node0Head.Transactions()[0]); m != message {
		t.Fatalf("Transaction message should be '%s' not, not %s\n",
			message, m)
	}
}

func initNodes(keys []*ecdsa.PrivateKey,
	addresses []string,
	peers *peers.Peers,
	cacheSize int,
	syncLimit int64,
	peersCount int,
	storeType string,
	logger *logrus.Logger,
	t testing.TB) []*Node {

	var nodes []*Node

	for idx, k := range keys {
		key := fmt.Sprintf("0x%X", crypto.FromECDSAPub(&k.PublicKey))
		peer, ok := peers.ReadByPubKey(key)
		if !ok {
			t.Fatalf("peer %v not found", key)
		}
		id := peer.ID

		conf := NewConfig(
			5*time.Millisecond,
			2*time.Second,
			cacheSize,
			syncLimit,
			peersCount,
			logger,
		)

		trans, err := net.NewTCPTransport(addresses[idx],
			nil, 2, 2*time.Second, logger)
		if err != nil {
			t.Fatalf("failed to create transport for peer %d: %s", id, err)
		}

		peers.Lock()
		peer.NetAddr = trans.LocalAddr()
		peers.Unlock()

		var store poset.Store
		switch storeType {
		case "badger":
			path, _ := ioutil.TempDir("", "badger")
			store, err = poset.NewBadgerStore(peers, conf.CacheSize, path, nil)
			if err != nil {
				t.Fatalf("failed to create BadgerStore for peer %d: %s",
					id, err)
			}
		case "inmem":
			store = poset.NewInmemStore(peers, conf.CacheSize, nil)
		}
		prox := dummy.NewInmemDummyApp(logger)

		node := NewNode(conf,
			id,
			k,
			peers,
			store,
			trans,
			prox)

		if err := node.Init(); err != nil {
			t.Fatalf("failed to initialize node%d: %s", id, err)
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func recycleNodes(
	oldNodes []*Node, logger *logrus.Logger, t *testing.T) []*Node {
	var newNodes []*Node
	for _, oldNode := range oldNodes {
		newNode := recycleNode(oldNode, logger, t)
		newNodes = append(newNodes, newNode)
	}
	return newNodes
}

func recycleNode(oldNode *Node, logger *logrus.Logger, t *testing.T) *Node {
	conf := oldNode.conf
	id := oldNode.id
	key := oldNode.core.key
	ps := oldNode.peerSelector.Peers()

	var store poset.Store
	var err error
	if _, ok := oldNode.core.poset.Store.(*poset.BadgerStore); ok {
		store, err = poset.LoadBadgerStore(
			conf.CacheSize, oldNode.core.poset.Store.StorePath())
		if err != nil {
			t.Fatal(err)
		}
	} else {
		store = poset.NewInmemStore(oldNode.core.participants, conf.CacheSize, nil)
	}

	trans, err := net.NewTCPTransport(oldNode.localAddr,
		nil, 2, time.Second, logger)
	if err != nil {
		t.Fatal(err)
	}
	prox := dummy.NewInmemDummyApp(logger)

	newNode := NewNode(conf, id, key, ps, store, trans, prox)

	if err := newNode.Init(); err != nil {
		t.Fatal(err)
	}

	return newNode
}

func runNodes(nodes []*Node, gossip bool) {
	for _, n := range nodes {
		node := n
		go func() {
			node.Run(gossip)
		}()
	}
}

func shutdownNodes(nodes []*Node) {
	for _, n := range nodes {
		n.Shutdown()
	}
}

func TestGossip(t *testing.T) {

	logger := common.NewTestLogger(t)

	keys, addresses, ps := initPeers(4, t)
	nodes := initNodes(keys, addresses, ps, 10000, 1000, 2, "inmem", logger, t)

	target := int64(1)

	err := gossip(nodes, target, true, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	srvAddr := utils.GetUnusedNetAddr(1, t)
	s := NewService(srvAddr[0], nodes[0], logger)

	srv := s.Serve()

	t.Logf("serving for 3 seconds")
	shutdownTimeout := 3 * time.Second
	time.Sleep(shutdownTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	t.Logf("stopping after waiting for Serve()...")
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err) // failure/timeout shutting down the server gracefully
	}

	checkGossip(nodes, 0, t)
}

func TestMissingNodeGossip(t *testing.T) {

	logger := common.NewTestLogger(t)

	keys, addresses, ps := initPeers(4, t)
	nodes := initNodes(keys, addresses, ps, 1000, 1000, 1, "inmem", logger, t)
	defer shutdownNodes(nodes)

	err := gossip(nodes[1:], 3, true, 120*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	checkGossip(nodes[1:], 0, t)
}

func TestSyncLimit(t *testing.T) {

	logger := common.NewTestLogger(t)

	keys, addresses, ps := initPeers(4, t)
	nodes := initNodes(keys, addresses, ps, 1000, 1000, 1, "inmem", logger, t)

	err := gossip(nodes, 10, false, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownNodes(nodes)

	// create fake node[0] known to artificially reach SyncLimit
	node0KnownEvents := nodes[0].core.KnownEvents()
	for k := range node0KnownEvents {
		node0KnownEvents[k] = 0
	}

	args := net.SyncRequest{
		FromID: nodes[0].id,
		Known:  node0KnownEvents,
	}
	expectedResp := net.SyncResponse{
		FromID:    nodes[1].id,
		SyncLimit: true,
	}

	var out net.SyncResponse
	if err := nodes[0].trans.Sync(nodes[1].localAddr, &args, &out); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Verify the response
	if expectedResp.FromID != out.FromID {
		t.Fatalf("SyncResponse.FromID should be %d, not %d",
			expectedResp.FromID, out.FromID)
	}
	if !expectedResp.SyncLimit {
		t.Fatal("SyncResponse.SyncLimit should be true")
	}
}

func TestFastForward(t *testing.T) {

	logger := common.NewTestLogger(t)

	keys, addresses, ps := initPeers(4, t)
	nodes := initNodes(keys, addresses, ps, 1000, 1000, 1, "inmem", logger, t)
	defer shutdownNodes(nodes)

	target := int64(3)
	err := gossip(nodes[1:], target, false, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)

	err = nodes[0].fastForward()
	if err != nil {
		t.Fatalf("Error FastForwarding: %s", err)
	}

	lbi := nodes[0].core.GetLastBlockIndex()
	if lbi < 0 {
		t.Fatalf("LastBlockIndex is too low: %d", lbi)
	}
	sBlock, err := nodes[0].GetBlock(lbi)
	if err != nil {
		t.Fatalf("Error retrieving latest Block"+
			" from reset hasposetraph: %v", err)
	}
	expectedBlock, err := nodes[1].GetBlock(lbi)
	if err != nil {
		t.Fatalf("Failed to retrieve block %d from node1: %v", lbi, err)
	}
	if !reflect.DeepEqual(sBlock.Body, expectedBlock.Body) {
		t.Fatalf("Blocks defer")
	}
}

func TestCatchUp(t *testing.T) {
	var let sync.Mutex
	caught := false
	logger := common.NewTestLogger(t)

	// Create  config for 4 nodes
	keys, addresses, ps := initPeers(4, t)

	// Initialize the first 3 nodes only
	normalNodes := initNodes(keys[0:3], addresses[0:3], ps, 1000, 400, 1, "inmem", logger, t)
	defer shutdownNodes(normalNodes)

	target := int64(3)

	err := gossip(normalNodes, target, false, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checkGossip(normalNodes, 0, t)

	node4 := initNodes(keys[3:], addresses[3:], ps, 1000, 40, 1, "inmem", logger, t)[0]

	// Run parallel routine to check node4 eventually reaches CatchingUp state.
	timeout := time.After(30 * time.Second)
	go func() {
		let.Lock()
		defer let.Unlock()
		for {
			select {
			case <-timeout:
				t.Logf("Timeout waiting for node4 to enter CatchingUp state")
				break
			default:
			}
			if node4.getState() == CatchingUp {
				caught = true
				break
			}
		}
	}()

	node4.RunAsync(true)
	defer node4.Shutdown()

	// Gossip some more
	nodes := append(normalNodes, node4)
	newTarget := target + 4
	err = bombardAndWait(nodes, newTarget, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	start := node4.core.poset.FirstConsensusRound
	checkGossip(nodes, *start, t)
	let.Lock()
	let.Unlock()
	if !caught {
		t.Fatalf("Node4 didn't reach CatchingUp state")
	}
}

func TestFastSync(t *testing.T) {
	var let sync.Mutex
	caught := false
	logger := common.NewTestLogger(t)

	// Create  config for 4 nodes
	keys, addresses, ps := initPeers(4, t)
	nodes := initNodes(keys, addresses, ps, 1000, 400, 1, "inmem", logger, t)
	defer shutdownNodes(nodes)

	var target int64 = 10

	err := gossip(nodes, target, false, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checkGossip(nodes, 0, t)

	node4 := nodes[3]
	node4.Shutdown()

	secondTarget := target + 10
	err = bombardAndWait(nodes[0:3], secondTarget, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checkGossip(nodes[0:3], 0, t)

	// Can't re-run it; have to reinstantiate a new node.
	node4 = recycleNode(node4, logger, t)

	// Run parallel routine to check node4 eventually reaches CatchingUp state.
	timeout := time.After(30 * time.Second)
	go func() {
		let.Lock()
		defer let.Unlock()
		for {
			select {
			case <-timeout:
				t.Logf("Timeout waiting for node4 to enter CatchingUp state")
				break
			default:
			}
			if node4.getState() == CatchingUp {
				caught = true
				break
			}
		}
	}()

	node4.RunAsync(true)
	defer node4.Shutdown()

	nodes[3] = node4

	// Gossip some more
	thirdTarget := secondTarget + 10
	err = bombardAndWait(nodes, thirdTarget, 25*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	start := node4.core.poset.FirstConsensusRound
	checkGossip(nodes, *start, t)
	let.Lock()
	let.Unlock()
	if !caught {
		t.Fatalf("Node4 didn't reach CatchingUp state")
	}
}

func TestShutdown(t *testing.T) {
	logger := common.NewTestLogger(t)

	keys, addresses, ps := initPeers(4, t)
	nodes := initNodes(keys, addresses, ps, 1000, 1000, 1, "inmem", logger, t)
	runNodes(nodes, false)

	nodes[0].Shutdown()

	peer := ps.ByPubKey[nodes[0].core.hexID]
	err := nodes[1].gossip([]*peers.Peer{peer}, nil)
	if err == nil {
		t.Fatal("Expected Timeout Error")
	}

	nodes[1].Shutdown()
}

func TestBootstrapAllNodes(t *testing.T) {
	logger := common.NewTestLogger(t)

	if err := os.RemoveAll("test_data"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("test_data", os.ModeDir|0777); err != nil {
		t.Fatal(err)
	}

	// create a first network with BadgerStore
	// and wait till it reaches 10 consensus rounds before shutting it down
	keys, addresses, ps := initPeers(4, t)
	nodes := initNodes(keys, addresses, ps, 1000, 1000, 2, "badger", logger, t)

	err := gossip(nodes, 10, false, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checkGossip(nodes, 0, t)
	shutdownNodes(nodes)

	// Now try to recreate a network from the databases created
	// in the first step and advance it to 20 consensus rounds
	newNodes := recycleNodes(nodes, logger, t)
	err = gossip(newNodes, 20, false, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checkGossip(newNodes, 0, t)
	shutdownNodes(newNodes)

	// Check that both networks did not have
	// completely different consensus events
	checkGossip([]*Node{nodes[0], newNodes[0]}, 0, t)
}

func gossip(
	nodes []*Node, target int64, shutdown bool, timeout time.Duration) error {
	runNodes(nodes, true)
	err := bombardAndWait(nodes, target, timeout)
	if err != nil {
		return err
	}
	if shutdown {
		shutdownNodes(nodes)
	}
	return nil
}

func bombardAndWait(nodes []*Node, target int64, timeout time.Duration) error {

	quit := make(chan struct{})
	makeRandomTransactions(nodes, quit)
	tag := "beginning"

	// wait until all nodes have at least 'target' blocks
	stopper := time.After(timeout)
	for {
		select {
		case <-stopper:
			return fmt.Errorf("timeout in %v", tag)
		default:
		}
		time.Sleep(10 * time.Millisecond)
		done := true
		for _, n := range nodes {
			ce := n.core.GetLastBlockIndex()
			fmt.Printf("%d %d\n", n.id, ce)
			if ce < target {
				done = false
				tag = fmt.Sprintf("ce<target:%v<%v", ce, target)
				break
			} else {
				// wait until the target block has retrieved a state hash from
				// the app
				targetBlock, _ := n.core.poset.Store.GetBlock(target)
				if len(targetBlock.GetStateHash()) == 0 {
					done = false
					tag = "stateHash==0"
					break
				}
			}
		}
		if done {
			break
		}
	}
	close(quit)
	return nil
}

type Service struct {
	bindAddress string
	node        *Node
	graph       *Graph
	logger      *logrus.Logger
}

func NewService(bindAddress string, n *Node, logger *logrus.Logger) *Service {
	service := Service{
		bindAddress: bindAddress,
		node:        n,
		graph:       NewGraph(n),
		logger:      logger,
	}

	return &service
}

func (s *Service) Serve() *http.Server {
	s.logger.WithField("bind_address", s.bindAddress).Debug("Service serving")

	http.HandleFunc("/stats", s.GetStats)

	http.HandleFunc("/block/", s.GetBlock)

	http.HandleFunc("/graph", s.GetGraph)

	srv := &http.Server{Addr: s.bindAddress, Handler: nil}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			s.logger.WithField("error", err).Error("Service failed")
		}
	}()

	return srv
}

func (s *Service) GetStats(w http.ResponseWriter, r *http.Request) {
	stats := s.node.GetStats()

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(stats); err != nil {
		s.logger.WithError(err).Errorf("Failed to encode stats %v", stats)
	}
}

func (s *Service) GetBlock(w http.ResponseWriter, r *http.Request) {
	param := r.URL.Path[len("/block/"):]

	blockIndex, err := strconv.ParseInt(param, 10, 64)

	if err != nil {
		s.logger.WithError(err).Errorf("Parsing block_index parameter %s", param)

		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	block, err := s.node.GetBlock(blockIndex)

	if err != nil {
		s.logger.WithError(err).Errorf("Retrieving block %d", blockIndex)

		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(block); err != nil {
		s.logger.WithError(err).Errorf("Failed to encode block %v", block)
	}
}

func (s *Service) GetGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	encoder := json.NewEncoder(w)

	res := s.graph.GetInfos()

	if err := encoder.Encode(res); err != nil {
		s.logger.WithError(err).Errorf("Failed to encode Infos %v", res)
	}
}

func checkGossip(nodes []*Node, fromBlock int64, t *testing.T) {
	nodeBlocks := map[uint64][]poset.Block{}
	for _, n := range nodes {
		var blocks []poset.Block
		lastIndex := n.core.poset.Store.LastBlockIndex()
		for i := fromBlock; i < lastIndex; i++ {
			block, err := n.core.poset.Store.GetBlock(i)
			if err != nil {
				t.Fatalf("checkGossip: %v ", err)
			}
			blocks = append(blocks, block)
		}
		nodeBlocks[n.id] = blocks
	}

	minB := len(nodeBlocks[0])
	for k := uint64(1); k < uint64(len(nodes)); k++ {
		if len(nodeBlocks[k]) < minB {
			minB = len(nodeBlocks[k])
		}
	}

	for i, block := range nodeBlocks[0][:minB] {
		for k := uint64(1); k < uint64(len(nodes)); k++ {
			oBlock := nodeBlocks[k][i]
			if !reflect.DeepEqual(block.Body, oBlock.Body) {
				t.Fatalf("check gossip: difference in block %d."+
					" node 0: %v, node %d: %v",
					block.Index(), block.Body, k, oBlock.Body)
			}
		}
	}
}

func makeRandomTransactions(nodes []*Node, quit <-chan struct{}) {
	go func() {
		seq := make(map[int]int)
		for {
			select {
			case <-quit:
				return
			default:
				n := rand.Intn(len(nodes))
				node := nodes[n]
				if err := submitTransaction(node, []byte(
					fmt.Sprintf("node%d transaction %d", n, seq[n]))); err != nil {
					panic(err)
				}
				seq[n] = seq[n] + 1
				time.Sleep(3 * time.Millisecond)
			}
		}
	}()
}

func submitTransaction(n *Node, tx []byte) error {
	n.proxy.SubmitCh() <- []byte(tx)
	return nil
}

func BenchmarkGossip(b *testing.B) {
	logger := common.NewTestLogger(b)
	for n := 0; n < b.N; n++ {
		keys, addresses, ps := initPeers(4, b)
		nodes := initNodes(keys, addresses, ps, 1000, 1000, 1, "inmem", logger, b)
		if err := gossip(nodes, 50, true, 3*time.Second); err != nil {
			b.Fatal(err)
		}
	}
}
