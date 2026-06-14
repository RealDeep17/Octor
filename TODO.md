# Vault & Library Improvements - To-Do List

This document organizes and tracks the implementation details for the requested vault and library changes, bug fixes, and feature additions.

---

## 1. Vault Default Sorting
**Goal:** Make the Vault and Admin Vault dashboards sort active seeding/downloading/processing torrents on top, followed by queued/vaulted/expired torrents ordered by their creation date (`created_at DESC`).

- [x] **Define Sorting logic in Go:**
  - Create a sorting helper or inline slice sorting logic for `PledgeDisplay` and `AdminPledgeDisplay`.
  - Sort actively processing/seeding/downloading items (`WorkerStatus != nil && WorkerStatus.Status == 1`) to the top.
  - Sort all other items below them sorted by `CreatedAt DESC`.
- [x] **Apply to User Vault index:**
  - Update [index.go](file:///home/ubuntu/octor/web-ui/handlers/vault/index.go) in the `index` handler. Sort the slice of `PledgeDisplay` returned by `buildPledgeDisplay` before passing it to the view template.
- [x] **Apply to Admin Vault index:**
  - Update [vault.go](file:///home/ubuntu/octor/web-ui/handlers/admin/vault.go) in the `vaultIndex` handler. Sort `enrichedPledges` after fetching worker statuses and before passing it to the view template.

---

## 2. Library Selector "Vaulted" Option
**Goal:** Add a "vaulted" filter option to the `(all | watched | unwatched)` selector in movies, series, and adult library views to filter titles that are currently saved/vaulted in the user's vault.

- [x] **Define the filter state in Go:**
  - Add `WatchedFilterVaulted WatchedFilter = "vaulted"` to the `WatchedFilter` constants in [args.go](file:///home/ubuntu/octor/web-ui/handlers/library/shared/args.go).
  - Handle `shared.WatchedFilterVaulted` in the `bindIndexArgs` switch inside [index.go](file:///home/ubuntu/octor/web-ui/handlers/library/index.go).
- [x] **Update template dropdown options:**
  - Add `<option value="vaulted">` to [watched_filter.html](file:///home/ubuntu/octor/web-ui/templates/partials/library/watched_filter.html).
  - Add locale translations for `"library.filterVaulted": "Vaulted"` in [en.json](file:///home/ubuntu/octor/web-ui/locales/en.json) (and other locale json files).
- [x] **Implement Database Queries for "vaulted" status:**
  - In [library.go](file:///home/ubuntu/octor/web-ui/models/library.go):
    - Update `GetLibraryMovieList`: add a case for `"vaulted"` checking that a corresponding non-expired, vaulted record exists in `vault.resource` table:
      ```go
      case "vaulted":
          query.Where("EXISTS (SELECT 1 FROM vault.resource WHERE resource.resource_id = movie.resource_id AND resource.vaulted = true AND resource.expired = false)")
      ```
    - Update `GetLibrarySeriesList`: add a case for `"vaulted"`:
      ```go
      case "vaulted":
          query.Where("EXISTS (SELECT 1 FROM vault.resource WHERE resource.resource_id = series.resource_id AND resource.vaulted = true AND resource.expired = false)")
      ```
    - Update `GetLibraryAdultList`: add a case for `"vaulted"`:
      ```go
      case "vaulted":
          query.Where("EXISTS (SELECT 1 FROM vault.resource WHERE resource.resource_id = movie.resource_id AND resource.vaulted = true AND resource.expired = false)")
      ```

---

## 3. Library Selection Mode Layout & Action Fixes
**Goal:** Fix layout bugs and add bulk actions during library item selection in user and admin library views:
1. Disable and hide all interactive icons/badges (rating, watched status, poster layout toggle, metadata enrich, delete capsule) when selection mode is active.
2. Fix the select-all bulk expansion bug where vertical/horizontal poster cards grow into 2x2 grid spaces.
3. Add bulk enrich (refresh metadata) and bulk toggle watched status actions to the bulk actions bar.

- [x] **Hide interactive badges/actions during selection mode:**
  - Add a CSS rule in [style.css](file:///home/ubuntu/octor/web-ui/assets/src/styles/style.css) to hide rating, watched, layout toggle, and action capsule buttons when the selection checkboxes are active:
    ```css
    .w-card-frame:has(.library-item-cb-wrap:not(.hidden)) .w-card-badge,
    .w-card-frame:has(.library-item-cb-wrap:not(.hidden)) .w-card-badge-ghost,
    .w-card-frame:has(.library-item-cb-wrap:not(.hidden)) div.bottom-2.right-2 {
      display: none !important;
    }
    ```
- [x] **Fix 2x2 grid item expansion:**
  - Refactor the selector to target the layout checkbox class specifically (`[&:has(.layout-toggle-checkbox:checked)]:col-span-2`) instead of matching any checked input.
  - Update library item card wrapper: [video_list.html](file:///home/ubuntu/octor/web-ui/templates/partials/library/video_list.html#L7).
  - Update admin library item card wrapper: [library.html](file:///home/ubuntu/octor/web-ui/templates/views/admin/library.html#L113).
- [x] **Implement Bulk Enrich, Bulk Toggle Watched, and Bulk Toggle Poster Layout:**
  - Update bulk actions bar in user library template: [index.html](file:///home/ubuntu/octor/web-ui/templates/views/library/index.html#L59) (added refresh, watched toggle, and layout toggle forms).
  - Update bulk actions bar in admin library template: [library.html](file:///home/ubuntu/octor/web-ui/templates/views/admin/library.html#L82) (added refresh and layout toggle forms).
  - Add data attributes (`data-video-type`, `data-video-id`) to checkboxes in [video_list.html](file:///home/ubuntu/octor/web-ui/templates/partials/library/video_list.html#L119) and [library.html](file:///home/ubuntu/octor/web-ui/templates/views/admin/library.html#L118).
  - Update `submitLibraryBulk` in JS: [index.html](file:///home/ubuntu/octor/web-ui/templates/views/library/index.html) and [library.html](file:///home/ubuntu/octor/web-ui/templates/views/admin/library.html) to populate appropriate inputs and submit directly.
  - Register `/lib/enrich-multiple` in [handler.go](file:///home/ubuntu/octor/web-ui/handlers/library/handler.go) and implement it in [enrich_multiple.go](file:///home/ubuntu/octor/web-ui/handlers/library/enrich_multiple.go).
  - Register `/admin/library/enrich-multiple` in [handler.go](file:///home/ubuntu/octor/web-ui/handlers/admin/handler.go) and implement it in [enrich_multiple.go](file:///home/ubuntu/octor/web-ui/handlers/admin/enrich_multiple.go).
  - Register `/library/toggle-multiple` and `/library/layout-multiple` in [handler.go](file:///home/ubuntu/octor/web-ui/handlers/user_video_status/handler.go) and implement them.

---

## 4. Bulk Retry warning dialog correction
**Goal:** Prevent the remove/delete confirmation modal from appearing when selecting items and clicking "Retry" in bulk on the Vault and Admin Vault pages.

- [x] **Modify `submitVaultBulk` in JS:**
  - Update [index.html](file:///home/ubuntu/octor/web-ui/templates/views/vault/index.html#L503) (User Vault) and [vault.html](file:///home/ubuntu/octor/web-ui/templates/views/admin/vault.html#L528) (Admin Vault):
    - Check if the form ID is `'vault-bulk-retry-form'`.
    - If so, bypass `window.handleConfirmRemove`, populate the selected `resource_ids[]` and `user_ids[]` inputs directly inside the handler, trigger the layout selection toggle off (`window.toggleVaultMode()`), and return `true` to submit the form immediately.

---

## 5. Additional Vault Status Filter Options
**Goal:** Add "Errors" and "Not in Library" filter options to the status filter select menu on user and admin vault dashboards.

- [x] **Backend Handler Modifications:**
  - Retrieve the user's library resource IDs in the vault index handlers to determine if each vaulted item is currently linked to the library.
  - For User Vault index handler in [index.go](file:///home/ubuntu/octor/web-ui/handlers/vault/index.go):
    - Query `resource_id` values from the `library` table matching the current user.
    - Set a boolean `InLibrary` on the `PledgeDisplay` objects.
  - For Admin Vault index handler in [vault.go](file:///home/ubuntu/octor/web-ui/handlers/admin/vault.go):
    - Query `user_id` and `resource_id` combinations from the `library` table.
    - Set `InLibrary` on the `AdminPledgeDisplay` objects.
- [x] **Template modifications:**
  - Add dropdown options for `Errors` and `Not in Library` to `#vault-status-filter` select elements:
    - User Vault index template: [index.html](file:///home/ubuntu/octor/web-ui/templates/views/vault/index.html#L58)
    - Admin Vault index template: [vault.html](file:///home/ubuntu/octor/web-ui/templates/views/admin/vault.html#L71)
  - Add data attributes `data-vault-error` and `data-vault-in-library` to item card container elements:
    - User Vault card: [index.html](file:///home/ubuntu/octor/web-ui/templates/views/vault/index.html#L112)
    - Admin Vault card: [vault.html](file:///home/ubuntu/octor/web-ui/templates/views/admin/vault.html#L125)
- [x] **JavaScript Client Filter Updates:**
  - Update `filterItems` in [progress.js](file:///home/ubuntu/octor/web-ui/assets/src/js/app/vault/progress.js#L273) to process `errors` and `notinlib` cases:
    - Match `errors` based on `data-vault-error="true"`.
    - Match `notinlib` based on `data-vault-in-library="false"`.
    - Also update dynamic SSE state handlers to toggle `data-vault-error="true"` when an SSE update payload reports an error starting with `Error:`.
