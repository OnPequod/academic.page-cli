# Fresh-site CLI audit, November 3, 2026

## Scope and status

This audit used the local Pequod application and an isolated CLI config. No production sites were changed. Setup operations missing from the public API were driven through real Livewire components from PHP CLI scripts. Stripe was used in test mode. These fallbacks establish application behavior, but do not make those operations available to a normal CLI user.

The audit created personal site 18, standalone course 19, lab 20, study 21, and nested course 22 for team 7. The personal site contains five degrees, 13 positions, five profile links, 19 publications, 20 talks, nine projects, four posts, four pages, a CV, and a profile photo. Counts include drafts. Course 19 has five lessons, 11 materials, three assignments, and five references. Course 22 has one lesson and one material. Lab 20 has six members and study 21 has three team members. The nested course was a lighter pass than the standalone course.

The original run covered signup and email verification, Pro setup, key creation, onboarding, content CRUD and reordering, DOI suggestions, uploads and assignments, themes, menu changes, nested sites, and bucket mirroring. Local data workarounds were necessary for stale media conversions and menu deletion. The original run used direct model deletion for duplicate menu entries. This is not evidence that every form or combination works without intervention. GitHub mirroring and production checkout/webhook delivery were not completed end to end.

## CLI changes in this branch

- Refresh the embedded OpenAPI document, retaining the production server URL.
- Add `publications get-order`, `publications set-order`, `talks get-order`, and `talks set-order`. The refreshed spec exposes `--year-order` for per-record positions. These commands are also available through the schema and MCP registry.
- Include record URLs and edit versions in write summaries, and omit missing IDs from singleton summaries.
- Show site types and complete URLs in list tables.
- Correct the macOS clipboard login example to `pbpaste`, use singular/plural site counts, and name the default site after login.
- Omit Laravel trace, exception, file, and line details from ordinary error output. Preserve them in JSON and keep actionable conflict details in ordinary output.

## Verification on resume

`go test ./...`, `go vet ./...`, and `go build -o bin/academic-page .` passed. Command tests cover both section-order endpoints and per-record year positions. Output checks cover singleton summaries, URLs, edit versions, login summaries, long site URLs, and diagnostic handling.

A read-only live pass checked 57 CLI calls across sites 18–22. There were 41 successful responses and 16 expected 404 feature gates (for example, course sites have no profile). A further 12 calls read the two courses' lesson, assignment, reference, and material lists successfully. `publications get-order` returned custom and `talks get-order` returned date. The new ordering writes were verified against a fake HTTP server; no new live ordering changes were made during the integration window.

No PHP tests or live database writes were run on resume, because the integration thread owns checks in the main checkout. Existing main-checkout edits to WorkspaceSeedCommand, workspace-setup, and WorkspacePreviewHostCommandTest predate this audit and are not owned here.

## Application findings

The findings below are observations from the original audit against the main checkout. They are not fixes in this CLI branch. The stale `thumb` metadata on this audit account's moved files was cleared as a data workaround, so later successful reads do not establish a code fix. The integrator should verify these findings against its final candidate and distinguish existing fixes from remaining work.


### Stripe webhooks all fail with 500 (Cashier customer model)
- config/cashier.php sets 'model' => Team::class, but Cashier 16 ignores that key; Cashier::$customerModel stays App\Models\User.
- POST /stripe/webhook (customer.subscription.updated) -> 500, "column stripe_id does not exist" (users has no stripe_id).
- WebhookHandled never fires, so SyncTeamSubscription never runs. Portal cancellations/plan changes only land via the scheduled reconcile.
- Fix: Cashier::useCustomerModel(Team::class) in AppServiceProvider::boot.
- Severity: major (production billing).

### subscription_items lacks Cashier 16 columns meter_id, meter_event_name
- App ships its own copies of Cashier's migrations; the 2025_06_06 meter migrations are missing.
- Team::newSubscription()->create() fails on insert. Any Cashier path that writes items (incl. webhook once the model is fixed) will fail.
- Severity: major once webhooks work.

### ReconcileSubscriptions catches only \Exception
- A local subscription row whose owner resolves null throws \Error ("stripe() on null") and aborts the whole job for every team.
- Severity: minor (robustness).


### Content API GET /profile answers 500 once a profile photo is set
- Steps: media upload profile.jpg; profile update --photo-media-id 184; profile get.
- "There is no conversion named `thumb`" from ContentApiPresenter::media(). The file moved from the site library (thumb generated) into the bio's photo collection, which registers no thumb; generated_conversions still lists it.
- Severity: blocker for any API/headless/static reader of a profile with a photo. Fix: drop generated_conversions on move, or list only conversions the new owner registers.

### Alt text is lost when a library file is assigned
- Uploaded with --alt-text; after --photo-media-id / --featured-media-id the media's custom properties are [] and the API returns alt null.
- Severity: major (accessibility).

### API-created publications take the DOI record wholesale (apply mode), silently
- PublicationIntake::create() uses MODE_SUGGEST only for source 'manual'; the API passes 'api', so typed values are replaced with no suggestion.
- Data lost from typed entries: pages 304-314 -> 304 (84), 845-854 -> 845 (86); 73-81 -> 073-081 (79); title case forced to sentence case (77, 83, 87); year/volume/issue replaced (87, 81, 80, 82).
- 88: the CV's title (the conference title) replaced by the paper's real title. Right result, but the user is never told.
- Severity: major. A person typing a publication by hand through the API should get the admin form's behaviour (keep theirs, offer a suggestion), or at least a list of what changed in the response.

### Crossref abstract double-escaped
- Publication 84 suggestion abstract contains "P &amp;lt;.001". Severity: minor.

### Reorder bumps edit_version unevenly and answers with a paginated list
- profile-positions reorder: some rows went to edit_version 3 after one reorder. The response is a paginated collection whose links point at the PUT-only /order URL.
- Severity: minor.

### Profile links: "Username or URL is required" when a URL was sent
- profile-links create --service github --url https://github.com/table1 -> 422 on service_handle. service_handle itself accepts a URL. Message should say to send the URL or username as service_handle.
- Severity: minor.

### Timestamps: management API writes +00:00, content API writes Z. Severity: polish.
### CV filename on the public site carries a timestamp prefix (20261103120323_18_erik-westlund-jh-cv.pdf). Severity: polish.


### Posts and projects published through the API get no slug, so no URL
- posts create --publish / projects create --publish: slug null, path null, url null. Public list shows the item with no link. Talks and pages make one from the title.
- Severity: major. Every API-published post and project is unreachable until a slug is set by hand.

### Post and project slugs are not validated
- posts update 41 --slug "New Role!! At JHU" accepted; url becomes /posts/New Role!! At JHU.
- A duplicate slug answers 500 (posts_site_id_slug_unique), not 422.
- Severity: major.

### Project status accepts any string
- projects create --status bogus stored "bogus"; the admin offers in-progress, completed, archived. The CLI help lists no choices.
- Severity: minor.

### The "thumb" conversion bug also breaks GET /sites/{site}/media (management list)
- media list --manage -> 500, same InvalidConversion. Severity: blocker for the media library over the API.

### Alt text cannot be set on an assigned image
- Upload takes alt_text, update takes default_alt_text (naming differs), and once a file is assigned (photo, featured image) it leaves the library and no endpoint edits its alt. Severity: major (accessibility).

### The "teaching" slug is reserved on a personal site with courses off (/teaching is 404). Severity: polish.

### Local: API site URLs (http://ewestlund.biostatistician.test) do not resolve unless the domain is linked in Herd. Severity: dev only.


### Deleting a menu item crashes the admin (blocker, on main since 2025-11-17)
- app/Livewire/Admin/Navigation/Edit.php:401 eager-loads 'link'; the relation is navigationLink. RelationNotFoundException on every delete. No test calls deleteItem.

### A slug redirect overrides a live record that later takes the slug
- Post 42 had slug new-role-at-jhu, then was renamed (redirect recorded). Post 41 then took new-role-at-jhu; /posts/new-role-at-jhu still 301s to post 42.
- Severity: major (rename-then-reuse is ordinary).

### Choosing a bucket fails for any site whose profile photo came from the library, and leaves the site half-configured
- BucketDestination::updateStorageProvider -> Site::save hook -> ByosSyncService::queueSiteSync -> MirrorSiteResourceJob::resourceForMedia -> InvalidConversion thumb.
- storage_provider_id is saved before the throw; choosing the bucket again is "No change" and returns, so nothing is ever copied, and there is no "copy everything now" for buckets.
- Severity: blocker for mirroring (same root cause as the thumb bug), major for the no-retry state.

### Mirror exports soft-deleted records (talks/43, talks/44, deleted_at set). Possibly intended; worth a decision.

### Menus fill themselves (Projects, Publications, Talks, Posts appear once a section has content); a CLI user adding them by hand gets duplicates. The API exposes menus in sites get, but neither the API nor the CLI can edit them.

### Personal site has no /courses index for nested course sites (404). Severity: minor.
### Lesson materials: no date/week field on lessons. Severity: polish/question.

## Gaps: things a CLI-only user cannot do (no API)

- Sign up, verify email, finish onboarding (domain, timezone, public email, search indexing).
- Upgrade plan / billing.
- Mint, list, expire, revoke API keys.
- Create a site (any type, subdomain or nested); delete a site.
- Publish or unpublish a site (a CLI-built site stays 404 until someone uses the admin).
- Theme, accent colour, list view, favicon, footer text (sites get reads them; nothing writes them).
- Menus: read-only in sites get; no add, remove, reorder, or groups. No way to put the CV in the menu.
- Content mirroring and exports (buckets, GitHub), and "copy everything now".
- Site settings: name, timezone, locale, search indexing, sections on/off (courses are off on personal sites with no way to turn them on).
- Study resources and study featured image (read-only in the API).
- Alt text on an assigned image; media list of assigned files.
- CV import (the admin's CV import sources).
- Publication section order existed in the API but not the CLI (added in this branch).

## Additional billing findings

### ReconcileSubscriptions passes the wrong shape to asStripeCustomer
- Calls asStripeCustomer(['expand' => ['subscriptions']]); Cashier already wraps the argument in 'expand', so Stripe rejects it: "Invalid array".
- Logged as "Failed to reconcile subscription" for every team without a local subscriptions row (which is every team, since checkout never writes one).
- Fix: asStripeCustomer(['subscriptions']).
- Severity: major. With webhooks also failing, the hourly reconcile is the only sync path and it never works.

### subscribed_until is always null on Stripe API 2025-08-27.basil
- TeamSubscriptionService reads current_period_end at the subscription's top level; on basil it lives on items.data.0.current_period_end.
- Observed: sync of an active Pro subscription left subscribed_until empty.
- Effect, together with the two above: a cancellation in the Stripe portal never reaches the app, and the plan never lapses.
- Severity: major.
