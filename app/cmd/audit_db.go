package cmd

import (
	"context"
	"encoding/json"
	"os"

	"github.com/urfave/cli/v3"
	"github.com/v03413/bepusdt/app/model"
)

// Rehearse additive schema/legacy reservations on an explicitly supplied COPY.
// No scanner, notifier, HTTP listener, wallet operation or credentials output.
var AuditDB = &cli.Command{Name: "audit-db", Usage: "验证隔离数据库副本的升级与历史认领", Flags: []cli.Flag{&cli.StringFlag{Name: "sqlite", Required: true}}, Action: func(ctx context.Context, c *cli.Command) error {
	path := c.String("sqlite")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if err := model.Init(path, ""); err != nil {
		return err
	}
	defer model.Close()
	var orders, claims, legacy int64
	if err := model.Db.Model(&model.Order{}).Count(&orders).Error; err != nil {
		return err
	}
	if err := model.Db.Model(&model.TransferClaim{}).Count(&claims).Error; err != nil {
		return err
	}
	if err := model.Db.Model(&model.TransferClaim{}).Where("event_index = ?", "*").Count(&legacy).Error; err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "COPY_AUDIT_OK", "orders": orders, "claims": claims, "legacy_reservations": legacy})
}}
