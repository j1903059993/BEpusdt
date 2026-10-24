package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrTransferClaimed = errors.New("chain transfer already claimed")
var ErrTransferInvalid = errors.New("invalid chain transfer facts")
var transactionHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var eventIndexPattern = regexp.MustCompile(`^(log|contract):(?:0|[1-9][0-9]{0,18})$`)

// Transaction reservations serialize event claims, including legacy whole-tx
// reservations, in SQLite and PostgreSQL. They survive order deletion/restarts.
type TransferLock struct {
	Network string `gorm:"primaryKey;size:32"`
	TxHash  string `gorm:"primaryKey;size:128"`
}

func (TransferLock) TableName() string { return "bep_transfer_lock" }

type TransferClaim struct {
	Network    string `gorm:"primaryKey;size:32"`
	TxHash     string `gorm:"primaryKey;size:128"`
	EventIndex string `gorm:"primaryKey;size:64"`
	OrderID    int64  `gorm:"uniqueIndex;not null"`
	CreatedAt  time.Time
}

func (TransferClaim) TableName() string { return "bep_transfer_claim" }

// Explicit administrator reconciliation cannot release or steal an automatic
// event claim. Without a verified event index it reserves the WHOLE transaction.
func (o *Order) ConfirmManualTransfer(hash string) error {
	n, h, _, err := normalizeTransfer(TradeNetwork(o.TradeType), hash, "*")
	if err != nil {
		return err
	}
	var stored Order
	err = Db.Transaction(func(tx *gorm.DB) error {
		if err := lockTransfer(tx, n, h); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&stored, o.ID).Error; err != nil {
			return err
		}
		if stored.Status == OrderStatusCanceled {
			return ErrTransferInvalid
		}
		if stored.RefHash != "" && strings.TrimPrefix(strings.ToLower(stored.RefHash), "0x") != h {
			return ErrTransferClaimed
		}
		var claims []TransferClaim
		if err := tx.Where("network = ? AND tx_hash = ?", n, h).Find(&claims).Error; err != nil {
			return err
		}
		for _, claim := range claims {
			if claim.OrderID != stored.ID {
				return ErrTransferClaimed
			}
		}
		if len(claims) == 0 {
			if err := tx.Create(&TransferClaim{Network: n, TxHash: h, EventIndex: "*", OrderID: stored.ID, CreatedAt: time.Now()}).Error; err != nil {
				return err
			}
		}
		at := time.Now()
		if err := tx.Model(&stored).Updates(map[string]any{"ref_hash": hash, "status": OrderStatusSuccess, "confirmed_at": Datetime(at)}).Error; err != nil {
			return err
		}
		return tx.First(&stored, o.ID).Error
	})
	if err == nil {
		*o = stored
	}
	return err
}

func TradeNetwork(trade TradeType) string { return string(registry[trade].Network) }

func normalizeTransfer(network, hash, event string) (string, string, string, error) {
	network = strings.ToLower(strings.TrimSpace(network))
	hash = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(hash)), "0x")
	if network == "" || !transactionHashPattern.MatchString(hash) || (event != "*" && !eventIndexPattern.MatchString(event)) {
		return "", "", "", ErrTransferInvalid
	}
	return network, hash, event, nil
}

func lockTransfer(tx *gorm.DB, network, hash string) error {
	key := TransferLock{Network: network, TxHash: hash}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&key).Error; err != nil {
		return err
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("network = ? AND tx_hash = ?", network, hash).First(&key).Error
}

// Backfill legacy rows conservatively: unknown event identity reserves the
// whole transaction. This is additive and never changes historical order facts.
func reserveLegacyTransfers(db *gorm.DB) error {
	var orders []Order
	if err := db.Where("ref_hash <> '' AND ref_event_index = ''").Find(&orders).Error; err != nil {
		return err
	}
	for _, o := range orders {
		network, hash, _, err := normalizeTransfer(TradeNetwork(o.TradeType), o.RefHash, "*")
		if err != nil {
			continue
		} // unsupported historical networks keep existing paths
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&TransferClaim{Network: network, TxHash: hash, EventIndex: "*", OrderID: o.ID, CreatedAt: time.Now()}).Error; err != nil {
			return err
		}
	}
	return nil
}

// ClaimConfirming atomically reserves one verified event and changes the order.
// All database reads/writes here use tx; chain RPC must happen before this call.
func (o *Order) ClaimConfirming(network, hash, event string, block int, from, receiver string, at time.Time, amount decimal.Decimal) error {
	n, h, e, err := normalizeTransfer(network, hash, event)
	if err != nil {
		return err
	}
	if n != TradeNetwork(o.TradeType) || !amount.IsPositive() {
		return ErrTransferInvalid
	}
	var updated Order
	for attempt := 0; attempt < 5; attempt++ {
		err = Db.Transaction(func(tx *gorm.DB) error {
			if err := lockTransfer(tx, n, h); err != nil {
				return err
			}
			var stored Order
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&stored, o.ID).Error; err != nil {
				return err
			}
			if stored.TradeType != o.TradeType || stored.Amount != o.Amount || stored.Address != o.Address || stored.MatchAddress != o.MatchAddress || stored.Money != o.Money {
				return ErrTransferInvalid
			}
			match := stored.MatchAddress
			if match == "" {
				match = stored.Address
			}
			if receiver != match || stored.CreatedAt == nil || !stored.CreatedAt.Before(at) || !stored.ExpiredAt.After(at) {
				return ErrTransferInvalid
			}
			expected, parseErr := decimal.NewFromString(stored.Amount)
			if !stored.AddressLocked && (parseErr != nil || !expected.Equal(amount)) {
				return ErrTransferInvalid
			}
			var claims []TransferClaim
			if err := tx.Where("network = ? AND tx_hash = ?", n, h).Find(&claims).Error; err != nil {
				return err
			}
			for _, claim := range claims {
				if claim.EventIndex == e && claim.OrderID == stored.ID {
					if stored.RefHash == "" || strings.TrimPrefix(strings.ToLower(stored.RefHash), "0x") != h || stored.RefBlockNum != block || stored.FromAddress != from {
						return ErrTransferInvalid
					}
					updated = stored
					return nil
				}
				if e == "*" || claim.EventIndex == "*" || claim.EventIndex == e {
					return ErrTransferClaimed
				}
			}
			if stored.Status != OrderStatusWaiting && stored.Status != OrderStatusExpired {
				return ErrTransferInvalid
			}
			if stored.RefHash != "" {
				return ErrTransferClaimed
			}
			claim := TransferClaim{Network: n, TxHash: h, EventIndex: e, OrderID: stored.ID, CreatedAt: time.Now()}
			if err := tx.Create(&claim).Error; err != nil {
				return err
			}
			changes := map[string]any{"from_address": from, "confirmed_at": Datetime(at), "ref_hash": hash, "ref_block_num": block, "status": OrderStatusConfirming}
			if e != "*" {
				changes["ref_event_index"] = e
			}
			if stored.AddressLocked {
				rate, err := decimal.NewFromString(stored.Rate)
				if err != nil {
					return ErrTransferInvalid
				}
				changes["amount"] = amount.String()
				changes["money"] = rate.Mul(amount).String()
			}
			result := tx.Model(&Order{}).Where("id = ? AND status = ? AND ref_hash = ''", stored.ID, stored.Status).Updates(changes)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTransferClaimed
			}
			return tx.First(&updated, stored.ID).Error
		})
		if err == nil {
			*o = updated
			return nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "locked") && !strings.Contains(strings.ToLower(err.Error()), "busy") {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	return fmt.Errorf("claim chain transfer: %w", err)
}
