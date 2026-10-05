// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package org

import (
	"errors"
	"net/http"

	audit_model "gitea.dev/models/audit"
	"gitea.dev/models/db"
	"gitea.dev/models/organization"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/web"
	"gitea.dev/services/audit"
	"gitea.dev/services/context"
	"gitea.dev/services/forms"
)

// NewSubgroup renders the subgroup creation form.
func NewSubgroup(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("org.subgroup.new")
	ctx.Data["IsSubgroup"] = true
	ctx.Data["visibility"] = ctx.Org.Organization.Visibility
	ctx.Data["repo_admin_change_team_access"] = true
	ctx.HTML(http.StatusOK, tplCreateOrg)
}

// NewSubgroupPost creates a subgroup organization.
func NewSubgroupPost(ctx *context.Context) {
	form := *web.GetForm[*forms.CreateOrgForm](ctx)
	ctx.Data["Title"] = ctx.Tr("org.subgroup.new")
	ctx.Data["IsSubgroup"] = true
	ctx.Data["visibility"] = ctx.Org.Organization.Visibility

	if ctx.HasError() {
		ctx.HTML(http.StatusOK, tplCreateOrg)
		return
	}

	org := &organization.Organization{
		Name:                      form.OrgName,
		IsActive:                  true,
		Type:                      user_model.UserTypeOrganization,
		Visibility:                form.Visibility,
		RepoAdminChangeTeamAccess: form.RepoAdminChangeTeamAccess,
	}
	if err := organization.CreateSubOrganization(ctx, ctx.Org.Organization, org, ctx.Doer); err != nil {
		ctx.Data["Err_OrgName"] = true
		var errNameReserved db.ErrNameReserved
		var errNamePatternNotAllowed db.ErrNamePatternNotAllowed
		var subgroupErr organization.ErrSubgroup
		switch {
		case user_model.IsErrUserAlreadyExist(err):
			ctx.RenderWithErrDeprecated(ctx.Tr("form.org_name_been_taken"), tplCreateOrg, &form)
		case errors.As(err, &errNameReserved):
			ctx.RenderWithErrDeprecated(ctx.Tr("org.form.name_reserved", errNameReserved.Name), tplCreateOrg, &form)
		case errors.As(err, &errNamePatternNotAllowed):
			ctx.RenderWithErrDeprecated(ctx.Tr("org.form.name_pattern_not_allowed", errNamePatternNotAllowed.Pattern), tplCreateOrg, &form)
		case errors.As(err, &subgroupErr):
			ctx.RenderWithErrDeprecated(ctx.Tr("org.subgroup.err_"+subgroupErr.Code), tplCreateOrg, &form)
		default:
			ctx.ServerError("CreateSubOrganization", err)
		}
		return
	}
	audit.Record(ctx, audit_model.OrganizationCreate, org.AsUser())
	ctx.Flash.Success(ctx.Tr("org.subgroup.created", org.RoutePath))
	ctx.Redirect(org.AsUser().DashboardLink())
}
