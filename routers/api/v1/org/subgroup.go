// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package org

import (
	"errors"
	"net/http"

	"gitea.dev/models/organization"
	user_model "gitea.dev/models/user"
	api "gitea.dev/modules/structs"
	"gitea.dev/modules/web"
	"gitea.dev/services/context"
	"gitea.dev/services/convert"
)

// ListSubgroups lists an organization's direct subgroups.
func ListSubgroups(ctx *context.APIContext) {
	// swagger:operation GET /orgs/{org}/subgroups organization orgListSubgroups
	// ---
	// summary: List an organization's subgroups
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/OrganizationList"
	orgs, err := organization.ListVisibleChildOrgs(ctx, ctx.Org.Organization.ID, ctx.Doer)
	if err != nil {
		ctx.APIErrorInternal(err)
		return
	}
	apiOrgs := make([]*api.Organization, len(orgs))
	for i := range orgs {
		apiOrgs[i] = convert.ToOrganization(ctx, orgs[i])
	}
	ctx.JSON(http.StatusOK, apiOrgs)
}

// CreateSubgroup creates a subgroup organization.
func CreateSubgroup(ctx *context.APIContext) {
	// swagger:operation POST /orgs/{org}/subgroups organization orgCreateSubgroup
	// ---
	// summary: Create a subgroup
	// description: A subgroup is an organization nested under another organization. Its web and git path is /{org}/{subgroup}/... while /{subgroup}/{repo} keeps working. The subgroup name is unique on the instance.
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the parent organization
	//   type: string
	//   required: true
	// - name: organization
	//   in: body
	//   required: true
	//   schema: { "$ref": "#/definitions/CreateOrgOption" }
	// responses:
	//   "201":
	//     "$ref": "#/responses/Organization"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "422":
	//     "$ref": "#/responses/validationError"
	form := web.GetForm[*api.CreateOrgOption](ctx)
	visibility := api.VisibleTypePublic
	if form.Visibility != "" {
		visibility = api.VisibilityModes[form.Visibility]
	}
	org := &organization.Organization{
		Name:                      form.UserName,
		FullName:                  form.FullName,
		Email:                     form.Email,
		Description:               form.Description,
		Website:                   form.Website,
		Location:                  form.Location,
		IsActive:                  true,
		Type:                      user_model.UserTypeOrganization,
		Visibility:                visibility,
		RepoAdminChangeTeamAccess: form.RepoAdminChangeTeamAccess,
	}
	if err := organization.CreateSubOrganization(ctx, ctx.Org.Organization, org, ctx.Doer); err != nil {
		if respondSubgroupError(ctx, err) {
			return
		}
		if user_model.IsErrUserAlreadyExist(err) {
			ctx.APIError(http.StatusUnprocessableEntity, "user already exists")
			return
		}
		ctx.APIErrorInternal(err)
		return
	}
	ctx.JSON(http.StatusCreated, convert.ToOrganization(ctx, org))
}

func respondSubgroupError(ctx *context.APIContext, err error) bool {
	subgroupErr, ok := errors.AsType[organization.ErrSubgroup](err)
	if !ok {
		return false
	}
	status := http.StatusUnprocessableEntity
	if subgroupErr.Code == organization.SubgroupCodeForbidden {
		status = http.StatusForbidden
	}
	ctx.APIError(status, subgroupErr.APIMessage())
	return true
}
