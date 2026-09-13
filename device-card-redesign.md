# Device Card Redesign

## Goal

Replace the visible Machine ID on the device home page with an account-scoped cloud display name and adopt the task-status-first device card from prototype option 2.

## Confirmed Requirements

- Keep `machine_id` as the stable internal identity and route key.
- Do not display Machine ID in normal device UI.
- Keep `Copy device identifier` in the card overflow menu for diagnostics.
- Store custom names in the cloud and isolate them by account.
- Resolve the visible name as `custom display name -> operating-system hostname -> unnamed device`.
- Show the operating-system hostname only on the device detail page.
- Support rename from the card overflow menu and beside the detail-page title.
- Use the task-status-first card design from HTML prototype option 2.

## Data Model

Add an account-scoped preference table instead of changing machine identity:

```text
operator_device_preferences
- operator_id BIGINT NOT NULL
- machine_id VARCHAR(128) NOT NULL
- display_name VARCHAR(80) NOT NULL DEFAULT ''
- created_at DATETIME
- updated_at DATETIME
- PRIMARY KEY (operator_id, machine_id)
```

The API limits display names to 40 Unicode characters after trimming. Names are not required to be unique within one account.

## API

- Extend device list/detail responses with `display_name`.
- Add `PATCH /api/devices/{machineID}/profile` with `{ "display_name": "..." }`.
- Validate that the authenticated operator can access the machine before reading or updating preferences.
- Batch-load preferences for device lists to avoid one query per card.
- Empty `display_name` clears the custom name and restores the hostname fallback.

## Flutter UI

### Home Card

- Two-line display name with stable card height.
- Online, busy, and offline status shown under the title.
- Task area:
  - No active task: `当前无运行任务`.
  - One active task: task label plus determinate progress when available.
  - Multiple active tasks: `N 个任务正在运行`.
  - Offline: `等待设备重新连接`.
- Footer metrics: running/total Agent count and last-seen time.
- Overflow menu: `修改设备名称`, `复制设备标识`.
- Card body continues to open device details.

### Detail Page

- Show the effective display name as the title.
- Put an edit icon beside the title.
- Show the operating-system hostname as muted supporting text.
- Keep Machine ID out of the visible layout; expose it only through the diagnostic copy action.

### Rename Flow

- Open a 40-character rename dialog prefilled with the effective name.
- Apply an optimistic local update after validation.
- Persist through the profile API and invalidate list/detail providers.
- Roll back on failure and report the result through the existing global notification system.

## Search

- Change the hint to `搜索设备名称或主机名`.
- Match custom display name and hostname visibly.
- Continue matching Machine ID internally so a pasted diagnostic identifier still locates the device.

## Non-Functional Requirements

- Support hundreds of devices per account without N+1 database reads.
- Never expose one account's device alias to another account.
- Make profile updates idempotent and preserve names across reconnects, Launcher updates, and hostname changes.
- Keep all routing and task associations keyed by Machine ID; display-name changes must not affect runtime behavior.
- Cover desktop, tablet, and mobile widths, including 40-character names and mixed Chinese/English text.

## Decision Log

- Selected option 2, task-status-first, because the home page is an operational surface and current work state matters more than quick-action density.
- Chose account-scoped cloud aliases over local or global names to support cross-device clients without cross-account interference.
- Kept Machine ID hidden but copyable to preserve diagnostics without adding visual noise.
- Kept hostname only on the detail page; the home page uses it solely as the fallback when no custom name exists.
- Chose a separate preference table so mutable user presentation data does not alter stable machine identity records.

## Verification

- Backend tests for account isolation, access checks, trimming, length validation, clearing, and batch list hydration.
- Flutter model/repository tests for `display_name` parsing and rename requests.
- Widget tests for online, busy, multiple-task, offline, long-name, menu, rename success, and rollback states.
- Responsive visual checks on desktop and mobile.
- Regression checks that routes, task submission, project memory, notifications, and Launcher commands still use Machine ID.
