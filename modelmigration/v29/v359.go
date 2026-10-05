// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"
	"fmt"
	"strings"

	"gitea.dev/modelmigration/base"

	"xorm.io/xorm/schemas"
)

// RelaxUserNameUniqueness lets two subgroups share a local name.
// disk_name stays globally unique (git directory and router key).
// namespace_key ("parentID/lowerName") keeps logins and siblings unique.
func RelaxUserNameUniqueness(ctx context.Context, x base.EngineMigration) error {
	type User struct {
		DiskName     string `xorm:"VARCHAR(255)"`
		NamespaceKey string `xorm:"VARCHAR(512)"`
	}
	if err := x.Sync(new(User)); err != nil {
		return err
	}

	type row struct {
		ID           int64
		LowerName    string
		ParentID     int64
		DiskName     string
		NamespaceKey string
	}
	rows := make([]row, 0, 64)
	if err := x.Table("user").Cols("id", "lower_name", "parent_id", "disk_name", "namespace_key").Find(&rows); err != nil {
		return err
	}
	for _, r := range rows {
		disk := r.LowerName
		ns := fmt.Sprintf("%d/%s", r.ParentID, r.LowerName)
		if r.DiskName == disk && r.NamespaceKey == ns {
			continue
		}
		if _, err := x.Exec("UPDATE `user` SET disk_name = ?, namespace_key = ? WHERE id = ?", disk, ns, r.ID); err != nil {
			return err
		}
	}

	indexes, err := x.Dialect().GetIndexes(x.DB(), ctx, "user")
	if err != nil {
		return err
	}
	for _, idx := range indexes {
		if idx.Type != schemas.UniqueType || len(idx.Cols) != 1 {
			continue
		}
		switch strings.Trim(strings.ToLower(idx.Cols[0]), "\"`[]") {
		case "lower_name", "name":
			if _, err := x.Exec(x.Dialect().DropIndexSQL("user", idx)); err != nil {
				return err
			}
		}
	}

	for _, col := range []string{"disk_name", "namespace_key"} {
		index := schemas.NewIndex(col, schemas.UniqueType)
		index.AddColumn(col)
		if _, err := x.Exec(x.Dialect().CreateIndexSQL("user", index)); err != nil {
			return err
		}
	}
	return nil
}
