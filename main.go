package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/gorilla/mux"
	logger "github.com/inconshreveable/log15"
	"github.com/spf13/cast"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/ybbus/jsonrpc/v3"
	"gopkg.in/yaml.v3"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	gorillaRPC "github.com/gorilla/rpc"
	gorillaJSON "github.com/gorilla/rpc/json"
)

func StoreStateDiff(db *leveldb.DB, client jsonrpc.RPCClient, backupClients []jsonrpc.RPCClient, transactionsStartFrom int, transactions types.Transactions, contractAddress common.Address) {
	for i, transaction := range transactions {
		if len(transaction.Data()) >= 4 {
			go func(client jsonrpc.RPCClient, txHash common.Hash, timestamp int64, transactionNumInBlock int) {
				resData, err := client.Call(context.Background(), "trace_replayTransaction", txHash, []string{"stateDiff"})
				if err != nil {
					logger.Error("MainChild Client Failed To Sync", "MainChildClient", client, "Error", err)
					for _, backupClient := range backupClients {
						resData, err = backupClient.Call(context.Background(), "trace_replayTransaction", txHash, []string{"stateDiff"})
						if err == nil {
							logger.Info("BackupChild Client Successfully Handled Request", "BackUpClient", backupClient)
							break
						}
						logger.Error("BackupChild Clients Failed To Sync", "Error", err)
					}
					if err != nil {
						logger.Error("Failed To Sync State Diff", "TxHash", txHash)
						return
					}
				}
				parsedResData := cast.ToStringMap(resData.Result)
				unparsedStateDiff := cast.ToStringMap(parsedResData["stateDiff"])
				if _, ok := unparsedStateDiff[hexutil.Encode(contractAddress.Bytes())]; ok {
					unparsedStateDiff = cast.ToStringMap(unparsedStateDiff[hexutil.Encode(contractAddress.Bytes())])
					unparsedStateDiff = cast.ToStringMap(unparsedStateDiff["storage"])
					if len(unparsedStateDiff) > 0 {
						for slotNum, unparsedSlotData := range unparsedStateDiff {
							data := cast.ToStringMap(cast.ToStringMap(unparsedSlotData)["*"])["to"]
							hasKey, err := db.Has(common.FromHex(slotNum), nil)
							if err != nil {
								logger.Error("Failed To Check Whether Key Exists", "Error", err)
								return
							}
							if !hasKey {
								slotDataBytes, err := json.Marshal([]SlotData{{Data: common.HexToHash(cast.ToString(data)), TimeStamp: timestamp, TransactionNumInBlock: transactionNumInBlock}})
								if err != nil {
									logger.Error("Failed To Marshal Slot Data", "Error", err)
									return
								}
								if err := db.Put(common.FromHex(slotNum), slotDataBytes, nil); err != nil {
									logger.Error("Failed To Store SlotData", "Error", err)
									return
								}
								logger.Info("SlotData Stored", "SlotNum", slotNum, "Data", data)
								return
							}
							resData, err := db.Get(common.FromHex(slotNum), nil)
							if err != nil {
								logger.Error("Failed To Get Slot Data", "SlotNum", slotNum, "Error", err)
								return
							}
							slotData := []SlotData{}
							if err := json.Unmarshal(resData, &slotData); err != nil {
								logger.Error("Failed To Unmarshal Slot Data", "Error", err)
								return
							}
							slotData = append(slotData, SlotData{Data: common.HexToHash(cast.ToString(data)), TimeStamp: timestamp, TransactionNumInBlock: transactionNumInBlock})
							sort.Sort(SlotDataList(slotData))
							slotDataBytes, err := json.Marshal(slotData)
							if err != nil {
								logger.Error("Failed To Marshal Slot Data", "Error", err)
								return
							}
							if err := db.Put(common.FromHex(slotNum), slotDataBytes, nil); err != nil {
								logger.Error("Failed To Store SlotData", "Error", err)
								return
							}
							logger.Info("SlotData Stored", "SlotNum", slotNum, "Data", data)
						}
					}
				}
			}(client, transaction.Hash(), transaction.Time().Unix(), transactionsStartFrom+i)
		}
	}
}

func ParseBlockDataAndStoreStateDiff(db *leveldb.DB, client *ethclient.Client, childClients []jsonrpc.RPCClient, blockNumber *big.Int, contractAddress common.Address) error {
	blockData, err := client.BlockByNumber(context.Background(), blockNumber)
	if err != nil {
		logger.Error("Failed To Get Block Data", "Error", err)
		return fmt.Errorf("failed to get block data: %v", err)
	}
	transactions := blockData.Transactions()
	totalTransactions := len(transactions)
	totalChildClients := len(childClients)
	logger.Info("", "TotalTransactions", totalTransactions, "BlockNumber", blockNumber)
	if totalTransactions < totalChildClients {
		for i := range transactions {
			go StoreStateDiff(db, childClients[i], childClients, i, transactions[i:i+1], contractAddress)
		}
	} else {
		transactionsForEachChildClient := totalTransactions / totalChildClients
		totalTransactionsAllocated := 0
		for i, childClient := range childClients {
			if i == totalChildClients-1 {
				go StoreStateDiff(db, childClient, childClients, totalTransactionsAllocated, transactions[totalTransactionsAllocated:totalTransactions], contractAddress)
			} else {
				go StoreStateDiff(db, childClient, childClients, totalTransactionsAllocated, transactions[totalTransactionsAllocated:totalTransactionsAllocated+transactionsForEachChildClient], contractAddress)
				totalTransactionsAllocated += transactionsForEachChildClient
			}
		}
	}
	return nil
}

func startRPCServer(syncContract *SyncContract, port int) {
	router := mux.NewRouter()
	router.HandleFunc("/", func(res http.ResponseWriter, req *http.Request) {
		res.Write([]byte("SyncContract RPC Server Is Running"))
		res.WriteHeader(http.StatusOK)
	})
	rpcServer := gorillaRPC.NewServer()
	rpcServer.RegisterCodec(gorillaJSON.NewCodec(), "application/json")
	rpcServer.RegisterService(syncContract, "")
	router.Handle("/syncContractServer", rpcServer)
	rpcPort := fmt.Sprintf("%v", port)
	logger.Info("SyncContract RPC Server Is Running", "Port", rpcPort)
	if err := http.ListenAndServe(":"+rpcPort, router); err != nil {
		log.Panic("Something Went Wrong: ", err)
	}
}

func setupClients(configData *Config) (*ethclient.Client, []jsonrpc.RPCClient) {
	masterClient, err := ethclient.Dial(configData.MasterClientWSS)
	if err != nil {
		log.Panic("Failed To Connect To ETH Client:", err)
	}
	childClients := []jsonrpc.RPCClient{}
	for _, rpcURL := range configData.ChildClientsRPC {
		client := jsonrpc.NewClient(rpcURL)
		_, err := client.Call(context.Background(), "eth_chainId")
		if err != nil {
			logger.Error("Failed To Connect To Client", "Error", err)
			log.Panic("Failed To Connect To Client:", err)
		}
		childClients = append(childClients, client)
	}
	return masterClient, childClients
}

func main() {
	// fmt.Println([]int{1, 2, 3, 4}[1:])
	// select {}
	configDataBytes, err := os.ReadFile("./config.yaml")
	if err != nil {
		log.Panic("Failed To Read Config File: ", err)
	}
	configData := &Config{}
	if err = yaml.Unmarshal(configDataBytes, configData); err != nil {
		log.Panic("Failed To Unmarshal Config Data: ", err)
	}
	if !common.IsHexAddress(configData.ContractToSync) {
		logger.Error("Invalid Contract Address", "ContractAddress", configData.ContractToSync)
		log.Panic("Invalid Contract Address")
	}
	masterClient, childClients := setupClients(configData)
	db, err := leveldb.OpenFile("syncContractData", nil)
	if err != nil {
		logger.Error("Failed To Connect To DB", "Error", err)
	}
	logger.Info("Connected To LevelDB")
	defer db.Close()
	syncContract := new(SyncContract)
	syncContract.db = db
	syncContract.masterClient = masterClient
	syncContract.childClients = childClients
	syncContract.ContractToSync = common.HexToAddress(configData.ContractToSync)
	syncContract.MasterClientEndpointURL = configData.MasterClientWSS
	syncContract.ChildClientsEndpointURL = configData.ChildClientsRPC
	go startRPCServer(syncContract, configData.Port)
	isBlockProcessed := map[*big.Int]struct{}{}
	blockDataChan := make(chan *types.Header)
	subscribe, err := masterClient.SubscribeNewHead(context.Background(), blockDataChan)
	if err != nil {
		logger.Error("Failed To Connect To ETH Client", "Error", err)
		log.Panic("Failed To Subscribe Contract Transactions:", err)
	}
	logger.Info("Syncing Contract State Changes")
	for {
		select {
		case err := <-subscribe.Err():
			logger.Error("Something Went Wrong In Transaction Subscription", "Error", err)
			log.Panic("Something Went Wrong In Transaction Subscription:", err)
		case subsData := <-blockDataChan:
			if _, ok := isBlockProcessed[subsData.Number]; !ok {
				logger.Info("Block Generated On ETH", "BlockNumber", subsData.Number)
				go ParseBlockDataAndStoreStateDiff(db, masterClient, syncContract.childClients, subsData.Number, syncContract.ContractToSync)
				isBlockProcessed[subsData.Number] = struct{}{}
			}
		}
	}
}
