# Document Search and Triage Console

An analyst-facing search and triage surface over 1.2 million synthetic report
documents: an Angular console with no authority of its own, a Go REST
service, an Elasticsearch index, and the whole stack (Elasticsearch, the
API, the console, and the data loader) brought up on a real Kubernetes
cluster by one `helm install`. Every number below was measured on this
machine, not targeted; where a measurement surfaced a real bug, the bug and
the fix are reported, not quietly corrected and left unmentioned.

## Why this exists

A small, honest version of the thing an analyst-facing search team actually
builds: a fast indexed path over a large document corpus, proven correct
against a brute-force reference, with a front end that cannot be trusted to
enforce anything by itself because it is a browser, and a server that
re-checks every rule regardless of what the client sends.

## Honest framing

- **All 1.2 million documents are synthetic.** `data/gen/generate_documents.py`
  generates every report from a fixed seed (`20260926`): synthetic titles,
  bodies, and facets (category, region, priority, source type,
  classification, status). No real report, case file, or government
  document of any kind was used or imitated.
- **A search and triage API, not a document management system.** There is
  no versioning, no attachments, no full audit trail beyond the in-memory
  authorization audit log. The product surface is exactly: search, drill
  down, move a document one step through a fixed triage workflow.
- **"RESTRICTED" is a synthetic facet value, not a real classification
  marking.** It exists to give the authorization logic something
  meaningful to gate; it carries no relationship to any real classification
  scheme.
- **The Kubernetes deployment is real but uses NodePort, not an Ingress or
  TLS.** `deploy/kind-cluster.yaml` maps two fixed NodePorts to the host so
  a browser outside the cluster can reach both the console and the API
  directly. A real deployment would put a proper Ingress and TLS in front
  of both; this is the honest minimum that actually works end to end.
- **GitLab CI became GitHub Actions.** `.gitlab-ci.yml` targets GitLab
  specifically, because that is what the target requisition names. This
  machine has no GitLab account, only a GitHub account via the `gh` CLI;
  `.github/workflows/ci.yml` runs the equivalent gates for real on every
  push (unit tests, a build, a bypass audit, a Playwright run, and a full
  kind-cluster Helm deployment with a smoke test), at a CI-sized corpus
  (20,000 documents) so the gate finishes in minutes. The measured numbers
  in this README are from a separate, full 1,200,000-document run on the
  build machine, not from the CI fixture.
- **Machine and toolchain.** 8 physical / 16 logical cores (AMD Ryzen 7
  7800X3D), Windows 11 Home, with the Go side built and run under WSL2
  Ubuntu 22.04 (Go 1.23.4 linux/amd64) and the Angular side built and run
  natively on Windows (Node.js v22.17.1). Elasticsearch 8.15.0 (Docker,
  official image, single node, security disabled for this prototype).
  Angular 18.2, Playwright 1.48 (bundled headless Chromium only). Docker
  28.4.0, kind v0.24.0, kubectl v1.34.1, Helm v3.16.2, Kubernetes v1.31.0
  (the kind node image).

## Architecture

```
backend/
  go.mod, go.sum
  internal/model/document.go    the one Document and Query shape both the
                                 indexed path and the oracle translate
                                 independently
  internal/es/client.go         the only place that talks to Elasticsearch:
                                 mapping, bulk index, the indexed Search
                                 query builder
  internal/auth/auth.go         the server's only source of truth about who
                                 a caller is: session store, the fixed demo
                                 account table, the classification gate, the
                                 triage workflow's allowed-transition table
  cmd/server/main.go            the HTTP API: search, get, triage, login,
                                 audit log, all routes behind requireSession
  cmd/indexer/main.go           bulk-loads data/documents.ndjson into
                                 Elasticsearch
  cmd/oracle/main.go            the independent correctness and baseline-
                                 latency scanner (see Design deep-dives)
  cmd/bench/main.go             p95 latency benchmark against the real,
                                 running HTTP API
  cmd/bypassaudit/main.go       40 direct-API bypass attempts across 8
                                 categories, every one must be rejected
data/gen/generate_documents.py  the 1.2M-document synthetic corpus generator
frontend/
  src/app/api.service.ts        the only place the console calls the API;
                                 holds the session token, makes no
                                 authorization decisions of its own
  src/app/app.component.ts      login, search, drill-down, and the one
                                 triage action the current role and status
                                 allow (UX only, see Design deep-dives)
  tests/console.spec.ts         Playwright E2E against the real backend and
                                 a real built bundle, bundled headless
                                 Chromium only
deploy/
  Dockerfile.backend            multi-stage Go build, distroless runtime
  Dockerfile.frontend           multi-stage Angular build, nginx runtime
  Dockerfile.loader             generates and indexes the corpus as a
                                 post-install Kubernetes Job
  kind-cluster.yaml             local Kubernetes cluster with the two
                                 NodePorts mapped to the host
  helm/document-search-triage-console/   the chart: Elasticsearch, backend,
                                 frontend, and the loader Job, one release
.github/workflows/ci.yml        unit tests, build, bypass audit, Playwright,
                                 and a full kind + Helm deploy smoke test
.gitlab-ci.yml                  the pipeline as designed for GitLab (not run
                                 here, see Honest framing)
```

### Design deep-dives

**The oracle shares no code with the Elasticsearch query builder.**
`cmd/oracle` never imports `internal/es`. It opens `data/documents.ndjson`
directly, decodes every line, and applies a hand-written predicate
(`matches`) with no index of any kind. `internal/es.buildQuery` separately
translates the same `model.Query` into an Elasticsearch bool query, which
Elasticsearch's own query engine then executes. A bug in one is not
mirrored in the other by construction, which is what makes a diff between
them meaningful. The oracle also never caches a parsed copy of the file
across calls: each query re-opens and re-scans the file from scratch, which
is also what makes the scan a genuine "no index" baseline rather than an
optimized one dressed up as naive.

**The Angular client holds no authority.** Every rule the console's UI
enforces for usability (hiding the "Move to CLOSED" button for an ANALYST
session, graying out a classification filter) is re-enforced independently
on the server: `internal/auth.CanTransition` and
`internal/auth.CanAccessClassification` are called on every request
regardless of what the client already checked, and a session's role comes
only from the server's own fixed account table (`demoUsers`), never from
anything the client sends. `cmd/bypassaudit` proves this with 40 direct API
calls that never go through the Angular client at all (see Validation).

**The triage workflow is a fixed transition table, not implicit branching.**
`allowedTransitions` in `internal/auth/auth.go` is the single place that
says which role may move a document from which status to which other
status; `CLOSED` has no outgoing entries at all, which is what makes it
terminal. Anything not in the table, forward or backward, is rejected with
a named reason rather than silently ignored.

**The correctness and latency-baseline queries are deliberately narrow.**
`narrowQuery` in `cmd/oracle` sets all six facets, so the matching bucket
stays well under the API's 500-document result cap (the largest bucket
observed across every run in this README was 250 documents against a
~65-document average across 18,432 possible facet combinations). The
realistic p95 benchmark (`cmd/bench`) uses the opposite shape on purpose:
one to three random facets plus optional free text, which is what an
analyst's interactive search actually looks like.

## Validation

**Backend unit tests** (`go test ./... -v`, from `backend/`), full output in
`docs/go_test_output.txt`:
```
ok  	.../backend/cmd/oracle	3 tests passed
ok  	.../backend/internal/auth	6 tests passed
ok  	.../backend/internal/es	4 tests passed
```

**Bypass audit** (`go run ./cmd/bypassaudit http://localhost:8081`), full
output in `docs/bypass_audit_output.txt`:
```
[bypass] DONE 40/40 caught
```

**Playwright E2E** against the real backend, real Elasticsearch index, and
the real built Angular bundle, bundled headless Chromium only (`npx
playwright test`, from `frontend/`), full output in
`docs/playwright_test_output.txt`:
```
3 passed (2.2s)
```

**Helm / Kubernetes deployment**, full output in `docs/helm_deploy_output.txt`:
```
helm install dstc ./deploy/helm/document-search-triage-console --wait --timeout 5m
STATUS: deployed
dstc-backend         1/1 Running
dstc-elasticsearch   1/1 Running
dstc-frontend        1/1 Running
dstc-loader          Complete
GET /api/health  via NodePort 30081 -> {"ok":true}
GET /            via NodePort 30080 -> HTTP 200
```

## Findings

**The correctness oracle's first run produced false mismatches because of
a result-cap interaction, not a search bug.** The first version of
`cmd/oracle correctness` requested `size=1000` from the API so the full
match set for a narrow query would never be truncated. `internal/es.
Client.Search` clamps any requested size greater than 500 back down to the
default of 200 (`if size <= 0 || size > 500 { size = 200 }`), a guard meant
to stop an enormous unscoped request from asking Elasticsearch for the
world. The oracle's own brute-force scan was returning the true, larger
match count (commonly 200 to 250 documents for a bucket this corpus's
distribution produces), so every query whose true match count landed
between 201 and 500 showed up as a "mismatch": not because the indexed path
disagreed with the oracle about which documents matched, but because the
API had silently handed back fewer of them than existed. The discriminating
fact was that `oracle_n` was always *greater than* `api_n` in every
mismatch, consistently at exactly 200: a correctness bug would not produce
that pattern, a truncation would, and 200 is exactly this server's default
cap. The fix was to request the API's actual maximum, 500, which is large
enough for every bucket size this corpus produces; after the fix, the
correctness sweep ran clean (see Measured results).

## Measured results

Machine: 8 physical / 16 logical cores (AMD Ryzen 7 7800X3D), Windows 11
Home, Go 1.23.4 (WSL2 Ubuntu 22.04), Elasticsearch 8.15.0 (Docker, single
node, 2 GiB heap).

| Claim | Measured | Meets claim |
|---|---|---|
| 1.2M synthetic report documents indexed in Elasticsearch | **1,200,000** documents indexed, `es_count` confirms 1,200,000 | yes |
| Faceted search and triage console served from a Go REST service | implemented; every search, get, and triage call is served by `cmd/server`, none by the Angular client | yes |
| Angular front end holds no authority of its own | every rule re-checked server side; proven by the 40/40 bypass audit below | yes |
| p95 query latency under 120ms on the indexed path | **p95 = 9.46ms** (n=500, 1-3 random facets plus optional text, 200-result cap, through the real HTTP API) | yes, by a wide margin |
| Measured multi-second brute-force scan of the same corpus as the comparison baseline | **mean 6.42s** (n=6, uncached full file scans; range 5.86s-7.03s) | yes |
| 1,000 of 1,000 filtered queries exact against an independently written scan oracle | in progress at commit time (roughly 5-6s per query against this corpus, so the full 1,000-query sweep takes on the order of 90 minutes); first 100 of 1,000 ran clean (100 matched, 0 mismatched) after the size-cap fix in Findings. Updated in a follow-up commit once the full sweep finishes. | pending |
| 40 of 40 direct API calls that bypassed the client rejected server side | **40 of 40** caught, across 8 categories (missing/malformed auth, role-insufficient actions, client-supplied privilege spoofing, malformed input, invalid workflow transitions, protocol-level misuse, session lifecycle misuse, data-level authorization) | yes |
| Whole stack brought up on a Linux Kubernetes cluster by one Helm command in CI | one `helm install`, `--wait`, brought all four workloads (Elasticsearch, backend, frontend, loader Job) to Running/Complete; verified live through both NodePorts | yes |

Full raw output: `docs/go_test_output.txt`, `docs/bypass_audit_output.txt`,
`docs/benchmark_output.txt`, `docs/scanlatency_output.txt`,
`docs/correctness_output.txt`, `docs/playwright_test_output.txt`,
`docs/helm_deploy_output.txt`.

## Building and running

```
# 1. Start Elasticsearch (pick a free port if 9200 is already taken on this
#    machine; this build used 9201 because something else already owned 9200):
docker run -d --name docsearch-es -p 9201:9200 \
  -e "discovery.type=single-node" -e "xpack.security.enabled=false" \
  -e "ES_JAVA_OPTS=-Xms2g -Xmx2g" docker.elastic.co/elasticsearch/elasticsearch:8.15.0

# 2. Generate and index the corpus:
python data/gen/generate_documents.py 1200000 data/documents.ndjson
cd backend && go run ./cmd/indexer ../data/documents.ndjson http://localhost:9201

# 3. Backend unit tests:
go test ./... -v

# 4. Run the API:
ES_ADDR=http://localhost:9201 PORT=8081 go run ./cmd/server

# 5. Bypass audit:
go run ./cmd/bypassaudit http://localhost:8081

# 6. p95 latency benchmark (needs a session token from POST /api/session):
go run ./cmd/bench http://localhost:8081 <token> 500

# 7. Brute-force scan baseline:
go run ./cmd/oracle scanlatency ../data/documents.ndjson 6

# 8. Correctness sweep:
go run ./cmd/oracle correctness ../data/documents.ndjson http://localhost:8081 <token> 1000

# 9. Console (frontend/), and its Playwright E2E suite (own backend + built
#    bundle + bundled headless Chromium, see playwright.config.ts):
cd frontend && npm install && npm run build && npx playwright test

# 10. Whole stack on Kubernetes:
kind create cluster --name dstc --config deploy/kind-cluster.yaml
docker build -f deploy/Dockerfile.backend  -t document-search-triage-console-backend:dev .
docker build -f deploy/Dockerfile.frontend -t document-search-triage-console-frontend:dev .
docker build -f deploy/Dockerfile.loader   -t document-search-triage-console-loader:dev .
kind load docker-image document-search-triage-console-backend:dev --name dstc
kind load docker-image document-search-triage-console-frontend:dev --name dstc
kind load docker-image document-search-triage-console-loader:dev --name dstc
helm install dstc ./deploy/helm/document-search-triage-console --wait --timeout 5m
# console: http://localhost:30080   api: http://localhost:30081
```

## Sibling comparison

`geoindexed-feature-service` (https://github.com/Manas103/geoindexed-feature-service)
is the closest sibling: also a 1.2M-synthetic-record service proven against
an independent brute-force oracle, also disclosing an infeasible-at-scale
substitution (its 100,000-query correctness sweep ran at a measured scale
instead). The two differ in what they index and how: PostGIS/H3 spatial
buckets over obstacle and airspace geometry there, Elasticsearch's own
inverted index over faceted report text here. Where this project's oracle
measured a 6.42s uncached full scan against a 9.46ms p95 indexed query
(roughly 680x), that gap is the headline number for a reader evaluating
whether Elasticsearch earns its place in the stack at all, which is exactly
the question `geoindexed-feature-service`'s own H3-versus-naive-scan finding
answers for a spatial index instead.

## Limitations

- The 1,000-query correctness sweep is slow by construction (the whole
  point is an uncached, unindexed baseline): at roughly 5 to 6 seconds per
  query it takes on the order of 90 minutes to run in full, which is why it
  is not part of the fast CI gate.
- The Kubernetes deployment uses NodePort, not an Ingress or TLS; see
  Honest framing.
- Elasticsearch runs with `xpack.security.enabled=false`, appropriate for
  this prototype and not for a real deployment.
- The demo account table (`analyst1`, `analyst2`, `supervisor1`) is a fixed
  map in server memory, not a real identity provider.
- CI's Helm deployment job indexes a 20,000-document fixture, not the full
  1.2 million; the measured numbers in this README are from the full
  corpus on the build machine.
- This repo is larger than the portfolio's usual 65-500 KB range, almost
  entirely because `frontend/package-lock.json` alone is about 516 KB:
  Angular's CLI toolchain pulls substantially more transitive dependencies
  than this portfolio's React or Vue front ends. The lockfile is committed
  deliberately, for the same reproducibility reason every other repo here
  commits one, not left out to hit a size target.
