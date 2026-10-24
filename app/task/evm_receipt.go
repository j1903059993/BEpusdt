package task

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
	"github.com/v03413/bepusdt/app/model"
)

func evmReceiptConfirms(o model.Order, receipt gjson.Result) bool {
	if receipt.Get("status").String() != "0x1" {
		return false
	}
	if o.RefEventIndex == "" {
		return true
	} // pre-upgrade confirming orders
	if !strings.EqualFold(receipt.Get("transactionHash").String(), o.RefHash) || receipt.Get("blockNumber").String() != fmt.Sprintf("0x%x", o.RefBlockNum) {
		return false
	}
	if o.RefEventIndex == "contract:0" {
		return true
	}
	receiver := orderMatchAddress(o)
	for _, event := range receipt.Get("logs").Array() {
		index, err := strconv.ParseUint(strings.TrimPrefix(event.Get("logIndex").String(), "0x"), 16, 64)
		if err != nil || "log:"+strconv.FormatUint(index, 10) != o.RefEventIndex || event.Get("removed").Bool() {
			continue
		}
		topics := event.Get("topics").Array()
		value := event.Get("data").String()
		if len(topics) != 3 || len(topics[1].String()) != 66 || len(topics[2].String()) != 66 || len(value) != 66 || topics[0].String() != evmTransferEvent {
			continue
		}
		if !strings.EqualFold(event.Get("address").String(), model.GetTradeContract(o.TradeType)) || !strings.EqualFold("0x"+topics[1].String()[26:], o.FromAddress) || !strings.EqualFold("0x"+topics[2].String()[26:], receiver) {
			continue
		}
		raw, ok := new(big.Int).SetString(value[2:], 16)
		expected, err := decimal.NewFromString(o.Amount)
		return ok && err == nil && decimal.NewFromBigInt(raw, model.GetTradeDecimal(o.TradeType)).Equal(expected)
	}
	return false
}
