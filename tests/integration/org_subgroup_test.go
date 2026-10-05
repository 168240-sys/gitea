// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"testing"

	auth_model "gitea.dev/models/auth"
	api "gitea.dev/modules/structs"
	"gitea.dev/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgSubgroups(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	ownerToken := getUserToken(t, "user2", auth_model.AccessTokenScopeWriteOrganization)
	memberToken := getUserToken(t, "user4", auth_model.AccessTokenScopeWriteOrganization)

	req := NewRequestWithJSON(t, "POST", "/api/v1/orgs/org3/subgroups", &api.CreateOrgOption{
		UserName:   "subgroupapi",
		Visibility: "public",
	}).AddTokenAuth(memberToken)
	MakeRequest(t, req, http.StatusForbidden)

	req = NewRequestWithJSON(t, "POST", "/api/v1/orgs/org3/subgroups", &api.CreateOrgOption{
		UserName:   "repo3",
		Visibility: "public",
	}).AddTokenAuth(ownerToken)
	MakeRequest(t, req, http.StatusUnprocessableEntity)

	req = NewRequestWithJSON(t, "POST", "/api/v1/orgs/org3/subgroups", &api.CreateOrgOption{
		UserName:   "subgroupapi",
		FullName:   "API subgroup",
		Visibility: "public",
	}).AddTokenAuth(ownerToken)
	resp := MakeRequest(t, req, http.StatusCreated)
	created := DecodeJSON(t, resp, &api.Organization{})
	assert.Equal(t, "subgroupapi", created.Name)
	assert.Equal(t, "org3/subgroupapi", created.RoutePath)
	assert.Positive(t, created.ParentID)

	req = NewRequest(t, "GET", "/api/v1/orgs/org3/subgroups").AddTokenAuth(ownerToken)
	resp = MakeRequest(t, req, http.StatusOK)
	list := DecodeJSON(t, resp, &[]*api.Organization{})
	require.Len(t, *list, 1)
	assert.Equal(t, "subgroupapi", (*list)[0].Name)

	// Nested URL is rewritten onto the subgroup. The flat slug still works.
	req = NewRequest(t, "GET", "/org3/subgroupapi")
	resp = MakeRequest(t, req, http.StatusOK)
	assert.Contains(t, resp.Body.String(), "API subgroup")

	req = NewRequest(t, "GET", "/subgroupapi")
	resp = MakeRequest(t, req, http.StatusOK)
	assert.Contains(t, resp.Body.String(), "API subgroup")

	// An existing private repository keeps its path for a user who can see it.
	loginUser(t, "user2").MakeRequest(t, NewRequest(t, "GET", "/org3/repo3"), http.StatusOK)

	req = NewRequest(t, "DELETE", "/api/v1/orgs/org3").AddTokenAuth(ownerToken)
	MakeRequest(t, req, http.StatusUnprocessableEntity)

	// The same local name can exist under another parent, and a user's login still wins the flat URL.
	req = NewRequestWithJSON(t, "POST", "/api/v1/orgs", &api.CreateOrgOption{
		UserName:   "otherparent",
		Visibility: "public",
	}).AddTokenAuth(ownerToken)
	MakeRequest(t, req, http.StatusCreated)

	req = NewRequestWithJSON(t, "POST", "/api/v1/orgs/otherparent/subgroups", &api.CreateOrgOption{
		UserName:   "subgroupapi",
		FullName:   "Second subgroup",
		Visibility: "public",
	}).AddTokenAuth(ownerToken)
	resp = MakeRequest(t, req, http.StatusCreated)
	second := DecodeJSON(t, resp, &api.Organization{})
	assert.Equal(t, "otherparent/subgroupapi", second.RoutePath)

	req = NewRequest(t, "GET", "/api/v1/orgs/otherparent/subgroups/subgroupapi").AddTokenAuth(ownerToken)
	resp = MakeRequest(t, req, http.StatusOK)
	got := DecodeJSON(t, resp, &api.Organization{})
	assert.Equal(t, second.ID, got.ID)

	req = NewRequestWithJSON(t, "POST", "/api/v1/orgs/org3/subgroups/subgroupapi/move", &api.MoveOrgOption{
		Parent: "otherparent",
	}).AddTokenAuth(ownerToken)
	MakeRequest(t, req, http.StatusUnprocessableEntity)

	req = NewRequestWithJSON(t, "POST", "/api/v1/orgs", &api.CreateOrgOption{
		UserName:   "thirdparent",
		Visibility: "public",
	}).AddTokenAuth(ownerToken)
	MakeRequest(t, req, http.StatusCreated)

	req = NewRequestWithJSON(t, "POST", "/api/v1/orgs/org3/subgroups/subgroupapi/move", &api.MoveOrgOption{
		Parent: "thirdparent",
	}).AddTokenAuth(ownerToken)
	resp = MakeRequest(t, req, http.StatusOK)
	moved := DecodeJSON(t, resp, &api.Organization{})
	assert.Equal(t, "thirdparent/subgroupapi", moved.RoutePath)

	req = NewRequest(t, "GET", "/org3/subgroupapi")
	resp = MakeRequest(t, req, http.StatusMovedPermanently)
	assert.Equal(t, "/thirdparent/subgroupapi", resp.Header().Get("Location"))
}
