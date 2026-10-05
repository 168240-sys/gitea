# Nested organizations (subgroups)

A subgroup is a real organization whose parent is another organization. It is not a second permission system. This is the implementation for [go-gitea/gitea#1872](https://github.com/go-gitea/gitea/issues/1872).

## Model

- `user.parent_id` is 0 for users and top-level organizations. `user.route_path` is the public path (`root/child`, lower case). Migration `358` backfills `route_path = lower_name`.
- `user.lower_name` and `user.name` are indexed, not globally unique. Migration `359` adds:
  - `disk_name`, globally unique. This is the git directory and the chi username. Roots keep `disk_name = lower_name`, so existing paths and clone URLs do not move.
  - `namespace_key` (`{parentID}/{lowerName}`), globally unique. Logins and sibling names stay unique. Two subgroups may share a local name when their parents differ.
- A new subgroup whose lower name is already a disk name is stored as `subgroup-{id}` (or `subgroup-{id}-x` if that is taken). It is reached at `/parent/name/...`. The flat URL of an unambiguous name still works.
- `GetUserByName` prefers the `parent_id = 0` row, otherwise the only match, otherwise `disk_name`. Login uses root individuals only, so a subgroup cannot take over a password login.
- `org_route_redirect` (migration `360`) stores previous nested paths. A move or subgroup rename keeps `disk_name` and 301s the old route. Git over SSH follows the same table.

## Routing and storage

Public URLs such as `/root/child/repo` are rewritten onto `disk_name` before chi. An existing repository segment wins over a subgroup of the same name. `/root/child/-/packages` stays on that organization.

`HomeLink` stays one segment (`disk_name` when it differs from the local name) so `/{name}/-/` handlers do not redirect-loop. Package and project links use `ScopedLink`, which is the nested route.

Repository files stay `{diskName}/{repo}.git`. A rename renames the directory only when `disk_name` changes.

## Permissions

The highest grant wins. Nothing denies a grant.

- Ancestor organization owners are owners of every descendant.
- A parent team grants access to descendant repositories only when `includes_all_repositories` is set. Teams that list specific repositories do not propagate. The same rule is in the repository list queries, up to five ancestor levels.
- Secrets and Actions variables do not propagate. A subgroup can have members who are not in the parent team that created the secret.
- An organization-level Actions runner picks jobs from that organization and its descendants. Repository runners stay on one repository.

Nesting stops at five levels. A child cannot be more public than its parent. An organization with subgroups cannot be deleted. A parent cannot have a repository and a subgroup with the same name. Moving rejects cycles, depth overflows, and name clashes. Owners of the organization and of the new parent may move it (ancestor owners count).

## API and UI

- `GET`/`POST /api/v1/orgs/{org}/subgroups`
- `GET /api/v1/orgs/{org}/subgroups/{subgroup}`
- `POST /api/v1/orgs/{org}/subgroups/{subgroup}/move` and `POST /api/v1/orgs/{org}/move`
- Web: create on the org home, move from the organization settings danger zone.

Mentions accept `@root/child` and `@root/child/team`. Issue references accept `root/child/repo#1`. A full issue URL with extra path segments keeps the whole owner path. Private organizations are not linked for a viewer who cannot see them.

## Not in this change

- Code-preview URLs (`/.../src/commit/...`) still treat the owner as a single path segment.
- The first commit on this branch has no `Signed-off-by` line. Later commits include `Signed-off-by: Cursor Agent <cursoragent@cursor.com>`. Upstream may want a human contributor's DCO identity instead.
