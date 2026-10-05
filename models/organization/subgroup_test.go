// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package organization_test

import (
	"strings"
	"testing"

	"gitea.dev/models/db"
	"gitea.dev/models/organization"
	"gitea.dev/models/perm"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
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

func TestDuplicateSubgroupNames(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	parent := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	// user2 already owns this lower name. The subgroup keeps a different disk name.
	child := &organization.Organization{Name: "user2", Visibility: structs.VisibleTypePublic}
	require.NoError(t, organization.CreateSubOrganization(t.Context(), parent, child, owner))
	assert.True(t, strings.HasPrefix(child.DiskName, "subgroup-"))
	assert.NotEqual(t, "user2", child.AsUser().StorageName())

	login, err := user_model.GetIndividualUserByName(t.Context(), "user2")
	require.NoError(t, err)
	assert.Equal(t, owner.ID, login.ID)

	byName, err := user_model.GetUserByName(t.Context(), "user2")
	require.NoError(t, err)
	assert.Equal(t, owner.ID, byName.ID)

	byDisk, err := user_model.GetUserByName(t.Context(), child.DiskName)
	require.NoError(t, err)
	assert.Equal(t, child.ID, byDisk.ID)

	rewritten, ok := organization.RewriteNestedPath(t.Context(), "/org3/user2")
	assert.True(t, ok)
	assert.Equal(t, "/"+child.DiskName, rewritten)

	other := &organization.Organization{Name: "otherparent", Visibility: structs.VisibleTypePublic}
	require.NoError(t, organization.CreateOrganization(t.Context(), other, owner))
	twin := &organization.Organization{Name: "user2", Visibility: structs.VisibleTypePublic}
	require.NoError(t, organization.CreateSubOrganization(t.Context(), other, twin, owner))
	assert.NotEqual(t, child.ID, twin.ID)
	assert.Equal(t, "otherparent/user2", twin.RoutePath)

	// An unambiguous local name still answers on the flat URL.
	only := &organization.Organization{Name: "onlyonce", Visibility: structs.VisibleTypePublic}
	require.NoError(t, organization.CreateSubOrganization(t.Context(), parent, only, owner))
	assert.Equal(t, "onlyonce", only.DiskName)
	flat, err := user_model.GetUserByName(t.Context(), "onlyonce")
	require.NoError(t, err)
	assert.Equal(t, only.ID, flat.ID)
}

func TestMoveSubOrganization(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	parent := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	child := &organization.Organization{Name: "movedchild", Visibility: structs.VisibleTypePublic}
	require.NoError(t, organization.CreateSubOrganization(t.Context(), parent, child, owner))

	dest := &organization.Organization{Name: "destparent", Visibility: structs.VisibleTypePublic}
	require.NoError(t, organization.CreateOrganization(t.Context(), dest, owner))

	require.NoError(t, organization.MoveSubOrganization(t.Context(), child, dest, owner))
	assert.Equal(t, dest.ID, child.ParentID)
	assert.Equal(t, "destparent/movedchild", child.RoutePath)

	redirected, ok := organization.RedirectedRequestPath(t.Context(), "/org3/movedchild/repo/issues")
	assert.True(t, ok)
	assert.Equal(t, "/destparent/movedchild/repo/issues", redirected)

	resolved, repoName, err := organization.ResolveNestedRepo(t.Context(), parent.AsUser(), "movedchild/repo")
	require.NoError(t, err)
	assert.Equal(t, child.ID, resolved.ID)
	assert.Equal(t, "repo", repoName)

	err = organization.MoveSubOrganization(t.Context(), dest, child, owner)
	var subgroupErr organization.ErrSubgroup
	require.ErrorAs(t, err, &subgroupErr)
	assert.Equal(t, organization.SubgroupCodeCycle, subgroupErr.Code)
}

func TestPropagatingAncestorTeam(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	parent := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	member := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	child := &organization.Organization{Name: "childrepos", Visibility: structs.VisibleTypePrivate}
	require.NoError(t, organization.CreateSubOrganization(t.Context(), parent, child, owner))

	_, err := db.GetEngine(t.Context()).Where("org_id = ? AND uid = ?", child.ID, member.ID).Delete(new(organization.OrgUser))
	require.NoError(t, err)

	repo := &repo_model.Repository{
		OwnerID:   child.ID,
		Owner:     child.AsUser(),
		OwnerName: child.Name,
		Name:      "hidden",
		LowerName: "hidden",
		IsPrivate: true,
	}
	require.NoError(t, db.Insert(t.Context(), repo))
	repo.Owner = child.AsUser()
	repo.Units = []*repo_model.RepoUnit{{RepoID: repo.ID, Type: unit.TypeCode}}

	assert.False(t, organization.HasOrgOrUserVisible(t.Context(), child.AsUser(), member))
	p, err := access_model.GetIndividualUserRepoPermission(t.Context(), repo, member)
	require.NoError(t, err)
	assert.Equal(t, perm.AccessModeNone, p.UnitAccessMode(unit.TypeCode))

	ids, err := repo_model.SearchRepositoryIDsByCondition(t.Context(), repo_model.AccessibleRepositoryCondition(member, unit.TypeCode))
	require.NoError(t, err)
	assert.NotContains(t, ids, repo.ID)

	team := &organization.Team{
		OrgID:                   parent.ID,
		Name:                    "allrepos",
		LowerName:               "allrepos",
		AccessMode:              perm.AccessModeWrite,
		IncludesAllRepositories: true,
		Visibility:              structs.VisibleTypePrivate,
	}
	require.NoError(t, db.Insert(t.Context(), team))
	require.NoError(t, db.Insert(t.Context(), &organization.TeamUser{OrgID: parent.ID, TeamID: team.ID, UID: member.ID}))

	assert.True(t, organization.HasOrgOrUserVisible(t.Context(), child.AsUser(), member))
	p, err = access_model.GetIndividualUserRepoPermission(t.Context(), repo, member)
	require.NoError(t, err)
	assert.Equal(t, perm.AccessModeWrite, p.UnitAccessMode(unit.TypeCode))

	ids, err = repo_model.SearchRepositoryIDsByCondition(t.Context(), repo_model.AccessibleRepositoryCondition(member, unit.TypeCode))
	require.NoError(t, err)
	assert.Contains(t, ids, repo.ID)
}
