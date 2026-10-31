# Distribution

This note records how `academic-page` is shipped today, what each platform does with unsigned binaries, and the options we have deferred. Revisit it once there is evidence that people outside the team use the CLI.

## Current setup

A `v*` tag runs GoReleaser (`.github/workflows/release.yml`). It builds binaries for macOS, Linux, and Windows on amd64 and arm64. It then publishes them to a GitHub release and updates the Homebrew cask in [OnPequod/homebrew-tap](https://github.com/OnPequod/homebrew-tap) with the `HOMEBREW_TAP_TOKEN` secret.

The binaries are not signed with a paid certificate. The Go linker gives macOS arm64 binaries an ad-hoc signature, which Apple Silicon requires before it will run any code. Gatekeeper still rejects the binaries when it checks them, because they are not notarized.

## How each install path behaves

macOS only runs Gatekeeper on files that carry the `com.apple.quarantine` attribute. Browsers set that attribute on downloads, and Homebrew sets it on casks. `curl`, `wget`, and `go install` do not.

| Install path | Result |
| --- | --- |
| macOS, `brew install onpequod/tap/academic-page` | Runs. The cask's postflight step removes the quarantine attribute. |
| macOS, `go install` | Runs. The binary is built locally and is not quarantined. |
| macOS, `curl` or `wget` from the release | Runs. The download is not quarantined. |
| macOS, browser download from the release | Blocked on first run with "cannot verify the developer". Users can run `xattr -d com.apple.quarantine academic-page` or choose Open Anyway in System Settings › Privacy & Security. |
| Linux, any path | Runs. Linux has no signing requirement. |
| Windows, run from a terminal | Usually runs. |
| Windows, `.exe` opened from Explorer after a browser download | SmartScreen may show "Windows protected your PC". Users can choose More info › Run anyway. Antivirus tools occasionally flag unsigned Go binaries. |

Homebrew has been tightening its rules on unsigned software. The rules announced so far apply to the official `homebrew/cask` repository, which does not include third-party taps. If a later Homebrew release stops the postflight step from working, the fallback is the deprecated `brews` setting in GoReleaser, which publishes a formula instead of a cask.

## Deferred options

Each option below solves a problem that only matters once there are users. They are listed roughly from cheapest to most expensive.

### Install script

A `curl -fsSL …/install.sh | sh` script (and a PowerShell equivalent) avoids quarantine on macOS for free, because `curl` does not set the attribute. Claude Code ships this way. The script would download the right archive from the GitHub release, check it against `checksums.txt`, and place the binary on the `PATH`.

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

### macOS notarization

Notarization removes the Gatekeeper prompt for every install path. It costs $99 a year for the Apple Developer Program. Enrolling as an organization needs a D-U-N-S number, and the certificate then shows the organization's name.

Setup:

1. Create a Developer ID Application certificate in the Apple developer portal. Export it from Keychain Access as a `.p12` file with a password.
2. Create an App Store Connect API key for the notary service under Users and Access › Integrations › App Store Connect API. Download the `.p8` file (it can only be downloaded once) and record the key ID and issuer ID.
3. Store five secrets on this repository: `MACOS_SIGN_P12` (the base64-encoded `.p12`), `MACOS_SIGN_PASSWORD`, `MACOS_NOTARY_KEY` (the base64-encoded `.p8`), `MACOS_NOTARY_KEY_ID`, and `MACOS_NOTARY_ISSUER_ID`. Pass them to GoReleaser in `release.yml`.
4. Add the configuration below to `.goreleaser.yaml`. GoReleaser signs and notarizes on the Linux runner, so no macOS runner is needed. This block has not been checked against the GoReleaser documentation or run.
5. Remove the cask's postflight quarantine step.

```yaml
notarize:
  macos:
    - enabled: '{{ isEnvSet "MACOS_SIGN_P12" }}'
      sign:
        certificate: "{{ .Env.MACOS_SIGN_P12 }}"
        password: "{{ .Env.MACOS_SIGN_PASSWORD }}"
      notarize:
        issuer_id: "{{ .Env.MACOS_NOTARY_ISSUER_ID }}"
        key_id: "{{ .Env.MACOS_NOTARY_KEY_ID }}"
        key: "{{ .Env.MACOS_NOTARY_KEY }}"
        wait: true
        timeout: 20m
```

A notarization ticket cannot be stapled to a bare binary. macOS checks with Apple online the first time a notarized binary runs, so a machine without network access may still show a prompt.

### Windows code signing

Windows has no notarization step. It uses Authenticode signatures, and SmartScreen weighs the signature together with the publisher's download reputation. A signed binary can still trigger a warning until it has built that reputation. Since 2024, EV certificates no longer skip this period.

The options are Azure Trusted Signing (about $10 a month, with eligibility limits on organization age and on individuals' countries) and commercial certificates from vendors such as Sectigo or DigiCert (a few hundred dollars a year). Since 2023, commercial certificate keys must be held in hardware or in the vendor's cloud signing service, which makes signing in CI harder. GoReleaser can call either through its `signs` setting. Check current eligibility and prices before committing to one.
