# Release guide

This document covers how maintainers create a release, and how users can verify that a downloaded artifact is authentic.

## Creating a release

1. Ensure all desired commits are merged to `main` and the post-merge `build` workflow is green.
2. Go to **Actions → release → Run workflow** and provide:
   - **version** — a semver tag, e.g. `v1.2.3`
   - **prerelease** — check if this is a pre-release
   - **release\_notes** — optional Markdown notes; these appear in the GitHub Release body
3. A maintainer with access to the **release** GitHub Environment must approve the workflow run before any signing secrets are exposed.
4. The workflow re-runs the full build matrix and security scans, builds platform bundles, signs the checksum file with cosign (keyless OIDC), generates an SBOM, and publishes a GitHub Release automatically.

## Artifacts in each release

| File | Description |
|------|-------------|
| `prismconductor-linux-amd64.tar.gz` | Linux binary |
| `prismconductor-macos-amd64.tar.gz` | macOS `.app` bundle |
| `prismconductor-windows-amd64.zip` | Windows `.exe` |
| `SHA256SUMS.txt` | SHA-256 checksums for all three platform archives |
| `SHA256SUMS.txt.bundle` | cosign bundle (signature + certificate) for `SHA256SUMS.txt` |
| `sbom-go.cdx.json` | CycloneDX SBOM for the Go module graph |

## First-launch instructions per platform

### macOS

The `.app` bundle is **ad-hoc codesigned only** — it does NOT carry an Apple Developer ID signature and is not notarized (the project does not yet have a paid Apple Developer Program membership). On first launch macOS will refuse to open it normally. To bypass the Gatekeeper prompt:

1. Open Finder, navigate to the extracted `prismconductor.app`.
2. **Right-click → Open** (or Control-click → Open).
3. macOS shows a warning dialog with an **Open** button — click it.
4. After the first successful launch the app opens normally on every subsequent run.

If the app silently refuses to open with no dialog, run `xattr -dr com.apple.quarantine prismconductor.app` once and try again.

### Windows

The `.exe` is unsigned. SmartScreen will show "Windows protected your PC". Click **More info → Run anyway**.

### Linux

No additional steps. The binary is a static cgo build linked against system GTK3 + WebKit2GTK 4.0 shared libs; install those via your distro's package manager if not already present.

## Verifying artifact integrity

### 1. Verify the checksum

```sh
sha256sum --check --ignore-missing SHA256SUMS.txt
```

### 2. Verify the cosign signature

Signatures use cosign keyless signing backed by the Sigstore transparency log. You need `cosign` installed (`go install github.com/sigstore/cosign/v2/cmd/cosign@latest` or [releases](https://github.com/sigstore/cosign/releases)).

```sh
cosign verify-blob \
  --bundle SHA256SUMS.txt.bundle \
  --certificate-identity-regexp "https://github.com/darkshade9/prismconductor/.github/workflows/release.yml@refs/tags/.*" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  SHA256SUMS.txt
```

A successful verification prints `Verified OK`.

### 3. Inspect the SBOM

`sbom-go.cdx.json` is a [CycloneDX](https://cyclonedx.org/) BOM in JSON format listing every Go module dependency and its resolved version. Any CycloneDX-compatible tool (e.g., Dependency-Track, `cyclonedx-cli`, `bomber`) can consume it.

```sh
# Example: list all components with bomber
bomber scan sbom-go.cdx.json
```

## Setting up signing prerequisites

The `release` GitHub Environment must be created in the repository settings with:

- **Required reviewers**: add at least one maintainer who is not the release initiator.
- **Deployment branches**: restrict to `main`.
- No long-lived secrets are needed — the release workflow uses cosign keyless signing via GitHub Actions OIDC, so no private key is stored in repository secrets.

## Remote workspaces

If you're running PrismConductor on a laptop that closes often, or working on
a big monorepo where local builds are slow, try a Remote workspace. Work
runs on Cloudflare's servers; your laptop just talks to it.

[Full setup guide → docs/remote-workspaces.md](docs/remote-workspaces.md)

Quick start:
1. Add Workspace → Remote.
2. Paste a Cloudflare API token (Workers Scripts: Edit + Account Settings: Read).
3. Paste a GitHub PAT (Contents + Pull requests: Read/Write on the target repo).
4. Click Deploy. The conductor uploads its worker bundle to your CF account.

Tokens are stored in your OS keychain (macOS Keychain / Windows Credential
Vault / Linux Secret Service). Workers run under your CF account, billed to
you. See [the security model](docs/remote-workspaces.md#threat-model) in the
full guide.

## Dependabot and auto-merge policy

Dependabot opens weekly PRs for Go modules, frontend npm packages, test npm packages, and GitHub Actions. Patch and minor version bumps are auto-merged via squash when the `build` workflow is green. Major version bumps always require human review.

To disable auto-merge, remove or edit `.github/workflows/dependabot-automerge.yml`.
