// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package organization

import (
	"context"
	"net/http"
	"strings"

	"gitea.dev/models/db"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
)

// RewriteRequestPath rewrites a nested organization URL onto the owning organization's
// flat name so the existing /{owner}/{repo} routes keep working.
//
//	/root/child/repo/issues -> /child/repo/issues
//	/root/repo/issues       -> unchanged when "repo" is a repository of root
//
// Existing repository paths win over a subgroup of the same name. "/{owner}/-/" routes
// (packages, projects) stay attached to the organization that owns that prefix.
func RewriteRequestPath(req *http.Request) {
	if req == nil || req.URL == nil || !setting.InstallLock {
		return
	}
	rewritten, ok := RewriteNestedPath(req.Context(), req.URL.Path)
	if !ok || rewritten == req.URL.Path {
		return
	}
	req.URL.Path = rewritten
	req.URL.RawPath = ""
}

// RewriteNestedPath resolves path. ok is false when the path is left unchanged.
func RewriteNestedPath(ctx context.Context, urlPath string) (string, bool) {
	if urlPath == "" || !strings.HasPrefix(urlPath, "/") {
		return urlPath, false
	}
	trimmed := strings.Trim(urlPath, "/")
	if trimmed == "" {
		return urlPath, false
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) < 2 || skipNestedRewrite(segments[0]) {
		return urlPath, false
	}

	names, err := parentLowerNameSet(ctx)
	if err != nil {
		log.Error("RewriteNestedPath: %v", err)
		return urlPath, false
	}
	if _, isParent := names[strings.ToLower(segments[0])]; !isParent {
		return urlPath, false
	}

	rewritten, ok, err := rewriteNestedSegments(ctx, segments)
	if err != nil {
		log.Error("RewriteNestedPath: %v", err)
		return urlPath, false
	}
	if !ok {
		return urlPath, false
	}
	return "/" + strings.Join(rewritten, "/"), true
}

func skipNestedRewrite(first string) bool {
	switch strings.ToLower(first) {
	case "api", "assets", "avatars", "repo-avatars", "attachments", "user", "org", "repo",
		"explore", "issues", "pulls", "notifications", "login", "metrics", "v2", "admin":
		return true
	default:
		return false
	}
}

func rewriteNestedSegments(ctx context.Context, segments []string) ([]string, bool, error) {
	current, err := user_model.GetUserByName(ctx, segments[0])
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !current.IsOrganization() {
		return nil, false, nil
	}

	descended := false
	for i := 1; i < len(segments); i++ {
		seg := segments[i]
		if seg == "-" {
			if !descended {
				return nil, false, nil
			}
			return append([]string{current.Name, "-"}, segments[i+1:]...), true, nil
		}
		if repoSegmentExists(ctx, current.ID, seg) {
			if !descended {
				return nil, false, nil
			}
			return append([]string{current.Name}, segments[i:]...), true, nil
		}
		child, err := GetChildOrg(ctx, current.ID, seg)
		if err != nil {
			if IsErrOrgNotExist(err) {
				if !descended {
					return nil, false, nil
				}
				return append([]string{current.Name}, segments[i:]...), true, nil
			}
			return nil, false, err
		}
		current = child.AsUser()
		descended = true
	}
	if !descended {
		return nil, false, nil
	}
	return []string{current.Name}, true, nil
}

func repoSegmentExists(ctx context.Context, ownerID int64, segment string) bool {
	name := segment
	name = strings.TrimSuffix(name, ".git")
	name = strings.TrimSuffix(name, ".wiki")
	if name == "" {
		return false
	}
	has, err := db.GetEngine(ctx).Table("repository").
		Where("owner_id = ? AND lower_name = ?", ownerID, strings.ToLower(name)).
		Exist(new(struct {
			ID int64
		}))
	if err != nil {
		log.Error("repoSegmentExists: %v", err)
		return false
	}
	return has
}

// ResolveNestedRepo walks subgroup segments in rest ("child/repo" or "child/nested/repo")
// and returns the organization that owns the repository plus the repository name.
func ResolveNestedRepo(ctx context.Context, root *user_model.User, rest string) (*user_model.User, string, error) {
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || root == nil {
		return root, rest, nil
	}
	current := root
	for _, seg := range parts[:len(parts)-1] {
		child, err := GetChildOrg(ctx, current.ID, seg)
		if err != nil {
			return nil, "", err
		}
		current = child.AsUser()
	}
	return current, parts[len(parts)-1], nil
}
