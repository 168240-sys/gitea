// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package organization

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"gitea.dev/models/db"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/structs"
	"gitea.dev/modules/util"

	"xorm.io/builder"
)

// MaxSubgroupDepth is the number of subgroup levels allowed below a top-level organization.
// Five covers department/project/component trees. It can be raised later without a schema change.
const MaxSubgroupDepth = 5

// ErrSubgroup is a rejected subgroup create, move, or visibility change.
type ErrSubgroup struct {
	Code string
}

func (e ErrSubgroup) Error() string {
	return "subgroup: " + e.Code
}

// IsErrSubgroup reports whether err is an ErrSubgroup.
func IsErrSubgroup(err error) bool {
	_, ok := err.(ErrSubgroup)
	return ok
}

const (
	SubgroupCodeDepth        = "depth"
	SubgroupCodeVisibility   = "visibility"
	SubgroupCodeRepoConflict = "repo_conflict"
	SubgroupCodeHasChildren  = "has_children"
	SubgroupCodeNotOrg       = "not_organization"
	SubgroupCodeForbidden    = "forbidden"
	SubgroupCodeCycle        = "cycle"
	SubgroupCodeNameTaken    = "name_taken"
)

// APIMessage is a stable English message for the API.
func (e ErrSubgroup) APIMessage() string {
	switch e.Code {
	case SubgroupCodeDepth:
		return fmt.Sprintf("subgroup nesting is limited to %d levels", MaxSubgroupDepth)
	case SubgroupCodeVisibility:
		return "subgroup visibility cannot be more public than its parent, and a parent cannot be more private than its subgroups"
	case SubgroupCodeRepoConflict:
		return "an organization cannot have a repository and a subgroup with the same name"
	case SubgroupCodeHasChildren:
		return "delete subgroups before deleting this organization"
	case SubgroupCodeNotOrg:
		return "subgroups can only be created inside an organization"
	case SubgroupCodeForbidden:
		return "only organization owners can move or create subgroups"
	case SubgroupCodeCycle:
		return "an organization cannot be moved under itself or one of its subgroups"
	case SubgroupCodeNameTaken:
		return "the destination already has an organization or repository with that name"
	default:
		return "invalid subgroup"
	}
}

// RoutePathOrName returns the stored public path, or the lower name when it was never backfilled.
func RoutePathOrName(u *user_model.User) string {
	if u.RoutePath != "" {
		return u.RoutePath
	}
	return u.LowerName
}

// SubgroupDepth returns how many parents an organization has. Top-level organizations are depth 0.
func SubgroupDepth(ctx context.Context, orgID int64) (int, error) {
	depth := 0
	seen := map[int64]struct{}{}
	for orgID > 0 {
		if _, dup := seen[orgID]; dup || depth > MaxSubgroupDepth+1 {
			return 0, fmt.Errorf("SubgroupDepth: cycle at organization %d", orgID)
		}
		seen[orgID] = struct{}{}
		var row struct {
			ParentID int64
		}
		has, err := db.GetEngine(ctx).Table("user").Where("id = ?", orgID).Cols("parent_id").Get(&row)
		if err != nil {
			return 0, fmt.Errorf("SubgroupDepth: %w", err)
		}
		if !has || row.ParentID == 0 {
			return depth, nil
		}
		depth++
		orgID = row.ParentID
	}
	return depth, nil
}

func visibilityAllowsChild(parent, child structs.VisibleType) bool {
	// Public=0, Limited=1, Private=2. A child may only be equally or more private.
	return child >= parent
}

// CreateSubOrganization creates a child organization under parent.
// The subgroup is a real organization: it has its own teams, members, and repositories.
// The local name only has to be unique among this parent's subgroups and repositories.
func CreateSubOrganization(ctx context.Context, parent, org *Organization, doer *user_model.User) error {
	if parent == nil || !parent.AsUser().IsOrganization() {
		return ErrSubgroup{Code: SubgroupCodeNotOrg}
	}
	if doer == nil {
		return ErrSubgroup{Code: SubgroupCodeForbidden}
	}
	if !doer.IsAdmin {
		ok, err := IsOrganizationOwner(ctx, parent.ID, doer.ID)
		if err != nil {
			return fmt.Errorf("CreateSubOrganization: %w", err)
		}
		if !ok {
			return ErrSubgroup{Code: SubgroupCodeForbidden}
		}
	}
	if !visibilityAllowsChild(parent.Visibility, org.Visibility) {
		return ErrSubgroup{Code: SubgroupCodeVisibility}
	}

	depth, err := SubgroupDepth(ctx, parent.ID)
	if err != nil {
		return fmt.Errorf("CreateSubOrganization: %w", err)
	}
	if depth+1 > MaxSubgroupDepth {
		return ErrSubgroup{Code: SubgroupCodeDepth}
	}

	slug := org.Name
	if conflict, err := repositoryNameTaken(ctx, parent.ID, slug); err != nil {
		return fmt.Errorf("CreateSubOrganization: %w", err)
	} else if conflict {
		return ErrSubgroup{Code: SubgroupCodeRepoConflict}
	}

	org.ParentID = parent.ID
	org.RoutePath = RoutePathOrName(parent.AsUser()) + "/" + strings.ToLower(slug)
	if err := CreateOrganization(ctx, org, doer); err != nil {
		return err
	}
	invalidateParentNameCache()
	return nil
}

// SiblingLowerNameTaken reports whether parentID already has a child with name, ignoring exceptID.
func SiblingLowerNameTaken(ctx context.Context, parentID, exceptID int64, name string) (bool, error) {
	if parentID < 0 || name == "" {
		return false, nil
	}
	return db.GetEngine(ctx).Table("user").
		Where("parent_id = ? AND lower_name = ? AND id != ?", parentID, strings.ToLower(name), exceptID).
		Exist(new(struct {
			ID int64
		}))
}

// RepoNameConflicts reports whether ownerID has a repository named name.
func RepoNameConflicts(ctx context.Context, ownerID int64, name string) (bool, error) {
	return repositoryNameTaken(ctx, ownerID, name)
}

func repositoryNameTaken(ctx context.Context, ownerID int64, name string) (bool, error) {
	return db.GetEngine(ctx).Table("repository").
		Where("owner_id = ? AND lower_name = ?", ownerID, strings.ToLower(name)).
		Exist(new(struct {
			ID int64
		}))
}

// HasChildOrg reports whether ownerID has a subgroup with name.
func HasChildOrg(ctx context.Context, ownerID int64, name string) (bool, error) {
	if ownerID <= 0 || name == "" {
		return false, nil
	}
	return db.GetEngine(ctx).Table("user").
		Where("parent_id = ? AND lower_name = ? AND type = ?", ownerID, strings.ToLower(name), user_model.UserTypeOrganization).
		Exist(new(struct {
			ID int64
		}))
}

// GetChildOrg returns the subgroup of parentID named name.
func GetChildOrg(ctx context.Context, parentID int64, name string) (*Organization, error) {
	if parentID <= 0 || name == "" {
		return nil, ErrOrgNotExist{Name: name}
	}
	org, has, err := db.Get[Organization](ctx, builder.Eq{
		"parent_id":  parentID,
		"lower_name": strings.ToLower(name),
		"`type`":     user_model.UserTypeOrganization,
	})
	if err != nil {
		return nil, fmt.Errorf("GetChildOrg: %w", err)
	}
	if !has {
		return nil, ErrOrgNotExist{Name: name}
	}
	return org, nil
}

// ListChildOrgs returns direct subgroups of parentID, ordered by name.
func ListChildOrgs(ctx context.Context, parentID int64) ([]*Organization, error) {
	orgs := make([]*Organization, 0, 8)
	err := db.GetEngine(ctx).
		Where("parent_id = ? AND type = ?", parentID, user_model.UserTypeOrganization).
		OrderBy("lower_name ASC").
		Find(&orgs)
	if err != nil {
		return nil, fmt.Errorf("ListChildOrgs: %w", err)
	}
	return orgs, nil
}

// ListVisibleChildOrgs returns subgroups the doer is allowed to see.
func ListVisibleChildOrgs(ctx context.Context, parentID int64, doer *user_model.User) ([]*Organization, error) {
	orgs, err := ListChildOrgs(ctx, parentID)
	if err != nil {
		return nil, err
	}
	visible := make([]*Organization, 0, len(orgs))
	for _, org := range orgs {
		if HasOrgOrUserVisible(ctx, org.AsUser(), doer) {
			visible = append(visible, org)
		}
	}
	return visible, nil
}

// HasChildOrgs reports whether the organization has any subgroups.
func HasChildOrgs(ctx context.Context, orgID int64) (bool, error) {
	return db.GetEngine(ctx).Table("user").
		Where("parent_id = ? AND type = ?", orgID, user_model.UserTypeOrganization).
		Exist(new(struct {
			ID int64
		}))
}

// CheckVisibilityChange rejects a visibility that would make a child more public than its parent
// or a parent more private than one of its subgroups.
func CheckVisibilityChange(ctx context.Context, org *Organization, next structs.VisibleType) error {
	if org.ParentID > 0 {
		parent, err := GetOrgByID(ctx, org.ParentID)
		if err != nil {
			return fmt.Errorf("CheckVisibilityChange: %w", err)
		}
		if !visibilityAllowsChild(parent.Visibility, next) {
			return ErrSubgroup{Code: SubgroupCodeVisibility}
		}
	}
	children, err := ListChildOrgs(ctx, org.ID)
	if err != nil {
		return fmt.Errorf("CheckVisibilityChange: %w", err)
	}
	for _, child := range children {
		if !visibilityAllowsChild(next, child.Visibility) {
			return ErrSubgroup{Code: SubgroupCodeVisibility}
		}
	}
	return nil
}

// RefreshRoutePaths updates this organization's public path and every descendant after a rename or move.
// Previous nested paths are recorded so HTTP and SSH can redirect them.
func RefreshRoutePaths(ctx context.Context, u *user_model.User) error {
	old := RoutePathOrName(u)
	if u.ParentID == 0 {
		u.RoutePath = u.LowerName
	} else {
		parent, err := user_model.GetUserByID(ctx, u.ParentID)
		if err != nil {
			return fmt.Errorf("RefreshRoutePaths: %w", err)
		}
		u.RoutePath = RoutePathOrName(parent) + "/" + u.LowerName
	}
	if err := user_model.UpdateUserCols(ctx, u, "route_path"); err != nil {
		return fmt.Errorf("RefreshRoutePaths: %w", err)
	}
	if err := clearOrgRouteRedirect(ctx, u.RoutePath); err != nil {
		return fmt.Errorf("RefreshRoutePaths: %w", err)
	}
	if old != u.RoutePath {
		if err := recordOrgRouteRedirect(ctx, old, u.ID); err != nil {
			return fmt.Errorf("RefreshRoutePaths: %w", err)
		}
	}
	children, err := ListChildOrgs(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("RefreshRoutePaths: %w", err)
	}
	for _, child := range children {
		if err := RefreshRoutePaths(ctx, child.AsUser()); err != nil {
			return err
		}
	}
	invalidateParentNameCache()
	return nil
}

// MoveSubOrganization moves org under newParent. A nil parent makes org top-level.
// The disk name stays put, so existing git directories and flat URLs that use it keep working.
// The old nested route redirects to the new one.
func MoveSubOrganization(ctx context.Context, org, newParent *Organization, doer *user_model.User) error {
	if org == nil || !org.AsUser().IsOrganization() {
		return ErrSubgroup{Code: SubgroupCodeNotOrg}
	}
	if doer == nil {
		return ErrSubgroup{Code: SubgroupCodeForbidden}
	}
	if newParent != nil && !newParent.AsUser().IsOrganization() {
		return ErrSubgroup{Code: SubgroupCodeNotOrg}
	}
	if !doer.IsAdmin {
		ok, err := IsOrganizationOwner(ctx, org.ID, doer.ID)
		if err != nil {
			return fmt.Errorf("MoveSubOrganization: %w", err)
		}
		if !ok {
			return ErrSubgroup{Code: SubgroupCodeForbidden}
		}
		if newParent != nil {
			ok, err = IsOrganizationOwner(ctx, newParent.ID, doer.ID)
			if err != nil {
				return fmt.Errorf("MoveSubOrganization: %w", err)
			}
			if !ok {
				return ErrSubgroup{Code: SubgroupCodeForbidden}
			}
		}
	}

	newParentID := int64(0)
	if newParent != nil {
		if newParent.ID == org.ID {
			return ErrSubgroup{Code: SubgroupCodeCycle}
		}
		cycle, err := parentChainContains(ctx, newParent.ID, org.ID)
		if err != nil {
			return fmt.Errorf("MoveSubOrganization: %w", err)
		}
		if cycle {
			return ErrSubgroup{Code: SubgroupCodeCycle}
		}
		if !visibilityAllowsChild(newParent.Visibility, org.Visibility) {
			return ErrSubgroup{Code: SubgroupCodeVisibility}
		}
		if conflict, err := repositoryNameTaken(ctx, newParent.ID, org.Name); err != nil {
			return fmt.Errorf("MoveSubOrganization: %w", err)
		} else if conflict {
			return ErrSubgroup{Code: SubgroupCodeRepoConflict}
		}
		taken, err := SiblingLowerNameTaken(ctx, newParent.ID, org.ID, org.LowerName)
		if err != nil {
			return fmt.Errorf("MoveSubOrganization: %w", err)
		} else if taken {
			return ErrSubgroup{Code: SubgroupCodeNameTaken}
		}
		newParentID = newParent.ID
	} else if exist, err := user_model.IsUserExist(ctx, org.ID, org.Name); err != nil {
		return fmt.Errorf("MoveSubOrganization: %w", err)
	} else if exist {
		return ErrSubgroup{Code: SubgroupCodeNameTaken}
	}

	newDepth := 0
	if newParent != nil {
		depth, err := SubgroupDepth(ctx, newParent.ID)
		if err != nil {
			return fmt.Errorf("MoveSubOrganization: %w", err)
		}
		newDepth = depth + 1
	}
	height, err := subtreeHeight(ctx, org.ID)
	if err != nil {
		return fmt.Errorf("MoveSubOrganization: %w", err)
	}
	if newDepth+height > MaxSubgroupDepth {
		return ErrSubgroup{Code: SubgroupCodeDepth}
	}
	if newParentID == org.ParentID {
		return nil
	}

	org.ParentID = newParentID
	org.NamespaceKey = user_model.NamespaceKeyFor(newParentID, org.LowerName)
	if err := user_model.UpdateUserCols(ctx, org.AsUser(), "parent_id", "namespace_key"); err != nil {
		return fmt.Errorf("MoveSubOrganization: %w", err)
	}
	if err := RefreshRoutePaths(ctx, org.AsUser()); err != nil {
		return fmt.Errorf("MoveSubOrganization: %w", err)
	}
	return nil
}

func parentChainContains(ctx context.Context, startID, targetID int64) (bool, error) {
	seen := map[int64]struct{}{}
	id := startID
	for id > 0 && len(seen) <= MaxSubgroupDepth+1 {
		if id == targetID {
			return true, nil
		}
		if _, dup := seen[id]; dup {
			return true, fmt.Errorf("parentChainContains: cycle at organization %d", id)
		}
		seen[id] = struct{}{}
		org, err := GetOrgByID(ctx, id)
		if err != nil {
			return false, err
		}
		id = org.ParentID
	}
	return false, nil
}

func subtreeHeight(ctx context.Context, orgID int64) (int, error) {
	children, err := ListChildOrgs(ctx, orgID)
	if err != nil {
		return 0, err
	}
	height := 0
	for _, child := range children {
		h, err := subtreeHeight(ctx, child.ID)
		if err != nil {
			return 0, err
		}
		height = max(height, h+1)
	}
	return height, nil
}

// DescendantOrgIDs returns orgID and every descendant organization, depth-capped.
func DescendantOrgIDs(ctx context.Context, orgID int64) ([]int64, error) {
	if orgID <= 0 {
		return nil, nil
	}
	ids := []int64{orgID}
	frontier := []int64{orgID}
	for depth := 0; depth < MaxSubgroupDepth && len(frontier) > 0; depth++ {
		next := make([]int64, 0, 4)
		for _, id := range frontier {
			children, err := ListChildOrgs(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("DescendantOrgIDs: %w", err)
			}
			for _, child := range children {
				ids = append(ids, child.ID)
				next = append(next, child.ID)
			}
		}
		frontier = next
	}
	return ids, nil
}

// MentionLink returns a root-relative link for a visible organization path or org/team path.
// An empty result means the caller should not link it (unknown, or not visible).
func MentionLink(ctx context.Context, path string, doer *user_model.User) string {
	path = strings.Trim(strings.ToLower(path), "/")
	if path == "" || strings.Contains(path, "..") {
		return ""
	}
	if org, err := GetOrgByRoutePath(ctx, path); err == nil {
		if !HasOrgOrUserVisible(ctx, org.AsUser(), doer) {
			return ""
		}
		return "/" + util.PathEscapeSegments(RoutePathOrName(org.AsUser()))
	}
	slash := strings.LastIndex(path, "/")
	if slash <= 0 {
		return ""
	}
	org, err := GetOrgByRoutePath(ctx, path[:slash])
	if err != nil {
		return ""
	}
	teamName := path[slash+1:]
	if teamName == "" || !HasOrgOrUserVisible(ctx, org.AsUser(), doer) {
		return ""
	}
	team, err := org.GetTeam(ctx, teamName)
	if err != nil {
		return ""
	}
	return "/org/" + url.PathEscape(org.AsUser().RouterName()) + "/teams/" + url.PathEscape(team.LowerName)
}

// parent-name cache avoids a database lookup on ordinary /owner/repo requests.
// It is process-local; a short TTL picks up subgroups created on another instance.
const parentNameCacheTTL = 30 * time.Second

type parentNameCache struct {
	names   map[string]struct{}
	expires time.Time
}

var (
	parentNamesMu sync.Mutex
	parentNames   *parentNameCache
)

func invalidateParentNameCache() {
	parentNamesMu.Lock()
	parentNames = nil
	parentNamesMu.Unlock()
}

// HasAnySubgroup reports whether this instance has at least one subgroup.
// The answer is cached so permission checks stay free while the feature is unused.
func HasAnySubgroup(ctx context.Context) bool {
	names, err := cachedParentLowerNames(ctx)
	return err == nil && len(names) > 0
}

func parentLowerNameSet(ctx context.Context) (map[string]struct{}, error) {
	return cachedParentLowerNames(ctx)
}

func cachedParentLowerNames(ctx context.Context) (map[string]struct{}, error) {
	parentNamesMu.Lock()
	defer parentNamesMu.Unlock()
	if parentNames != nil && time.Now().Before(parentNames.expires) {
		return parentNames.names, nil
	}
	names, err := loadParentLowerNames(ctx)
	if err != nil {
		return nil, err
	}
	parentNames = &parentNameCache{names: names, expires: time.Now().Add(parentNameCacheTTL)}
	return names, nil
}

type parentNameRow struct {
	LowerName string
}

func loadParentLowerNames(ctx context.Context) (map[string]struct{}, error) {
	rows := make([]parentNameRow, 0, 8)
	err := db.GetEngine(ctx).
		Table("user").
		In("id", builder.Select("parent_id").From("`user`").Where(builder.Gt{"parent_id": 0})).
		Cols("lower_name").
		Find(&rows)
	if err != nil {
		return nil, fmt.Errorf("loadParentLowerNames: %w", err)
	}
	names := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.LowerName != "" {
			names[row.LowerName] = struct{}{}
		}
	}
	return names, nil
}
