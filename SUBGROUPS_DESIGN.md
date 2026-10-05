# Nested organizations (subgroups)

This is a first phase for [go-gitea/gitea#1872](https://github.com/go-gitea/gitea/issues/1872). A subgroup is a real organization whose parent is another organization. It is not a second permission system.

## What shipped

- `user.parent_id` (0 for users and top-level organizations) and `user.route_path` (`root/child`, lower case). Migration `358` backfills `route_path = lower_name`.
- Create, list, and browse subgroups on the web (`/org/{org}/subgroups/new`, org home) and API (`GET`/`POST /api/v1/orgs/{org}/subgroups`). API organization objects include `parent_id` and `route_path`.
- Public URLs such as `/root/child/repo` are rewritten onto the subgroup's unique name before routing. `/child/repo` keeps working. Git over HTTP and SSH follow the same path.
- Repository disk layout stays `{uniqueOwnerName}/{repo}.git`.
- Ancestor organization owners are owners of every descendant: they can see private subgroups and get owner access to descendant repositories. The highest grant wins. Nothing denies a grant.
- A child cannot be more public than its parent. An organization with subgroups cannot be deleted. Nesting stops at five levels below the root. A parent cannot have a repository and a subgroup with the same name.

## Why this shape

[lunny](https://github.com/go-gitea/gitea/issues/1872) asked to reuse organization rather than add a parallel group type, and to keep repository storage off the nested URL. [wxiaoguang](https://github.com/go-gitea/gitea/pull/35265) rejected integer group ids because they do not produce `/owner/group/repo`, and asked for a design before a large patch. [delvh](https://github.com/go-gitea/gitea/issues/1872) asked how teams and conflicting grants behave. [TheFox0x7](https://github.com/go-gitea/gitea/issues/1872) said existing clone URLs cannot change.

`user.lower_name` stays globally unique. Dropping that uniqueness touches login, redirects, API tokens, and every `{username}` lookup. This phase does not do that, so two subgroups cannot share a slug. The nested URL is a route alias. Existing repositories win when a path segment is both a repository and a possible subgroup, so current clone URLs stay valid.

`/-/` is already the owner-feature prefix (`/{username}/-/packages`). It is not used as a subgroup separator.

## Permissions

Teams stay on the organization that defines them. A parent team does not automatically include child-organization repositories. Ancestor owners are the inheritance that matches "owners administer the tree" without inventing deny rules or a second unit-permission table.

Direct owners still go through the existing team lookup. Only an owner who is not a direct owner of that organization takes the inherited owner path.

## Not in this phase

- The same local name under different parents (requires relaxing `lower_name` uniqueness).
- Parent teams, secrets, runners, and Actions permissions inherited by subgroups.
- Mentions, markdown links, and package URLs that assume a single path segment.
- Moving a subgroup to another parent.
- DCO sign-off. Add `Signed-off-by` on the commits before any upstream pull request. This fork's commit rule forbids that trailer here.
