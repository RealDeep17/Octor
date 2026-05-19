# Admin Universal Viewing Plan

Branch: `WebDav-Web-UI_Universal_Viewing-Admin`
Base branch: `linux-vps`

## Important Git Context

The original `/srv/octor` checkout had dirty work on `expermintal-R&D`. Those changes were not discarded or committed. They are parked in:

```text
stash@{0}: On expermintal-R&D: r-and-d-dirty-before-webdav-admin-branch
```

Do not drop that stash unless the owner explicitly says so.

## Mental Model

Keep these concepts separate:

```text
Vault   = S3 storage backend, currently through rclone union -> Google Drive drives.
Library = Webtor user library DB rows.
WebDAV  = a library/torrent/media mapping exposed as folders.
Admin   = a real Webtor user account with elevated cross-user visibility.
```

Current WebDAV is not Vault. It maps the logged-in/token user library into:

```text
torrents/
movies/
series/
all/
```

The planned admin WebDAV should also be library-based, not raw Vault browsing.

## User Requirements Captured

- Admin is a normal user too.
- Admin's own library must appear under the per-user admin tree, for example:

```text
admin/users/admin@example.com/torrents
admin/users/admin@example.com/movies
admin/users/admin@example.com/tvseries
admin/users/admin@example.com/all
```

- Admin Webtor UI must show:
  - all users' library
  - all users' vault pledges/resources
  - filter/select by user
- Admin WebDAV should show all users' library mappings:

```text
admin/
  torrents/
  movies/
  tvseries/
  all/
  users/
    user1@example.com/
      torrents/
      movies/
      tvseries/
      all/
    user2@example.com/
      torrents/
      movies/
      tvseries/
      all/
```

- Do not add a raw WebDAV `vault/` folder for now.
- Existing normal-user WebDAV should remain unchanged.
- Current `/all` folder should mean current user's full library media. If it is empty, investigate/fix.
- Global admin folders should be deduped where practical. First acceptable dedupe key:
  - `resource_id` for torrents/all
  - movie/series metadata video ID later if needed
- Do not rename or reorganize real Vault object keys. Keep:

```text
vault/<file_hash>
```

because the worker, webseed, GC, integrity verification, delete, and dedupe logic depend on hash-keyed objects.

## Work Already Started In This Branch

### Admin Identity Service

New file:

```text
web-ui/services/admin/admin.go
```

Adds:

```text
ADMIN_EMAILS=admin@example.com,other@example.com
```

and helper methods:

```go
IsAdminEmail(email string) bool
IsAdminUser(u *auth.User) bool
HasAdmin(c *gin.Context) bool
Require() gin.HandlerFunc
```

### Server Wiring

`web-ui/serve.go` has been modified to:

- register `ADMIN_EMAILS`
- instantiate `adminSvc := admins.New(c)`
- register admin UI handler
- pass admin service to WebDAV handler

### Admin UI Handler

New package started:

```text
web-ui/handlers/admin/handler.go
```

Planned routes:

```text
/admin
/admin/library
/admin/library/movies
/admin/library/series
/admin/vault
```

Current implementation is Webtor-style: the admin library keeps the normal library sections, normal torrent rows, movie/series poster cards, owner badges, and a user selector. It is still routed under `/admin/*` so normal `/lib` and `/vault` behavior stays untouched.

### Admin UI Templates

New templates started:

```text
web-ui/templates/views/admin/library.html
web-ui/templates/views/admin/vault.html
```

They are normal Webtor-style views with user selectors. `admin/library.html` uses library-like torrent rows and media poster cards; `admin/vault.html` uses vault-like resource rows with owner context.

### Library Model Owner Relation

`web-ui/models/library.go` now adds:

```go
User *User `pg:"rel:has-one,fk:user_id"`
```

This lets admin UI show owner email.

### All-User Library Query Helpers

`web-ui/models/library.go` now has draft helpers:

```go
GetLibraryByNameAny(...)
GetLibraryByTorrentNameAny(...)
GetLibraryTorrentsListAll(...)
GetLibraryMovieTorrentListAll(...)
GetLibrarySeriesTorrentListAll(...)
```

These are intended for admin WebDAV global views and dedupe by `library.resource_id`.

### WebDAV User Scoping Started

`web-ui/handlers/webdav/content.go` and `web-ui/handlers/webdav/torrent_lib.go` are being modified so existing WebDAV directory types can optionally use a fixed `UserID` instead of the token owner.

This is needed for:

```text
admin/users/{email}/movies
admin/users/{email}/tvseries
admin/users/{email}/torrents
admin/users/{email}/all
```

`TorrentLibraryDirectory` also has an `AllUsers` flag started for read-only global admin folders.

## Checklist

### 1. Finish Compile-Safe Admin Service Wiring

- [x] Ensure `web-ui/services/admin/admin.go` is tracked and formatted.
- [x] Ensure `web-ui/serve.go` imports compile.
- [x] Ensure `web-ui/handlers/admin/handler.go` does not keep unused fields/imports.
- [ ] Decide whether admin link should appear in nav/profile for admin users. Low priority.

### 2. Finish Admin Webtor UI

- [x] Verify `/admin/library` compiles for all users' torrents.
- [x] Verify `/admin/library/movies` compiles with real movie metadata/poster-card items.
- [x] Verify `/admin/library/series` compiles with real series metadata/poster-card items.
- [x] Verify `?user=<uuid>` is wired for library and vault filters.
- [x] Verify `/admin/vault` compiles loading pledges with `User` and `Resource` relations.
- [x] Verify `/admin/vault?user=<uuid>` is wired to filter vault by user.
- [x] Make owner email visible on every admin row.
- [x] Keep normal `/lib` and `/vault` behavior unchanged in routing; admin views live under `/admin/*`.

### 3. Finish Admin WebDAV

- [x] Update `web-ui/handlers/webdav/handler.go` signature to accept admin service.
- [x] Update `web-ui/handlers/webdav/fs.go` signature to accept admin service.
- [x] Add admin-aware root filtering so normal users do not see/use `admin/`.
- [x] Add `AdminDirectory` for:

```text
admin/torrents
admin/movies
admin/tvseries
admin/all
admin/users
```

- [x] Add `AdminUsersDirectory` that lists all users by email.
- [x] Add per-user nested WebDAV roots using fixed `UserID`.
- [x] Use `tvseries` in admin WebDAV, even though current normal WebDAV uses `series`.
- [x] Keep existing normal-user WebDAV folders unchanged:

```text
torrents
movies
series
all
```

- [x] Make global admin folders read-only unless owner explicitly asks for writes.
- [x] Deduplicate global admin folders by `resource_id` first.

### 4. Investigate Current `/all` Empty Folder

The current code says `/all` should expose current user's full library media:

```go
"all": &ContentDirectory{Library: &AllLibrary{}, ...}
```

Investigation checklist:

- [ ] Confirm the WebDAV access token belongs to the expected user.
- [ ] Confirm that user's `library` rows exist.
- [ ] Confirm `models.GetLibraryTorrentsList(ctx, db, userID, models.SortTypeName)` returns rows.
- [ ] Confirm `ContentDirectory.ReadDir(ctx, "/", ...)` gets those rows.
- [ ] Check whether `ContentDirectory.getContentItem` lookup by `torrent_resource.name` mismatches visible root names.
- [ ] Check WebDAV client cache/stale PROPFIND result.

### 5. Add Infra-Data/GDrive Index Later

Separate from WebDAV/admin UI. Safe generated files could live under:

```text
infra-data/index/users.json
infra-data/index/library.json
infra-data/index/vault.json
infra-data/users/{email}/library.json
infra-data/users/{email}/vault.json
infra-data/resources/{resource_id}.json
infra-data/files/{hash}.json
```

This should explain the hash-keyed Vault backend without moving or renaming real files.

### 6. Verification

Run from `/srv/octor`:

```text
/usr/bin/go test ./web-ui/...
/usr/bin/go test ./vault/...
```

If workspace/module layout blocks those exact commands, inspect `go.work` and run the narrow package tests instead.

Also verify manually:

```text
ADMIN_EMAILS=<admin email> web-ui serve ...
```

Then test:

```text
/admin/library
/admin/library/movies
/admin/library/series
/admin/vault
/profile -> generate WebDAV token -> admin WebDAV paths
```

## Notes / Risks

- Current branch is in the main `/srv/octor` checkout.
- R&D dirty state is stashed, not applied.
- Do not restore the R&D stash while staying on this admin branch unless the owner explicitly wants to merge that work here.
- Global WebDAV naming can collide if two different resources have the same torrent name. Dedupe by `resource_id` helps, but display path collision still needs careful handling.
- Full movie/series dedupe by metadata video ID can be added later; it is more complex than resource-level dedupe.


## Current Implementation Status

Updated: 2026-05-19

- [x] `custom.env` contains:

```text
ADMIN_EMAILS=shudeepan@gmail.com,admin@mail.com,admin2@mail.com
```

- [x] Admin identity is configured by `ADMIN_EMAILS`.
- [x] Admin Webtor UI is implemented as normal Webtor-style views, not a separate ops dashboard:
  - `/admin/library`
  - `/admin/library/movies`
  - `/admin/library/series`
  - `/admin/vault`
- [x] Admin library has an all-users scope and per-user selector.
- [x] Admin movie/series views load real `models.Movie` / `models.Series` records with metadata, posters, years, and ratings where available.
- [x] All-users admin library is deduped at the resource level.
- [x] Owner context is visible:
  - torrents show the owner label in row metadata
  - media cards show an owner/user-count badge
- [x] Admin vault loads all users' pledges/resources and can filter by selected user.
- [x] Admin WebDAV exposes:

```text
admin/torrents
admin/movies
admin/tvseries
admin/all
admin/users/{email}/torrents
admin/users/{email}/movies
admin/users/{email}/tvseries
admin/users/{email}/all
```

- [x] Admin WebDAV global folders are read-only.
- [x] Admin WebDAV per-user folders are scoped to that user's library.
- [x] Admin WebDAV does not expose raw Vault storage.
- [x] WebDAV admin root is hidden/blocked for non-admin users.
- [x] Nested WebDAV move handling was tightened so per-user moves cannot cross between users.

## Verification Run

- [x] Focused compile:

```text
/usr/bin/go test ./web-ui/handlers/admin ./web-ui/handlers/webdav ./web-ui/services/admin ./web-ui/models
```

- [x] Full web-ui suite with existing protobuf workaround:

```text
GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn /usr/bin/go test ./web-ui/...
```

- [x] Admin service and WebDAV root tests were added and pass with the protobuf workaround where needed.

- [x] Temporary admin template parse check:

```text
cd /srv/octor/web-ui && /usr/local/go/bin/go run /tmp/check_admin_templates.go
```

## Remaining Manual QA

- [ ] Login as `admin@mail.com` or `admin2@mail.com` and browse `/admin/library`.
- [ ] Confirm admin selector shows all users and all sections render with production data.
- [ ] Confirm `/admin/vault` shows all users' vault pledges/resources.
- [ ] Mount WebDAV as an admin token and confirm `admin/` tree shape.
- [ ] Mount WebDAV as a normal token and confirm `admin/` is hidden/blocked.
- [ ] Investigate user-reported normal WebDAV `/all` empty folder with a real token and DB row sample if it still reproduces.
