package task

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/tronprotocol/core"
)

// The index is the position in the COMPLETE receipt log array, before any
// filtering. It is stable across retries, unlike a filtered transfer-list index.
func (t *tron) receiptTransfers(info *core.TransactionInfo, txID string, block int, timestamp time.Time) ([]transfer, error) {
	if info == nil || len(info.Id) != 32 || hex.EncodeToString(info.Id) != txID || info.BlockNumber != int64(block) || info.BlockTimeStamp != timestamp.UnixMilli() {
		return nil, fmt.Errorf("TRON receipt identity mismatch")
	}
	if info.Receipt == nil {
		return nil, fmt.Errorf("TRON execution receipt missing")
	}
	if info.Receipt.Result != core.Transaction_Result_SUCCESS || info.Result != core.TransactionInfo_SUCESS {
		return nil, nil
	}
	result := make([]transfer, 0)
	for index, event := range info.Log {
		if event == nil || len(event.Topics) != 3 || len(event.Topics[0]) != 32 || len(event.Topics[1]) != 32 || len(event.Topics[2]) != 32 || len(event.Data) != 32 {
			continue
		}
		if hex.EncodeToString(event.Topics[0]) != strings.TrimPrefix(evmTransferEvent, "0x") {
			continue
		}
		if !bytes.Equal(event.Topics[1][:12], make([]byte, 12)) || !bytes.Equal(event.Topics[2][:12], make([]byte, 12)) {
			continue
		}
		contract := event.Address
		if len(contract) == 21 && contract[0] == 0x41 {
			contract = contract[1:]
		}
		trade := model.TradeType("")
		if bytes.Equal(contract, usdtTrc20ContractAddress[1:]) {
			trade = model.UsdtTrc20
		}
		if bytes.Equal(contract, usdcTrc20ContractAddress[1:]) {
			trade = model.UsdcTrc20
		}
		if trade == "" {
			continue
		}
		amount := new(big.Int).SetBytes(event.Data)
		if amount.Sign() <= 0 {
			continue
		}
		result = append(result, transfer{Network: conf.Tron, TxHash: txID, EventIndex: "log:" + strconv.Itoa(index), BlockNum: block, Timestamp: timestamp,
			FromAddress: t.base58CheckEncode(append([]byte{0x41}, event.Topics[1][12:]...)), RecvAddress: t.base58CheckEncode(append([]byte{0x41}, event.Topics[2][12:]...)),
			TradeType: trade, Amount: decimal.NewFromBigInt(amount, model.GetTradeDecimal(trade))})
	}
	return result, nil
}
