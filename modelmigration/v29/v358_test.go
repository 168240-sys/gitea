// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddOrgSubgroupColumns(t *testing.T) {
	type User struct {
		ID        int64  `xorm:"pk autoincr"`
		LowerName string `xorm:"UNIQUE NOT NULL"`
		Name      string `xorm:"UNIQUE NOT NULL"`
	}

	x, deferrable := migrationtest.PrepareTestEnv(t, 0, new(User))
	defer deferrable()
	if x == nil || t.Failed() {
		return
	}

	_, err := x.Insert(&User{ID: 1, LowerName: "org3", Name: "org3"})
	require.NoError(t, err)

	require.NoError(t, AddOrgSubgroupColumns(t.Context(), x))

	var got struct {
		ParentID  int64
		RoutePath string
	}
	has, err := x.Table("user").Where("id = ?", 1).Cols("parent_id", "route_path").Get(&got)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Equal(t, int64(0), got.ParentID)
	assert.Equal(t, "org3", got.RoutePath)
}
