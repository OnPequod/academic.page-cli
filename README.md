# academic-page

A command-line tool and MCP server for [Academic.page](https://academic.page) sites. It talks only to the public API, so anything it does, any API client can do.

It is built for AI agents as much as for people: every command has `--json`, `academic-page schema` describes every command and field, writes have `--dry-run`, exit codes are stable, and `academic-page mcp` serves the same commands as MCP tools.

## Install

```sh
brew install onpequod/tap/academic-page
```

Or download a binary from the [releases](https://github.com/OnPequod/academic.page-cli/releases), or `go install github.com/OnPequod/academic.page-cli@latest`.

The macOS binaries are signed and notarized by Apple. Windows may show a SmartScreen prompt for the unsigned `.exe`; choose More info › Run anyway. See [docs/distribution.md](docs/distribution.md) for every platform.

## Sign in

API access needs a Pro plan. Create a key at [Account › API](https://academic.page/admin/account/api), then:

```sh
academic-page auth login          # paste the key; it is checked, then saved
academic-page sites list
academic-page sites use jane.academic.page   # default site for this profile, by its address
academic-page sites create --site-type lab --name "Smith Lab" --slug smith-lab   # a new site; prints its ID
```

A content key reads published content. A management key can also read drafts and edit the site.

Keys are stored in your OS config directory (`academic-page/config.json`, mode 0600) and are never printed. Use `--profile` for more than one account. Environment variables override the file:

| Variable | Meaning |
| --- | --- |
| `ACADEMIC_PAGE_TOKEN` | API key |
| `ACADEMIC_PAGE_SITE` | Default site ID |
| `ACADEMIC_PAGE_PROFILE` | Profile name |
| `ACADEMIC_PAGE_CONFIG` | Config file path |

## Use

```sh
academic-page profile get
academic-page publications list --all --json
academic-page publications bibtex > pubs.bib
academic-page posts list --drafts
academic-page key show                      # what this key may do
academic-page publications import --bibtex-file refs.bib --preview
academic-page publications import --bibtex-file refs.bib --select 0,smith2024
academic-page course-lessons list 3         # lessons of course 3
academic-page navigation get header         # the header menu, with each item's target
academic-page navigation update header --data @menu.json   # replace the whole header menu
academic-page navigation update header --data '{"items":[{"target":"section:projects","dropdown":"recent","dropdown_limit":6}]}'
academic-page appearance get                # theme, accent color, footer, and the themes this site can use
academic-page appearance update --theme pragma              # switch theme (takes its own accent color)
academic-page appearance update --footer-about "Methods for missing data." --show-footer-links=false   # footer: text on the left, no link icons

academic-page posts create --title "New paper out" --content-file post.md          # a draft
academic-page posts update 12 --publish
academic-page bluesky-handle update --did did:plc:kl3vsthiwyiov5c7n7lt2df3   # use the site's address as your Bluesky handle
academic-page post-settings update --feed-enabled           # publish an RSS feed of the posts at /feed.xml
academic-page profile-links update 7 --show-in-top-bar      # show a link as an icon at the top right of every page
academic-page pages create --title "Teaching" --content-markdown "I teach **stats**." --dry-run
academic-page media upload headshot.jpg --alt-text "Portrait"   # up to 1 GB
academic-page imports list                  # CV and syllabus imports, newest first
academic-page imports get 3                 # what an import found, and where it had trouble
academic-page courses update 4 --instructor-name "Ada Lovelace" --primary-institution "Example University"
academic-page courses update 4 --level graduate --class-size "10–20 students"
academic-page courses update 4 --course-site-id 12        # link a course on a Teaching page to its course site
academic-page posts delete 12 --yes
```

Content is written as Markdown, and the API converts it to the editor's format. Only the formatting both the editor and the public site support is used: headings (levels 1–3), paragraphs, lists, block quotes, code blocks, rules, and bold, italic, strikethrough, code, and links. Images become links until image embedding is supported. Every rich-text field works this way: `--content-markdown`, `--abstract-file`, `--bio-markdown`, and so on. If you need exact control, you can send the editor's Tiptap JSON in `--data` instead; the API checks it either way.

`academic-page schema` prints JSON describing every command, argument, flag, body field, exit code, and MCP tool. `academic-page schema posts create` describes one command.

Fields that take fixed values list them, in `--help` ("one of: reading, slides, …"), in `schema`, and as `enum` in the MCP tool input schemas. That covers values nested in lists too, such as the `term` of each course offering, which `--help` lists under the fields that only `--data` takes. Length limits show the same way. The values come from the API's OpenAPI document, so they match what the API accepts, and a rejected value's error names the accepted ones.

### Publication and talk order

Years always appear newest first. Within each year, talks and publications follow date order until you set your own. To set your own, list the IDs in the order you want:

```sh
academic-page talks reorder --ids 318,302,311
academic-page publications reorder --ids 911,904
```

`reorder` switches the section to custom order and places each ID within its own year, so one command can cover several years. In each year, the listed records take the order given and the year's other records keep their places; list every record of a year to set the whole year. The output shows each year it changed, in its new order.

The setting itself:

```sh
academic-page publications get-order
academic-page publications set-order --order custom
academic-page talks set-order --order date
academic-page talks set-order --reset
```

Switching to date order clears the saved positions. `--reset` clears them while keeping custom order. To place one record, update it with `--year-order`; in custom order, records without a position come first in date order.

### Courses and Course Info

On a course site, the site's first course is the one the admin's Course Info page edits. Its `title`, `instructor_name`, `primary_institution`, `excerpt`, and content are Course Info's Course Title, Instructor, Primary Institution, Short Introduction, and Full Description. Its title is also the site's name: changing it renames the site, and it cannot be emptied. `courses update` sends only the fields you give, so `--instructor-name` alone leaves the rest as they are; `--null instructor_name` clears one. Pass `--edit-version` as with any update.

A course on a personal site's Teaching page can link to its course website. `--course-site-id` links one of the team's course sites, and `--website-url` links any other address. Setting one clears the other. `--level` is `undergraduate`, `graduate`, or `other`. With `other`, `--level-other` gives the name visitors see, such as Professional. `--class-size` is free text.

### Uploads and retries

`media upload` sends the file straight to storage in parts when the site's storage supports it. Signing, each part, and completion are retried on network and server errors, and completing twice returns the same record.

Otherwise the file goes in one request with an `Idempotency-Key` header: a new UUID for each upload, reused on the CLI's own retries. If a response is lost after the file was stored, the retry returns the stored record rather than a second copy. If every retry fails, the error gives the key (`details.idempotency_key` with `--json`). Pass it back with `--idempotency-key` (or `idempotency_key` for MCP) within 24 hours to get the stored record, or the file stored once if it never arrived. The API answers 409 `idempotency_key_conflict` (exit code 10) when the key is still in use by an unfinished upload or was used for a different file.

## MCP

```sh
claude mcp add academic-page -- academic-page mcp
```

Each command is a tool (`posts_list`, `posts_create`, …) with the same arguments. Pin a server to an account or site with `academic-page mcp --profile work --site 42`.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Other failure |
| 2 | Usage: bad arguments, or no site chosen; nothing was sent |
| 3 | No key, or key rejected (401) |
| 4 | Not allowed: ability, scope, Pro plan, or site disabled (403) |
| 5 | Not found (404) |
| 6 | Rejected: validation failed (422) or file too large (413) |
| 7 | Rate limited (429) |
| 8 | Server error (5xx) |
| 9 | Network error |
| 10 | Conflict (409): the record changed since it was read, it already exists, a tag is in use, or an upload's idempotency key is in use |

With `--json`, errors go to stderr as `{"error": {"code", "message", "status", "fields", "details", "hint", "exit_code"}}`.

## What the API covers

Reads and writes: the profile (with degrees, positions, and links), posts, pages, projects, publications (including BibTeX import), talks, courses (with Course Info on course sites) and their lessons, materials, assignments, and references, lab members, study details, tags, and media. Reads only: sites, the manifest, and the BibTeX export. Records with drafts take `--drafts` on reads; the others take `--manage` for the management view, which includes `edit_version`. Pass `--edit-version` on updates so a change made elsewhere since you read the record returns a conflict (exit code 10) instead of being overwritten.

A page published without a slug gets one from its title; a slug another page under the same parent already uses is refused with 422 under `slug`.

Record and site URLs in responses come from the API, which builds them from the site's real address: its custom domain, its `.page` subdomain, or a mounted site's path under its parent. The CLI shows them as given.

## Development

```sh
make test      # go test ./...
make lint      # golangci-lint
make spec      # refresh internal/spec/openapi.json from the live API
make spec SPEC_URL=http://academic-turbo-raven.test/api/v1/openapi.json   # from a local app
```

`make spec` fetches from production unless `SPEC_URL` names another copy of the API, such as a local app with unreleased changes. Either way it sets the document's `servers` entry back to production, and a test fails if the embedded document names another host.

Commands, flags, the schema output and the MCP tools are all generated, when the CLI starts, from the embedded OpenAPI document plus the table in `internal/registry/registry.go`. Path and query parameters become arguments and flags, header parameters become options (`Idempotency-Key` becomes `--idempotency-key`), and body fields become flags, with lists of objects taken through `--data`. After `make spec`, the tests fail if the API gained an operation that has no command, or if an `enum` in a request body or query parameter, nested ones included, does not reach the input schemas.

Releases: push a `v*` tag. GoReleaser builds the binaries, publishes a GitHub release, and updates the cask in [OnPequod/homebrew-tap](https://github.com/OnPequod/homebrew-tap) using the `HOMEBREW_TAP_TOKEN` secret.

## License

MIT. See [LICENSE](LICENSE).

### Project links and publishing

Projects accept an ordered list of titled URLs through `--data`:

```sh
academic-page projects create --title "Research toolkit" --data '{"links":[{"title":"Dataset","url":"https://example.org/data"}]}' --publish
academic-page projects update 42 --unpublish
```

Projects are published or drafts. They have no publication-date or scheduling
flag. `--project-url`, `--repository-url`, and `--technologies` have been replaced
by the `links` list.
