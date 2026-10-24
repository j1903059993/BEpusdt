package task

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/v03413/bepusdt/app/model"
)

// A transfer can fall inside overlapping cashier lifetimes after an amount is
// reused. In-memory removal is not a durable cross-order reservation.
func TestLegacyTransactionCannotConfirmAnotherOrder(t *testing.T) {
	initTaskTestEnv(t, map[model.ConfKey]string{model.PaymentMatchMode: "classic"})
	now := time.Now()
	makeOrder := func(id string) model.Order {
		return createTestOrder(t, model.Order{OrderId: id, TradeId: id, TradeType: model.UsdtBep20,
			Address: "0x1111111111111111111111111111111111111111", Amount: "1", Money: "7",
			Status: model.OrderStatusWaiting, ExpiredAt: now.Add(time.Minute)}, now.Add(-time.Minute))
	}
	first, second := makeOrder("first"), makeOrder("second")
	hash := "0x" + strings.Repeat("a", 64)
	if err := first.MarkConfirming(10, "sender", hash, now, decimal.NewFromInt(1)); err != nil {
		t.Fatal(err)
	}
	first.SetSuccess()
	if err := second.MarkConfirming(10, "sender", hash, now, decimal.NewFromInt(1)); err == nil {
		t.Fatal("same historical transaction confirmed a second order")
	}
	if got := getTestOrder(t, second.TradeId); got.Status != model.OrderStatusWaiting {
		t.Fatal("duplicate claim changed target order")
	}
}

func TestEventClaimsAreAtomicAndPersistent(t *testing.T) {
	initTaskTestEnv(t, map[model.ConfKey]string{model.PaymentMatchMode: "classic"})
	now := time.Now()
	hash := "0x" + strings.Repeat("b", 64)
	address := "0x1111111111111111111111111111111111111111"
	makeOrder := func(id string) model.Order {
		return createTestOrder(t, model.Order{OrderId: id, TradeId: id, TradeType: model.UsdtBep20, Address: address, Amount: "1", Money: "7", Status: model.OrderStatusWaiting, ExpiredAt: now.Add(time.Minute)}, now.Add(-time.Minute))
	}
	first, second := makeOrder("event-first"), makeOrder("event-second")
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, order := range []model.Order{first, second} {
		wg.Add(1)
		go func(o model.Order) {
			defer wg.Done()
			errs <- o.ClaimConfirming("bsc", hash, "log:7", 10, "sender", address, now, decimal.NewFromInt(1))
		}(order)
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("claims succeeded = %d, want exactly one", successes)
	}
	var claim model.TransferClaim
	if err := model.Db.First(&claim).Error; err != nil {
		t.Fatal(err)
	}
	// A second real event in the same transaction remains independently usable.
	next := makeOrder("second-real-event")
	if err := next.ClaimConfirming("bsc", hash, "log:8", 10, "sender", address, now, decimal.NewFromInt(1)); err != nil {
		t.Fatal(err)
	}
	if err := model.Db.Delete(&model.Order{}, claim.OrderID).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	replacement := makeOrder("restarted-replay")
	if err := replacement.ClaimConfirming("bsc", hash, "log:7", 10, "sender", address, now, decimal.NewFromInt(1)); err == nil {
		t.Fatal("deleted order released persistent claim")
	}
}

func TestClaimValidatesActualOrderFacts(t *testing.T) {
	initTaskTestEnv(t, map[model.ConfKey]string{model.PaymentMatchMode: "classic"})
	now := time.Now()
	address := "0x1111111111111111111111111111111111111111"
	for _, scenario := range []string{"network", "amount", "address", "old", "late", "missing-event", "closed", "snapshot-change"} {
		t.Run(scenario, func(t *testing.T) {
			o := createTestOrder(t, model.Order{OrderId: scenario, TradeId: scenario, TradeType: model.UsdtBep20, Address: address, Amount: "1", Money: "7", Status: model.OrderStatusWaiting, ExpiredAt: now.Add(time.Minute)}, now.Add(-time.Minute))
			network, event, receiver, at, amount := "bsc", "log:0", address, now, decimal.NewFromInt(1)
			switch scenario {
			case "network":
				network = "polygon"
			case "amount":
				amount = decimal.RequireFromString("0.99")
			case "address":
				receiver = "other"
			case "old":
				at = now.Add(-time.Hour)
			case "late":
				at = now.Add(time.Hour)
			case "missing-event":
				event = ""
			case "closed":
				model.Db.Model(&o).Update("status", model.OrderStatusCanceled)
			case "snapshot-change":
				model.Db.Model(&model.Order{}).Where("id = ?", o.ID).Update("amount", "2")
			}
			if err := o.ClaimConfirming(network, "0x"+strings.Repeat("c", 64), event, 10, "sender", receiver, at, amount); err == nil {
				t.Fatal("invalid facts claimed a payment")
			}
		})
	}
	var count int64
	model.Db.Model(&model.TransferClaim{}).Count(&count)
	if count != 0 {
		t.Fatal("failed validation left claims behind")
	}
}

func TestLegacyBackfillReservesAllEvents(t *testing.T) {
	initTaskTestEnv(t, map[model.ConfKey]string{model.PaymentMatchMode: "classic"})
	now := time.Now()
	hash := "0x" + strings.Repeat("d", 64)
	address := "0x1111111111111111111111111111111111111111"
	createTestOrder(t, model.Order{OrderId: "legacy", TradeId: "legacy", TradeType: model.UsdtBep20, Address: address, Amount: "1", Money: "7", Status: model.OrderStatusSuccess, RefHash: hash, ExpiredAt: now.Add(time.Minute)}, now.Add(-time.Minute))
	if err := model.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	o := createTestOrder(t, model.Order{OrderId: "legacy-replay", TradeId: "legacy-replay", TradeType: model.UsdtBep20, Address: address, Amount: "1", Money: "7", Status: model.OrderStatusWaiting, ExpiredAt: now.Add(time.Minute)}, now.Add(-time.Minute))
	if err := o.ClaimConfirming("bsc", hash, "log:9", 10, "sender", address, now, decimal.NewFromInt(1)); err == nil {
		t.Fatal("legacy transaction event was reused")
	}
	if err := o.ConfirmManualTransfer(hash); err == nil {
		t.Fatal("administrator reconciliation stole a historical transaction")
	}
}
