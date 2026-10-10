# Amiga Constellation: suggestions, voting, and private management

## Summary

Convert the existing catalogue into a lightweight VPS application, preserving its design, catalogue views, lineage map, and reading interactions.

Visitors can propose changes, new entries, removals, and image URLs. A public table shows suggestions, their status, and +1 support. Your private panel supports review, editing, immediate approval, and direct catalogue management.

Implementation and verification stay local. **No deployment or upload is included.**

## Architecture and migration

- Use **one Go application with SQLite**, organized into compact modules connected through explicit constructors. Serve HTML, CSS, JavaScript, images, and APIs from the same application.
- Retain vanilla JavaScript; split catalogue logic, lineage layout, shared entry rendering, suggestion forms, and administration into focused modules.
- Import the existing **333 entries, 422 lineage links, six routes, and 159 images**. After initialization, SQLite becomes the live source of truth; application restarts and upgrades never overwrite it with seed data.
- Preserve existing entry IDs and content fields. Add entry revisions, suggestions, votes, moderation history, image provenance, and owner authentication records.
- Replace standalone HTML generation and the editable JavaScript sidecar with normal application assets. Provide validated JSON import/export commands and update the documentation.
- Cache the public catalogue response with an ETag and rebuild it only after a published change. Keep filtering and map calculations in the browser; use no background polling or additional database service. SQLite fits this small, lightly written catalogue. [SQLite guidance](https://www.sqlite.org/whentouse.html)

## Public suggestions and voting

- Add `/suggestions`, `/suggestions/new`, and individual suggestion pages. Entry dialogs gain **Suggest a change**, **Suggest an image**, and **Suggest removal** actions that preselect the entry.
- Offer four proposal types: new entry, entry change, removal, and image. Forms use existing category and architecture vocabularies, searchable relationship selection, a reason, and supporting source URLs.
- New-entry forms collect the required catalogue information. Importance and verification flags remain owner-controlled. Existing-entry proposals record only changed fields and retain the original entry snapshot.
- Show likely existing entries and pending suggestions before submission to reduce duplicates.
- Publish valid submissions immediately as **Pending**. The public table includes type, entry/title, summary, date, status, and +1 count, with search, filters, pagination, and newest/most-supported sorting.
- Public detail pages show the original proposal, sources, decision, and approved result. **Accepted with edits** is clearly distinguished. Optional contact details and internal moderation notes remain private.
- Allow one toggleable +1 per browser on pending suggestions. Preserve totals when suggestions are resolved; freeze further voting. Votes inform your review and never trigger automatic acceptance.
- Mark duplicate suggestions with a link to the canonical proposal; keep their vote totals separate. Hidden spam disappears from public pages and APIs.

### Spam protection

- Combine **Turnstile** with signed browser identifiers, CSRF/origin checks, request limits, a honeypot, timing checks, and duplicate detection.
- Validate Turnstile tokens on the server for submissions and vote changes, including hostname/action checks. Refresh expired tokens and preserve form input on failure. Production mutations fail closed if verification is unavailable. [Turnstile validation](https://developers.cloudflare.com/turnstile/get-started/server-side-validation/)
- Apply configurable limits per browser and hashed IP: initially five suggestions per day and 30 vote changes per hour, with higher shared-IP ceilings.
- Enforce vote uniqueness in SQLite, including concurrent requests. Avoid fingerprinting; anonymous votes remain a support signal rather than guaranteed unique people.
- Provide hide-spam controls and temporary submission/voting pauses in the private panel.

## Private review and catalogue management

- Protect `/admin` with one owner account using a password and authenticator code. Include secure sessions, login throttling, recovery codes, and a local recovery command.
- Show a review queue sortable by age or support, with filters for proposal type, status, and affected entry.
- Present **current entry, proposed differences, and final preview**, followed by **Accept**, **Edit & accept**, **Reject**, **Mark duplicate**, or **Hide spam**.
- Preserve the visitor’s original proposal when you edit it. Store the final accepted version separately.
- Publish approval atomically with its decision and history record. Repeated clicks cannot apply a proposal twice. Stale proposals highlight conflicts and require review before overwriting newer changes.
- Support direct creation, editing, removal, and restoration without a visitor proposal.
- Before removal, show affected lineage links, guided-route stops, and featured placements. Acceptance explicitly confirms their cleanup and records all affected changes together.
- Record before/after values for every publication. Restoring creates a new validated revision; conflicts with later changes must be resolved.
- Keep taxonomy and narrative maintenance available through validated JSON import/export in v1.

### Suggested images

- Collect the image URL, credit, and optional source-page URL. Pending public suggestions display links rather than automatically loading remote images.
- Fetch images only through an owner-triggered preview or approval. Stage the validated file privately, then publish a local copy on acceptance.
- Permit supported raster images; reject SVG/HTML, private-network destinations, oversized files, and excessive dimensions. Validate DNS and redirects to prevent internal-network access. [OWASP image-fetch security guidance](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)
- Start with an 8 MB download limit, 16-megapixel dimension limit, and one image-processing operation at a time.
- Preserve source URL and credit alongside the local asset. Failed downloads leave the proposal pending and the catalogue unchanged.

## Verification and VPS preparation

- Preserve the existing 40 passing lineage-layout scenarios. Verify catalogue navigation, details, filters, routes, both themes, keyboard operation, and narrow screens after refactoring.
- Test all proposal types, public visibility, voting/unvoting, duplicate requests, spam hiding, Turnstile failures, and rate limits.
- Test authentication, private-data isolation, stale approvals, removal dependencies, restoration, image-fetch restrictions, and transaction rollback.
- Verify migration fidelity, restart persistence, and backup restoration.
- Measure idle memory and CPU, catalogue requests, concurrent voting, and image approval. Aim for **under 64 MB idle application memory**, excluding the reverse proxy; report actual measurements.
- Prepare a Linux binary, systemd unit, environment configuration example, HTTPS reverse-proxy instructions, health endpoint, and database/media backup commands. Runtime data and secrets stay outside Git.

**Defaults:** one Linux VPS, one application instance, one owner, account-free contributors, immediate publication after approval, local approved images, and built-in protection plus Turnstile. Comments, email notifications, and contributor accounts are outside v1. Real domain details and Turnstile keys are supplied during future deployment.
