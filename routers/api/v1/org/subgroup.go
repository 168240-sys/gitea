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
	// description: A subgroup is an organization nested under another organization. Its web and git path is /{org}/{subgroup}/... The local name only has to be unique under that parent. A colliding name is reached through the nested path; its flat URL uses a generated disk name.
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

// GetSubgroup gets one direct subgroup by its local name.
func GetSubgroup(ctx *context.APIContext) {
	// swagger:operation GET /orgs/{org}/subgroups/{subgroup} organization orgGetSubgroup
	// ---
	// summary: Get a subgroup
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the parent organization
	//   type: string
	//   required: true
	// - name: subgroup
	//   in: path
	//   description: local name of the subgroup
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/Organization"
	//   "404":
	//     "$ref": "#/responses/notFound"
	org, err := organization.GetChildOrg(ctx, ctx.Org.Organization.ID, ctx.PathParam("subgroup"))
	if err != nil {
		if organization.IsErrOrgNotExist(err) {
			ctx.APIErrorNotFound()
			return
		}
		ctx.APIErrorInternal(err)
		return
	}
	if !organization.HasOrgOrUserVisible(ctx, org.AsUser(), ctx.Doer) {
		ctx.APIErrorNotFound()
		return
	}
	ctx.JSON(http.StatusOK, convert.ToOrganization(ctx, org))
}

// MoveSubgroup moves a direct subgroup under a new parent.
func MoveSubgroup(ctx *context.APIContext) {
	// swagger:operation POST /orgs/{org}/subgroups/{subgroup}/move organization orgMoveSubgroup
	// ---
	// summary: Move a subgroup
	// description: The caller must own the subgroup and the new parent. The git directory is not renamed. Previous nested URLs redirect to the new path.
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the current parent organization
	//   type: string
	//   required: true
	// - name: subgroup
	//   in: path
	//   description: local name of the subgroup
	//   type: string
	//   required: true
	// - name: body
	//   in: body
	//   required: true
	//   schema: { "$ref": "#/definitions/MoveOrgOption" }
	// responses:
	//   "200":
	//     "$ref": "#/responses/Organization"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "422":
	//     "$ref": "#/responses/validationError"
	org, err := organization.GetChildOrg(ctx, ctx.Org.Organization.ID, ctx.PathParam("subgroup"))
	if err != nil {
		if organization.IsErrOrgNotExist(err) {
			ctx.APIErrorNotFound()
			return
		}
		ctx.APIErrorInternal(err)
		return
	}
	moveOrganization(ctx, org, web.GetForm[*api.MoveOrgOption](ctx))
}

// MoveOrganization moves this organization under a new parent, or to the top level.
func MoveOrganization(ctx *context.APIContext) {
	// swagger:operation POST /orgs/{org}/move organization orgMove
	// ---
	// summary: Move an organization
	// description: The caller must own this organization and the new parent. An empty parent makes it top-level. The git directory is not renamed. Previous nested URLs redirect to the new path.
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
	//   type: string
	//   required: true
	// - name: body
	//   in: body
	//   required: true
	//   schema: { "$ref": "#/definitions/MoveOrgOption" }
	// responses:
	//   "200":
	//     "$ref": "#/responses/Organization"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "422":
	//     "$ref": "#/responses/validationError"
	moveOrganization(ctx, ctx.Org.Organization, web.GetForm[*api.MoveOrgOption](ctx))
}

func moveOrganization(ctx *context.APIContext, org *organization.Organization, form *api.MoveOrgOption) {
	var parent *organization.Organization
	if form.Parent != "" {
		var err error
		parent, err = organization.GetOrgByName(ctx, form.Parent)
		if err != nil {
			if organization.IsErrOrgNotExist(err) {
				ctx.APIErrorNotFound("parent")
				return
			}
			ctx.APIErrorInternal(err)
			return
		}
	}
	if err := organization.MoveSubOrganization(ctx, org, parent, ctx.Doer); err != nil {
		if respondSubgroupError(ctx, err) {
			return
		}
		ctx.APIErrorInternal(err)
		return
	}
	ctx.JSON(http.StatusOK, convert.ToOrganization(ctx, org))
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
