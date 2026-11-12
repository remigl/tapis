# Code signing policy

## All platforms: Sigstore build provenance (active)

Every file of every release is accompanied by a signed SLSA build-provenance attestation, created by GitHub Actions with Sigstore keyless signing. It proves which workflow, repository and commit produced the file. Verify with:

```sh
gh attestation verify <file> --repo remigl/tapis
```

`SHA256SUMS` in each release lists the checksums of all files.

## Windows: Authenticode (prepared, pending approval)

Windows SmartScreen only trusts executables signed with a code-signing certificate from a public authority. Tapis applies to the [SignPath Foundation](https://signpath.org) program, which provides such certificates for free to open-source projects. Once approved:

> Free code signing provided by [SignPath.io](https://signpath.io), certificate by [SignPath Foundation](https://signpath.org).

- Only binaries built by this repository's release workflow from a public tag are submitted for signing.
- Committers and reviewers: [Rémi Gladieu](https://github.com/remigl). Approver: Rémi Gladieu.
- Signed binaries will show "SignPath Foundation" as publisher.

### Enabling it (maintainer)

1. Apply at https://signpath.org/apply with this repository.
2. In SignPath, create the project with a signing policy named `release-signing` and link the GitHub repository as trusted build system.
3. In GitHub, *Settings → Secrets and variables → Actions*:
   - variable `SIGNPATH_ORGANIZATION_ID`, variable `SIGNPATH_PROJECT_SLUG`,
   - secret `SIGNPATH_API_TOKEN`.
4. Publish a release: the workflow signs the `.exe` files automatically.

## Privacy

Tapis does not collect or send any data. It never connects to the Internet.
