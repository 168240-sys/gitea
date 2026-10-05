// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package organization_test

import (
	"testing"

	"gitea.dev/models/db"
	"gitea.dev/models/organization"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/structs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSubOrganization(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	parent := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	child := &organization.Organization{
		Name:       "subgroup1",
		Visibility: structs.VisibleTypePublic,
	}
	require.NoError(t, organization.CreateSubOrganization(t.Context(), parent, child, owner))
	assert.Equal(t, parent.ID, child.ParentID)
	assert.Equal(t, "org3/subgroup1", child.RoutePath)

	loaded := unittest.AssertExistsAndLoadBean(t, &organization.Organization{Name: "subgroup1"})
	assert.Equal(t, parent.ID, loaded.ParentID)

	rewritten, ok := organization.RewriteNestedPath(t.Context(), "/org3/subgroup1/repo3/issues")
	assert.True(t, ok)
	assert.Equal(t, "/subgroup1/repo3/issues", rewritten)

	unchanged, ok := organization.RewriteNestedPath(t.Context(), "/org3/repo3/issues")
	assert.False(t, ok)
	assert.Equal(t, "/org3/repo3/issues", unchanged)

	groupHome, ok := organization.RewriteNestedPath(t.Context(), "/org3/subgroup1")
	assert.True(t, ok)
	assert.Equal(t, "/subgroup1", groupHome)

	packages, ok := organization.RewriteNestedPath(t.Context(), "/org3/subgroup1/-/packages")
	assert.True(t, ok)
	assert.Equal(t, "/subgroup1/-/packages", packages)

	// A repository of the parent occupies that path segment.
	conflict := &organization.Organization{Name: "repo3", Visibility: structs.VisibleTypePublic}
	err := organization.CreateSubOrganization(t.Context(), parent, conflict, owner)
	require.Error(t, err)
	var subgroupErr organization.ErrSubgroup
	require.ErrorAs(t, err, &subgroupErr)
	assert.Equal(t, organization.SubgroupCodeRepoConflict, subgroupErr.Code)

	// A private parent cannot contain a public subgroup.
	privateParent := &organization.Organization{Name: "privateparent", Visibility: structs.VisibleTypePrivate}
	require.NoError(t, organization.CreateOrganization(t.Context(), privateParent, owner))
	tooPublic := &organization.Organization{Name: "toopublic", Visibility: structs.VisibleTypePublic}
	err = organization.CreateSubOrganization(t.Context(), privateParent, tooPublic, owner)
	require.ErrorAs(t, err, &subgroupErr)
	assert.Equal(t, organization.SubgroupCodeVisibility, subgroupErr.Code)
}

func TestAncestorOrganizationOwner(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	parent := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	member := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	child := &organization.Organization{Name: "nestedorg", Visibility: structs.VisibleTypePrivate}
	require.NoError(t, organization.CreateSubOrganization(t.Context(), parent, child, owner))

	// Drop the creator's direct membership. Access must still come from the parent owner team.
	_, err := db.GetEngine(t.Context()).Where("org_id = ? AND uid = ?", child.ID, owner.ID).Delete(new(organization.OrgUser))
	require.NoError(t, err)
	_, err = db.GetEngine(t.Context()).Where("org_id = ? AND uid = ?", child.ID, owner.ID).Delete(new(organization.TeamUser))
	require.NoError(t, err)

	isOwner, err := organization.IsOrganizationOwner(t.Context(), child.ID, owner.ID)
	require.NoError(t, err)
	assert.True(t, isOwner)

	isOwner, err = organization.IsOrganizationOwner(t.Context(), child.ID, member.ID)
	require.NoError(t, err)
	assert.False(t, isOwner)

	assert.True(t, organization.HasOrgOrUserVisible(t.Context(), child.AsUser(), owner))
	assert.False(t, organization.HasOrgOrUserVisible(t.Context(), child.AsUser(), member))

	grandchild := &organization.Organization{Name: "grandchild", Visibility: structs.VisibleTypePrivate}
	require.NoError(t, organization.CreateSubOrganization(t.Context(), child, grandchild, owner))
	assert.Equal(t, "org3/nestedorg/grandchild", grandchild.RoutePath)

	resolvedOwner, repoName, err := organization.ResolveNestedRepo(t.Context(), parent.AsUser(), "nestedorg/grandchild/demo")
	require.NoError(t, err)
	assert.Equal(t, grandchild.ID, resolvedOwner.ID)
	assert.Equal(t, "demo", repoName)

	rewritten, ok := organization.RewriteNestedPath(t.Context(), "/org3/nestedorg/grandchild/demo.git/info/refs")
	assert.True(t, ok)
	assert.Equal(t, "/grandchild/demo.git/info/refs", rewritten)
}

func TestSubgroupDepthLimit(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	current := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	for i := 1; i <= organization.MaxSubgroupDepth; i++ {
		next := &organization.Organization{
			Name:       "level" + string(rune('a'+i-1)),
			Visibility: structs.VisibleTypePublic,
		}
		require.NoError(t, organization.CreateSubOrganization(t.Context(), current, next, owner), "level %d", i)
		current = next
	}
	overflow := &organization.Organization{Name: "leveloverflow", Visibility: structs.VisibleTypePublic}
	err := organization.CreateSubOrganization(t.Context(), current, overflow, owner)
	var subgroupErr organization.ErrSubgroup
	require.ErrorAs(t, err, &subgroupErr)
	assert.Equal(t, organization.SubgroupCodeDepth, subgroupErr.Code)
}
