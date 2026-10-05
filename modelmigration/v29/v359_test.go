// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm/schemas"
)

func TestRelaxUserNameUniqueness(t *testing.T) {
	type User struct {
		ID        int64  `xorm:"pk autoincr"`
		LowerName string `xorm:"UNIQUE NOT NULL"`
		Name      string `xorm:"UNIQUE NOT NULL"`
		ParentID  int64  `xorm:"INDEX NOT NULL DEFAULT 0"`
	}

	x, deferrable := migrationtest.PrepareTestEnv(t, 0, new(User))
	defer deferrable()
	if x == nil || t.Failed() {
		return
	}

	_, err := x.Insert(&User{ID: 1, LowerName: "org3", Name: "org3", ParentID: 0})
	require.NoError(t, err)

	require.NoError(t, RelaxUserNameUniqueness(t.Context(), x))

	var got struct {
		DiskName     string
		NamespaceKey string
	}
	has, err := x.Table("user").Where("id = ?", 1).Cols("disk_name", "namespace_key").Get(&got)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Equal(t, "org3", got.DiskName)
	assert.Equal(t, "0/org3", got.NamespaceKey)

	indexes, err := x.Dialect().GetIndexes(x.DB(), t.Context(), "user")
	require.NoError(t, err)
	for _, idx := range indexes {
		if idx.Type == schemas.UniqueType && len(idx.Cols) == 1 {
			assert.NotEqual(t, "lower_name", idx.Cols[0])
			assert.NotEqual(t, "name", idx.Cols[0])
		}
	}

	_, err = x.Exec("INSERT INTO `user` (id, lower_name, name, parent_id, disk_name, namespace_key) VALUES (?, ?, ?, ?, ?, ?)",
		2, "org3", "org3", 9, "subgroup-2", "9/org3")
	require.NoError(t, err)
}
