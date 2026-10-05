// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package organization

import (
	"context"
	"fmt"
	"strings"

	"gitea.dev/models/db"
	user_model "gitea.dev/models/user"

	"xorm.io/builder"
)

// OrgRouteRedirect sends an old nested organization path to the organization that used to own it.
type OrgRouteRedirect struct {
	ID        int64  `xorm:"pk autoincr"`
	RoutePath string `xorm:"UNIQUE VARCHAR(512) NOT NULL"`
	OrgID     int64  `xorm:"INDEX NOT NULL"`
}

func init() {
	db.RegisterModel(new(OrgRouteRedirect))
}

// GetOrgByRoutePath returns the organization whose public path is route.
func GetOrgByRoutePath(ctx context.Context, route string) (*Organization, error) {
	route = strings.ToLower(strings.Trim(route, "/"))
	if route == "" {
		return nil, ErrOrgNotExist{Name: route}
	}
	org, has, err := db.Get[Organization](ctx, builder.Eq{
		"route_path": route,
		"`type`":     user_model.UserTypeOrganization,
	})
	if err != nil {
		return nil, fmt.Errorf("GetOrgByRoutePath: %w", err)
	}
	if !has {
		return nil, ErrOrgNotExist{Name: route}
	}
	return org, nil
}

func clearOrgRouteRedirect(ctx context.Context, route string) error {
	route = strings.ToLower(strings.Trim(route, "/"))
	if route == "" {
		return nil
	}
	_, err := db.GetEngine(ctx).Delete(&OrgRouteRedirect{RoutePath: route})
	if err != nil {
		return fmt.Errorf("clearOrgRouteRedirect: %w", err)
	}
	return nil
}

func recordOrgRouteRedirect(ctx context.Context, oldPath string, orgID int64) error {
	oldPath = strings.ToLower(strings.Trim(oldPath, "/"))
	if oldPath == "" || !strings.Contains(oldPath, "/") || orgID <= 0 {
		return nil
	}
	if _, err := GetOrgByRoutePath(ctx, oldPath); err == nil {
		return nil
	} else if !IsErrOrgNotExist(err) {
		return fmt.Errorf("recordOrgRouteRedirect: %w", err)
	}
	if err := clearOrgRouteRedirect(ctx, oldPath); err != nil {
		return err
	}
	if err := db.Insert(ctx, &OrgRouteRedirect{RoutePath: oldPath, OrgID: orgID}); err != nil {
		return fmt.Errorf("recordOrgRouteRedirect: %w", err)
	}
	return nil
}

// LookupOrgRouteRedirect returns the organization an old nested path now belongs to.
// A live route wins, so a reused path is not redirected away.
func LookupOrgRouteRedirect(ctx context.Context, route string) (*user_model.User, bool, error) {
	route = strings.ToLower(strings.Trim(route, "/"))
	if route == "" || !strings.Contains(route, "/") {
		return nil, false, nil
	}
	if _, err := GetOrgByRoutePath(ctx, route); err == nil {
		return nil, false, nil
	} else if !IsErrOrgNotExist(err) {
		return nil, false, fmt.Errorf("LookupOrgRouteRedirect: %w", err)
	}
	row, has, err := db.Get[OrgRouteRedirect](ctx, builder.Eq{"route_path": route})
	if err != nil {
		return nil, false, fmt.Errorf("LookupOrgRouteRedirect: %w", err)
	}
	if !has {
		return nil, false, nil
	}
	org, err := GetOrgByID(ctx, row.OrgID)
	if err != nil {
		if IsErrOrgNotExist(err) || user_model.IsErrUserNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("LookupOrgRouteRedirect: %w", err)
	}
	return org.AsUser(), true, nil
}
