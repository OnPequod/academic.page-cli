# academic-page

A command-line tool and MCP server for [Academic.page](https://academic.page) sites. It talks only to the public API, so anything it does, any API client can do.

It is built for AI agents as much as for people: every command has `--json`, `academic-page schema` describes every command and field, writes have `--dry-run`, exit codes are stable, and `academic-page mcp` serves the same commands as MCP tools.

## Install

```sh
brew install onpequod/tap/academic-page
```

Or download a binary from the [releases](https://github.com/OnPequod/academic.page-cli/releases), or `go install github.com/OnPequod/academic.page-cli@latest`.

The binaries are not notarized. On macOS, a binary downloaded with a browser is blocked on first run. Allow it with `xattr -d com.apple.quarantine academic-page`, or with Open Anyway in System Settings › Privacy & Security. Homebrew, `curl`, and `go install` are not affected. See [docs/distribution.md](docs/distribution.md) for every platform.

## Sign in

API access needs a Pro plan. Create a key at [Account › API](https://academic.page/admin/account/api), then:

```sh
academic-page auth login          # paste the key; it is checked, then saved
academic-page sites list
academic-page sites use 42        # default site for this profile
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

academic-page posts create --title "New paper out" --content-file post.md          # a draft
academic-page posts update 12 --publish
academic-page pages create --title "Teaching" --content-markdown "I teach **stats**." --dry-run
academic-page media upload headshot.jpg --alt-text "Portrait"   # up to 1 GB
academic-page posts delete 12 --yes
```

Content is written as Markdown and converted to the editor's format. Only the formatting both the editor and the public site support is used: headings (levels 1–3), paragraphs, lists, block quotes, code blocks, rules, and bold, italic, strikethrough, code, and links. Images become links until image embedding is supported. Every rich-text field works this way: `--content-markdown`, `--abstract-file`, `--bio-markdown`, and so on. If you need exact control, you can send the editor's Tiptap JSON in `--data` instead; the API checks it either way.

`academic-page schema` prints JSON describing every command, argument, flag, body field, exit code, and MCP tool. `academic-page schema posts create` describes one command.

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
| 10 | Conflict (409): the record changed since it was read, it already exists, or a tag is in use |

With `--json`, errors go to stderr as `{"error": {"code", "message", "status", "fields", "details", "hint", "exit_code"}}`.

## What the API covers

Reads and writes: the profile (with degrees, positions, and links), posts, pages, projects, publications (including BibTeX import), talks, courses and their lessons, lab members, study details, tags, and media. Reads only: sites, the manifest, and the BibTeX export. Records with drafts take `--drafts` on reads; the others take `--manage` for the management view, which includes `edit_version`. Pass `--edit-version` on updates so a change made elsewhere since you read the record returns a conflict (exit code 10) instead of being overwritten.

## Development

```sh
make test      # go test ./...
make lint      # golangci-lint
make spec      # refresh internal/spec/openapi.json from the live API
```

Commands, flags, the schema output and the MCP tools are all generated from the embedded OpenAPI document plus the table in `internal/registry/registry.go`. After `make spec`, the tests fail if the API gained an operation that has no command.

Releases: push a `v*` tag. GoReleaser builds the binaries, publishes a GitHub release, and updates the cask in [OnPequod/homebrew-tap](https://github.com/OnPequod/homebrew-tap) using the `HOMEBREW_TAP_TOKEN` secret.

## License

MIT. See [LICENSE](LICENSE).
