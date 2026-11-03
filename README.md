# changelog-805-fork

Test copy of a proposed SigNoz changelog tool (SigNoz/echo-pod#805): a fork of
[chloggen](https://github.com/open-telemetry/opentelemetry-go-build-tools/tree/main/chloggen)
at `d90a13a`. Apache-2.0, see `LICENSE` and `NOTICE`.

`.github/workflows/` holds the reusable workflows that would live in SigNoz/primus.workflows:
`changelog-check.yml`, `prereleaser.yml` and `release-draft.yml`.
[crazyuploader/changelog-805-e2e](https://github.com/crazyuploader/changelog-805-e2e) calls them.

Commands: `new`, `validate`, `update`, `next`, `check`, `docs-check`, `notes`. Run `go test -race ./...`.
