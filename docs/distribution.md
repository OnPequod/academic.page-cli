# Distribution

This note records how `academic-page` is shipped today, how each platform treats the binaries, and the options we have deferred. Revisit it once there is evidence that people outside the team use the CLI.

## Current setup

A `v*` tag runs GoReleaser (`.github/workflows/release.yml`). It builds binaries for macOS, Linux, and Windows on amd64 and arm64. It then publishes them to a GitHub release and updates the Homebrew cask in [OnPequod/homebrew-tap](https://github.com/OnPequod/homebrew-tap) with the `HOMEBREW_TAP_TOKEN` secret.

The macOS binaries are signed with the Developer ID Application certificate of Erik Westlund (team 2NATT8G496) and notarized by Apple. GoReleaser does both on the Linux runner (the `notarize` block in `.goreleaser.yaml`), using five repository secrets: `MACOS_SIGN_P12` (the base64-encoded `.p12`), `MACOS_SIGN_PASSWORD`, `MACOS_NOTARY_KEY` (the base64-encoded App Store Connect `.p8`), `MACOS_NOTARY_KEY_ID`, and `MACOS_NOTARY_ISSUER_ID`. The values also live in the vault under `apple.*`. The certificate expires on November 30, 2027.

Signing is skipped when the secrets are absent, as in local snapshots. Tags with a prerelease suffix, such as `v0.2.0-rc.1`, publish a GitHub prerelease and leave the Homebrew tap alone.

The Linux and Windows binaries are not signed.

## How each install path behaves

| Install path | Result |
| --- | --- |
| macOS, `brew install onpequod/tap/academic-page` | Runs. The binary is notarized. |
| macOS, browser download from the release | Runs. Gatekeeper checks the notarization online on first run. |
| macOS, `curl`, `wget`, or `go install` | Runs. These do not quarantine the file, and `go install` builds it locally without a signature. |
| Linux, any path | Runs. Linux has no signing requirement. |
| Windows, run from a terminal | Usually runs. |
| Windows, `.exe` opened from Explorer after a browser download | SmartScreen may show "Windows protected your PC". Users can choose More info › Run anyway. Antivirus tools occasionally flag unsigned Go binaries. |

## Deferred options

Each option below solves a problem that only matters once there are users. They are listed roughly from cheapest to most expensive.

### Install script

A `curl -fsSL …/install.sh | sh` script (and a PowerShell equivalent) would give a one-line install without Homebrew. Claude Code ships this way. The script would download the right archive from the GitHub release, check it against `checksums.txt`, and place the binary on the `PATH`.

### npm

esbuild and Biome publish native binaries through npm as platform-specific optional dependencies. npm downloads do not carry the quarantine attribute or the Windows Mark of the Web, so neither Gatekeeper nor SmartScreen intervenes. This would also allow `npx -y academic-page mcp`, which is a common way to configure MCP servers. It requires users to have Node.

Rewriting the CLI in JavaScript would only help if npm were the sole channel. A JavaScript tool compiled to a single executable (with Bun, Node's single-executable feature, or `pkg`) is a native binary and meets the same checks. Claude Code is an example: it is built with Bun, and Anthropic signs it with a Developer ID certificate.

### winget

winget is the Windows package manager. It does not require signed binaries, because Microsoft scans submissions. A winget install does not trigger SmartScreen.

Setup:

1. Fork `microsoft/winget-pkgs` into the OnPequod organization: `gh repo fork microsoft/winget-pkgs --org OnPequod --clone=false`.
2. Create a personal access token that can push to the fork and open pull requests against `microsoft/winget-pkgs`. A classic token with the `public_repo` scope is the simplest. Store it as the `WINGET_TOKEN` secret on this repository and pass it to GoReleaser in `release.yml`.
3. Add the configuration below to `.goreleaser.yaml`. A snapshot build with it produced valid manifests for x64 and arm64.
4. Sign the Microsoft contributor license agreement on the first pull request and wait for review. Later versions are mostly automatic.


```yaml
winget:
  - name: academic-page
    package_identifier: OnPequod.AcademicPage
    publisher: OnPequod
    publisher_url: https://github.com/OnPequod
    short_description: Manage an Academic.page site from the command line, or as an MCP server
    license: MIT
    homepage: https://academic.page
    skip_upload: '{{ if isEnvSet "WINGET_TOKEN" }}false{{ else }}true{{ end }}'
    commit_author:
      name: academic-page-release
      email: noreply@academic.page
    commit_msg_template: "New version: OnPequod.AcademicPage {{ .Version }}"
    repository:
      owner: OnPequod
      name: winget-pkgs
      branch: "academic-page-{{ .Version }}"
      token: '{{ envOrDefault "WINGET_TOKEN" "" }}'
      pull_request:
        enabled: true
        draft: false
        base:
          owner: microsoft
          name: winget-pkgs
          branch: master
```

### Windows code signing

Windows has no notarization step. It uses Authenticode signatures, and SmartScreen weighs the signature together with the publisher's download reputation. A signed binary can still trigger a warning until it has built that reputation. Since 2024, EV certificates no longer skip this period.

The options are Azure Trusted Signing (about $10 a month, with eligibility limits on organization age and on individuals' countries) and commercial certificates from vendors such as Sectigo or DigiCert (a few hundred dollars a year). Since 2023, commercial certificate keys must be held in hardware or in the vendor's cloud signing service, which makes signing in CI harder. GoReleaser can call either through its `signs` setting. Check current eligibility and prices before committing to one.
