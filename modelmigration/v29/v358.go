// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
)

// AddOrgSubgroupColumns stores the parent organization and the public route path.
// Existing users and top-level organizations get parent_id 0 and route_path = lower_name,
// so current URLs and logins do not change.
func AddOrgSubgroupColumns(_ context.Context, x base.EngineMigration) error {
	type User struct {
		ParentID  int64  `xorm:"INDEX NOT NULL DEFAULT 0"`
		RoutePath string `xorm:"VARCHAR(512) INDEX"`
	}
	if err := x.Sync(new(User)); err != nil {
		return err
	}
	_, err := x.Exec("UPDATE `user` SET route_path = lower_name WHERE route_path IS NULL OR route_path = ''")
	return err
}
