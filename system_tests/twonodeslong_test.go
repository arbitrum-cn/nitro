// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

// race detection makes things slow and miss timeouts
//go:build !race
// +build !race

package arbtest

import (
	"context"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"io/ioutil"
	"math/big"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/offchainlabs/nitro/arbos/l2pricing"
	"github.com/offchainlabs/nitro/das"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/offchainlabs/nitro/arbnode"
)

func testTwoNodesLong(t *testing.T, dasModeStr string) {
	largeLoops := 8
	avgL2MsgsPerLoop := 30
	avgDelayedMessagesPerLoop := 10
	avgTotalL1MessagesPerLoop := 12 // including delayed
	avgExtraBlocksPerLoop := 20
	finalPropagateLoops := 15

	fundsPerDelayed := big.NewInt(int64(100000000000000001))
	fundsPerDirect := big.NewInt(int64(200003))
	directTransfers := int64(0)
	delayedTransfers := int64(0)

	delayedTxs := make([]*types.Transaction, 0, largeLoops*avgDelayedMessagesPerLoop*2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1NodeConfigA := arbnode.ConfigDefaultL1Test()
	l1NodeConfigA.DataAvailability.ModeImpl = dasModeStr
	chainConfig := params.ArbitrumDevTestChainConfig()
	var dbPath string
	var err error
	if dasModeStr == das.LocalDiskDataAvailabilityString {
		dbPath, err = ioutil.TempDir("/tmp", "das_test")
		Require(t, err)
		defer os.RemoveAll(dbPath)
		chainConfig = params.ArbitrumDevTestDASChainConfig()
		dasConfig := das.LocalDiskDASConfig{
			KeyDir:            dbPath,
			DataDir:           dbPath,
			AllowGenerateKeys: true,
		}
		l1NodeConfigA.DataAvailability.LocalDiskDASConfig = dasConfig
	}
	l2info, nodeA, l2client, l1info, l1backend, l1client, l1stack := CreateTestNodeOnL1WithConfig(t, ctx, true, l1NodeConfigA, chainConfig)
	defer l1stack.Close()

	usingDas := nodeA.DataAvailService

	if usingDas != nil {
		keysetBytes, err := usingDas.CurrentKeysetBytes(ctx)
		Require(t, err)

		sequencerInbox, err := bridgegen.NewSequencerInbox(l1info.Accounts["SequencerInbox"].Address, l1client)
		Require(t, err)
		trOps := l1info.GetDefaultTransactOpts("RollupOwner", ctx)
		tx, err := sequencerInbox.SetValidKeyset(&trOps, keysetBytes)
		Require(t, err)
		_, err = EnsureTxSucceeded(ctx, l1client, tx)
		Require(t, err)
	}

	l1NodeConfigB := arbnode.ConfigDefaultL1Test()
	l1NodeConfigB.BatchPoster.Enable = false
	l1NodeConfigB.BlockValidator.Enable = false
	l1NodeConfigB.DataAvailability.ModeImpl = dasModeStr
	dasConfig := das.LocalDiskDASConfig{
		KeyDir:            dbPath,
		DataDir:           dbPath,
		AllowGenerateKeys: true,
	}
	l1NodeConfigB.DataAvailability.LocalDiskDASConfig = dasConfig
	l2clientB, nodeB := Create2ndNodeWithConfig(t, ctx, nodeA, l1stack, &l2info.ArbInitData, l1NodeConfigB)

	l2info.GenerateAccount("DelayedFaucet")
	l2info.GenerateAccount("DelayedReceiver")
	l2info.GenerateAccount("DirectReceiver")

	l2info.GenerateAccount("ErrorTxSender")

	SendWaitTestTransactions(t, ctx, l2client, []*types.Transaction{
		l2info.PrepareTx("Faucet", "ErrorTxSender", l2info.TransferGas, big.NewInt(l2pricing.InitialBaseFeeWei*int64(l2info.TransferGas)), nil),
	})

	delayedMsgsToSendMax := big.NewInt(int64(largeLoops * avgDelayedMessagesPerLoop * 10))
	delayedFaucetNeeds := new(big.Int).Mul(new(big.Int).Add(fundsPerDelayed, new(big.Int).SetUint64(l2pricing.InitialBaseFeeWei*100000)), delayedMsgsToSendMax)
	SendWaitTestTransactions(t, ctx, l2client, []*types.Transaction{
		l2info.PrepareTx("Faucet", "DelayedFaucet", l2info.TransferGas, delayedFaucetNeeds, nil),
	})
	delayedFaucetBalance, err := l2client.BalanceAt(ctx, l2info.GetAddress("DelayedFaucet"), nil)
	Require(t, err)

	if delayedFaucetBalance.Cmp(delayedFaucetNeeds) != 0 {
		t.Fatalf("Unexpected balance, has %v, expects %v", delayedFaucetBalance, delayedFaucetNeeds)
	}
	t.Logf("DelayedFaucet has %v, per delayd: %v, baseprice: %v", delayedFaucetBalance, fundsPerDelayed, l2pricing.InitialBaseFeeWei)

	if avgTotalL1MessagesPerLoop < avgDelayedMessagesPerLoop {
		Fail(t, "bad params, avgTotalL1MessagesPerLoop should include avgDelayedMessagesPerLoop")
	}
	for i := 0; i < largeLoops; i++ {
		l1TxsThisTime := rand.Int() % (avgTotalL1MessagesPerLoop * 2)
		l1Txs := make([]*types.Transaction, 0, l1TxsThisTime)
		for len(l1Txs) < l1TxsThisTime {
			randNum := rand.Int() % avgTotalL1MessagesPerLoop
			var l1tx *types.Transaction
			if randNum < avgDelayedMessagesPerLoop {
				delayedTx := l2info.PrepareTx("DelayedFaucet", "DelayedReceiver", 30001, fundsPerDelayed, nil)
				l1tx = WrapL2ForDelayed(t, delayedTx, l1info, "User", 100000)
				delayedTxs = append(delayedTxs, delayedTx)
				delayedTransfers++
			} else {
				l1tx = l1info.PrepareTx("Faucet", "User", 30000, big.NewInt(1e12), nil)
			}
			l1Txs = append(l1Txs, l1tx)
		}
		// adding multiple messages in the same AddLocal to get them in the same L1 block
		errs := l1backend.TxPool().AddLocals(l1Txs)
		for _, err := range errs {
			if err != nil {
				Fail(t, err)
			}
		}
		l2TxsThisTime := rand.Int() % (avgL2MsgsPerLoop * 2)
		l2Txs := make([]*types.Transaction, 0, l2TxsThisTime)
		for len(l2Txs) < l2TxsThisTime {
			l2Txs = append(l2Txs, l2info.PrepareTx("Faucet", "DirectReceiver", l2info.TransferGas, fundsPerDirect, nil))
		}
		SendWaitTestTransactions(t, ctx, l2client, l2Txs)
		directTransfers += int64(l2TxsThisTime)
		if len(l1Txs) > 0 {
			_, err := EnsureTxSucceeded(ctx, l1client, l1Txs[len(l1Txs)-1])
			if err != nil {
				Fail(t, err)
			}
		}
		// create bad tx on delayed inbox
		l2info.GetInfoWithPrivKey("ErrorTxSender").Nonce = 10
		SendWaitTestTransactions(t, ctx, l1client, []*types.Transaction{
			WrapL2ForDelayed(t, l2info.PrepareTx("ErrorTxSender", "DelayedReceiver", 30002, delayedFaucetNeeds, nil), l1info, "User", 100000),
		})

		extrBlocksThisTime := rand.Int() % (avgExtraBlocksPerLoop * 2)
		for i := 0; i < extrBlocksThisTime; i++ {
			SendWaitTestTransactions(t, ctx, l1client, []*types.Transaction{
				l1info.PrepareTx("Faucet", "User", 30000, big.NewInt(1e12), nil),
			})
		}
	}

	t.Log("Done sending", delayedTransfers, "delayed transfers", directTransfers, "direct transfers")
	if (delayedTransfers + directTransfers) == 0 {
		Fail(t, "No transfers sent!")
	}

	// sending l1 messages creates l1 blocks.. make enough to get that delayed inbox message in
	for i := 0; i < finalPropagateLoops; i++ {
		var tx *types.Transaction
		for j := 0; j < 30; j++ {
			tx = l1info.PrepareTx("Faucet", "User", 30000, big.NewInt(1e12), nil)
			err := l1client.SendTransaction(ctx, tx)
			if err != nil {
				Fail(t, err)
			}
		}
		_, err := EnsureTxSucceeded(ctx, l1client, tx)
		if err != nil {
			Fail(t, err)
		}
	}

	_, err = EnsureTxSucceededWithTimeout(ctx, l2client, delayedTxs[len(delayedTxs)-1], time.Second*10)
	Require(t, err, "Failed waiting for Tx on main node")
	_, err = EnsureTxSucceededWithTimeout(ctx, l2clientB, delayedTxs[len(delayedTxs)-1], time.Second*10)
	Require(t, err, "Failed waiting for Tx on secondary node")
	delayedBalance, err := l2clientB.BalanceAt(ctx, l2info.GetAddress("DelayedReceiver"), nil)
	Require(t, err)
	directBalance, err := l2clientB.BalanceAt(ctx, l2info.GetAddress("DirectReceiver"), nil)
	Require(t, err)
	delayedExpectd := new(big.Int).Mul(fundsPerDelayed, big.NewInt(delayedTransfers))
	directExpectd := new(big.Int).Mul(fundsPerDirect, big.NewInt(directTransfers))
	if (delayedBalance.Cmp(delayedExpectd) != 0) || (directBalance.Cmp(directExpectd) != 0) {
		t.Error("delayed balance", delayedBalance, "expected", delayedExpectd, "transfers", delayedTransfers)
		t.Error("direct balance", directBalance, "expected", directExpectd, "transfers", directTransfers)
		ownerBalance, _ := l2clientB.BalanceAt(ctx, l2info.GetAddress("Owner"), nil)
		delayedFaucetBalance, _ := l2clientB.BalanceAt(ctx, l2info.GetAddress("DelayedFaucet"), nil)
		t.Error("owner balance", ownerBalance, "delayed faucet", delayedFaucetBalance)
		Fail(t, "Unexpected balance")
	}

	nodeA.StopAndWait()

	if nodeB.BlockValidator != nil {
		lastBlockHeader, err := l2clientB.HeaderByNumber(ctx, nil)
		Require(t, err)
		testDeadLine, deadlineExist := t.Deadline()
		var timeout time.Duration
		if deadlineExist {
			timeout = time.Until(testDeadLine) - (time.Second * 10)
		} else {
			timeout = time.Hour * 12
		}
		if !nodeB.BlockValidator.WaitForBlock(lastBlockHeader.Number.Uint64(), timeout) {
			Fail(t, "did not validate all blocks")
		}
	}

	nodeB.StopAndWait()
}

func TestTwoNodesLong(t *testing.T) {
	testTwoNodesLong(t, das.OnchainDataAvailabilityString)
}

func TestTwoNodesLongLocalDAS(t *testing.T) {
	testTwoNodesLong(t, das.LocalDiskDataAvailabilityString)
}
