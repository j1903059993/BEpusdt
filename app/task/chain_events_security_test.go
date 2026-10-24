package task

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
	"github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/tronprotocol/core"
)

func TestEVMLogIdentityAndNetwork(t *testing.T) {
	timestamp := time.Unix(1700000000, 0)
	log := func(index string) map[string]any {
		return map[string]any{"address": model.GetTradeContract(model.UsdtBep20), "topics": []string{evmTransferEvent, "0x" + strings.Repeat("0", 24) + strings.Repeat("1", 40), "0x" + strings.Repeat("0", 24) + strings.Repeat("2", 40)}, "data": fmt.Sprintf("0x%064x", big.NewInt(1000000000000000000)), "logIndex": index, "transactionHash": "0x" + strings.Repeat("a", 64), "blockNumber": "0xa", "removed": false}
	}
	a, b, missing, removed, wrong := log("0x7"), log("0x8"), log("0x9"), log("0xa"), log("0xb")
	delete(missing, "logIndex")
	removed["removed"] = true
	wrong["address"] = model.GetTradeContract(model.UsdcPolygon)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": []any{wrong, a, missing, b, removed}})
	}))
	defer srv.Close()
	initTaskTestEnv(t, map[model.ConfKey]string{model.RpcEndpointBsc: srv.URL})
	e := evm{Network: conf.Bsc, Client: srv.Client()}
	got, err := e.parseEventTransfer(evmBlock{From: 10, To: 10}, map[string]time.Time{"0xa": timestamp})
	if err != nil || len(got) != 2 {
		t.Fatalf("transfers=%d err=%v", len(got), err)
	}
	if got[0].EventIndex != "log:7" || got[1].EventIndex != "log:8" {
		t.Fatal("event index changed with filtering")
	}
}

func TestReceiptConfirmationRequiresTheClaimedEvent(t *testing.T) {
	hash := "0x" + strings.Repeat("c", 64)
	o := model.Order{TradeType: model.UsdtBep20, RefHash: hash, RefEventIndex: "log:7", RefBlockNum: 10, FromAddress: "0x" + strings.Repeat("1", 40), Address: "0x" + strings.Repeat("2", 40), Amount: "1"}
	event := map[string]any{"address": model.GetTradeContract(o.TradeType), "logIndex": "0x7", "topics": []string{evmTransferEvent, "0x" + strings.Repeat("0", 24) + strings.Repeat("1", 40), "0x" + strings.Repeat("0", 24) + strings.Repeat("2", 40)}, "data": fmt.Sprintf("0x%064x", big.NewInt(1000000000000000000))}
	receipt := map[string]any{"status": "0x1", "transactionHash": hash, "blockNumber": "0xa", "logs": []any{event}}
	check := func() bool { raw, _ := json.Marshal(receipt); return evmReceiptConfirms(o, gjson.ParseBytes(raw)) }
	if !check() {
		t.Fatal("valid event receipt rejected")
	}
	event["logIndex"] = "0x8"
	if check() {
		t.Fatal("different event confirmed claim")
	}
	event["logIndex"] = "0x7"
	receipt["transactionHash"] = "0x" + strings.Repeat("d", 64)
	if check() {
		t.Fatal("different transaction confirmed claim")
	}
}

func TestTRONReceiptEventsUseCanonicalLogPositions(t *testing.T) {
	timestamp := time.UnixMilli(1700000000000)
	id := strings.Repeat("a", 64)
	hash, _ := hex.DecodeString(id)
	topic, _ := hex.DecodeString(strings.TrimPrefix(evmTransferEvent, "0x"))
	addressTopic := func(v byte) []byte {
		b := make([]byte, 32)
		for i := 12; i < 32; i++ {
			b[i] = v
		}
		return b
	}
	event := func() *core.TransactionInfo_Log {
		return &core.TransactionInfo_Log{Address: usdtTrc20ContractAddress[1:], Topics: [][]byte{topic, addressTopic(1), addressTopic(2)}, Data: big.NewInt(1000000).FillBytes(make([]byte, 32))}
	}
	info := &core.TransactionInfo{Id: hash, BlockNumber: 10, BlockTimeStamp: timestamp.UnixMilli(), Receipt: &core.ResourceReceipt{Result: core.Transaction_Result_SUCCESS}, Log: []*core.TransactionInfo_Log{{}, event(), event()}}
	tr := newTron()
	got, err := tr.receiptTransfers(info, id, 10, timestamp)
	if err != nil || len(got) != 2 {
		t.Fatalf("transfers=%d err=%v", len(got), err)
	}
	if got[0].EventIndex != "log:1" || got[1].EventIndex != "log:2" {
		t.Fatal("filtered position replaced receipt index")
	}
	info.Receipt.Result = core.Transaction_Result_REVERT
	if got, err := tr.receiptTransfers(info, id, 10, timestamp); err != nil || len(got) != 0 {
		t.Fatal("failed receipt produced a payment")
	}
	if _, err := tr.receiptTransfers(info, strings.Repeat("b", 64), 10, timestamp); err == nil {
		t.Fatal("wrong transaction receipt accepted")
	}
}
