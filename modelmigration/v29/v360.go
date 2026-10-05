// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
)

// AddOrgRouteRedirect stores previous subgroup paths so a move or rename can redirect them.
func AddOrgRouteRedirect(_ context.Context, x base.EngineMigration) error {
	type OrgRouteRedirect struct {
		ID        int64  `xorm:"pk autoincr"`
		RoutePath string `xorm:"UNIQUE VARCHAR(512) NOT NULL"`
		OrgID     int64  `xorm:"INDEX NOT NULL"`
	}
	return x.Sync(new(OrgRouteRedirect))
}
