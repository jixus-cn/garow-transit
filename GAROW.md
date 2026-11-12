# Garow deployment branch

This branch preserves the native new-api application and its upstream attribution.

- Upstream release: `v1.0.0-rc.49`, commit `48a6c87f9e7757fdc58e5be9bcb4f8281ad0dfd0`.
- Feature: QuantumNous/new-api PR [#8079](https://github.com/QuantumNous/new-api/pull/8079), head `9343178395ed922009d640174abb34eb731b53bc`.
- `main` tracks the original upstream baseline; `garow/main` is the deployment branch. Upstream changes are reviewed at selected major releases, with urgent security fixes handled separately.
- The concurrency limit is shared by every API key of a user. Zero means unlimited; excess requests receive HTTP 429. Configure existing users individually or set the new-user default. Async job concurrency covers in-flight submission, not all unfinished vendor jobs.

## Merge adaptations

Retained release cache HMAC/expiry validation, numeric quota repair, and Redis cluster hash tags alongside PR concurrency version floors. Included the concurrency version in the signed payload and advanced the cache schema. Added a regression test that fails when the version is unsigned. Adapted the PR plugin query test to the release's RouteGroup and credential middleware layout. The migration fixture was verified byte-for-byte after whitespace normalization against the rc.49 User schema.

## Validation

Validated on Debian 12 x86_64 using Go 1.26.10 and Bun 1.4.4.

- `GOWORK=off go test -p 2 -timeout 8m ./...` passed, with `TEST_REDIS_ADDR` pointing to real Redis 7. MySQL/PostgreSQL matrix tests run separately to avoid shared database fixture collisions.
- `go test -p 2 -v -run TestMigrationSchemaStability -count=1 ./model` passed with `TEST_MYSQL_DSN` and `TEST_POSTGRES_DSN`: SQLite 3.50.4, MySQL 8.0.46, PostgreSQL 17.11. Covered fresh schema, rc.49 upgrade, existing concurrency field upgrade, preservation of user data/indexes/uniqueness, and two migration passes.
- Model tests with both database DSNs passed, including authentication cache integrity across all three engines.
- `bun check -b`; the three affected Vitest files (40 tests); oxlint on all 11 affected TS/TSX files; and production frontend build passed. Full-repository lint has pre-existing errors outside this change.
- Production Go binary built with CGO disabled, `GOEXPERIMENT=greenteagc`, upstream moejs PGO profile, and version `v1.0.0-rc.49-garow.1`.

No production upstream provider credentials were configured and no paid inference calls were made. Production configuration and secrets are maintained outside this repository.
