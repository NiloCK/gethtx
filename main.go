package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/nilock/gethtx/bindings"
)

func main() {

	client := NewAnvilClient("http://localhost:8888")

	userPK, err := crypto.GenerateKey()
	if err != nil {
		log.Fatal(err)
	}
	userAddress := crypto.PubkeyToAddress(userPK.PublicKey)

	err = client.SetBalance(context.Background(), userAddress, 10*1e18)
	if err != nil {
		log.Fatal(err)
	}

	price, err := client.ethclient.SuggestGasPrice(context.Background())
	if err != nil {
		panic(err)
	}

	id, err := client.ethclient.ChainID(context.Background())
	if err != nil {
		panic(err)
	}

	signer := types.NewEIP155Signer(id)

	signedTx, err := types.SignTx(
		types.NewTx(
			&types.LegacyTx{
				Nonce:    0,
				To:       &common.Address{}, // burn
				Value:    big.NewInt(1e18 / 100),
				Gas:      21000,
				GasPrice: price,
			},
		),
		signer,
		userPK,
	)
	if err != nil {
		panic(err)
	}

	err = client.ethclient.SendTransaction(context.Background(), signedTx)
	if err != nil {
		panic(err)
	}
	lf("tx sent: %s", signedTx.Hash().Hex())

	depositAddress := "0xEE915F299A6d1eFf68c6EA22E5f93cFD551936F3"
	// wait for the RU to be up and running?
	waitForContract(client, depositAddress, "OptimismPortal")

	portal, err := bindings.NewOptimismPortal(common.HexToAddress(depositAddress), client.ethclient)
	if err != nil {
		panic(err)
	}
	fmt.Println("portal created")

	nonce, err := client.ethclient.NonceAt(context.Background(), userAddress, nil)
	if err != nil {
		log.Panicf("err getting nonce: %s", err)
	}
	fmt.Println("User nonce: ", nonce)

	depositTx, err := portal.DepositTransaction(
		&bind.TransactOpts{
			From: userAddress,
			Signer: func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
				signed, err := types.SignTx(tx, signer, userPK)
				if err != nil {
					return nil, err
				}
				return signed, nil
			},
			GasPrice:  price,
			GasLimit:  1e6,
			GasFeeCap: nil,
			GasTipCap: nil,
			Nonce:     big.NewInt(int64(nonce)),
			Value:     big.NewInt(1e18), // 1 eth
			Context:   nil,
			NoSend:    false,
		},
		userAddress,
		big.NewInt(1e18/2), // 0.5 eth
		25_000_000/2,       // from outputs on ruConfig - gasLimit: 25_000_000
		false,
		[]byte{},
	)

	if err != nil {
		lf("deposit tx: %+v", depositTx)
		log.Panicf("error submitting deposit tx: %s", err)
	}

	lf("deposit tx sent: %s", depositTx.Hash().Hex())
	receipt := waitForReceipt(client, depositTx.Hash(), "depositTx")

	str, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		log.Panicf("error marshalling receipt: %s", err)
	}
	lf("deposit receipt:\n\n%s", str)
}

func waitForReceipt(client *AnvilClient, txHash common.Hash, name string) *types.Receipt {
	foundReceipt := false

	for !foundReceipt {
		receipt, err := client.ethclient.TransactionReceipt(context.Background(), txHash)
		if err != nil || receipt == nil {
			time.Sleep(1 * time.Second)
			lf("waiting for receipt [%s] to be mined", name)
		} else {
			lf("found receipt [%s]", name)
			return receipt
		}
	}
	return nil
}

func lf(format string, args ...interface{}) {
	ts := time.Now().Format("[15:04:05.000] ")
	fmt.Printf(ts+format, args...)
	fmt.Println()
}

// blocks until a contract is deployed at the given address. Checks on a 1 second interval.
func waitForContract(client *AnvilClient, contractAddress string, name string) {
	foundContract := false

	for !foundContract {
		codeAt, err := client.ethclient.CodeAt(context.Background(), common.HexToAddress(contractAddress), nil)
		if err != nil || len(codeAt) == 0 {
			time.Sleep(1 * time.Second)
			lf("waiting for contract [%s] to be deployed", name)
		} else {
			lf("found contract [%s]", name)
			foundContract = true
		}
	}
}

type AnvilClient struct {
	client    *rpc.Client
	ethclient *ethclient.Client
}

func NewAnvilClient(url string) *AnvilClient {
	rpc, err := rpc.Dial(url)
	if err != nil {
		log.Fatal(err)
	}

	return &AnvilClient{
		client:    rpc,
		ethclient: ethclient.NewClient(rpc),
	}
}

func (a *AnvilClient) SetBalance(ctx context.Context, account common.Address, balance uint64) error {
	if err := a.client.CallContext(ctx, nil, "anvil_setBalance", account, hexutil.Uint64(balance)); err != nil {
		return fmt.Errorf("%s, %d: %v", account, balance, err)
	}
	return nil
}
