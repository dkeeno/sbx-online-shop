# online-shop

First real workload on the **sbx-02 GKE Autopilot cluster**. A tiny Go HTTP server that serves a hardcoded 3-product catalog as JSON + an HTML page, exposed through the cluster's internal Gateway at path prefix `/shop/`.

This repo's job is **build the image and bump the gitops manifest**. Everything else (deployment, service, route) is owned by [`sbx-02-manifests`](../../sbx-gitops/sbx-02-manifests) and reconciled by ArgoCD.

## What the app does

| Route | Returns |
|---|---|
| `GET /shop/` | HTML catalog page rendering the 3 products with inline CSS |
| `GET /shop/api/products` | JSON array of all products |
| `GET /shop/api/products/{id}` | JSON for one product (`p1`, `p2`, `p3`) or 404 |
| `GET /shop/healthz` | `ok` (used by k8s liveness/readiness probes) |

Listens on `:8080`. No external deps — std-lib only. Three products are hardcoded in `main.go`; this is a single-pod prototype.

## File map

| File | Purpose |
|---|---|
| `main.go` | The HTTP server (~150 lines) |
| `index.html` | Embedded HTML template (rendered via `html/template` + `embed.FS`) |
| `go.mod` | Module path + Go 1.23 — no go.sum because no external deps |
| `Dockerfile` | Multi-stage: `golang:1.23-alpine` builder → `gcr.io/distroless/static-debian12:nonroot` runtime |
| `.gitlab-ci.yml` | 2-stage CI: build+push to AR → bump-manifest in sbx-02-manifests |
| `.gitignore` | Go binary, IDE noise, accidental credential files |

## CI flow (push to main)

```
git push (main)
    │
    ▼
┌──────────────────────────────────────┐
│ build-and-push                       │
│   docker:27 + dind                   │
│   • install gcloud                   │
│   • WIF auth                         │
│   • docker build (multi-stage)       │
│   • docker push to AR sbx-images     │
│     - tag :SHORT_SHA                 │
│     - tag :latest                    │
└──────────────────┬───────────────────┘
                   │
                   ▼
┌──────────────────────────────────────┐
│ bump-manifest                        │
│   alpine/git + downloaded kustomize  │
│   • clone sbx-02-manifests           │
│   • cd manifests/dev/online-shop/    │
│         overlays/dev                 │
│   • kustomize edit set image         │
│        online-shop=...:SHORT_SHA     │
│   • commit on temp branch            │
│   • push branch                      │
│   • POST /merge_requests via API     │
│   • PUT /merge to auto-merge         │
└──────────────────┬───────────────────┘
                   │
                   ▼
   (~3 min — ArgoCD git poll)
                   │
                   ▼
   ArgoCD rebuilds the dev overlay with Kustomize,
   detects the new image tag, rolls the pods.
```

Both jobs share `resource_group: online-shop-pipeline` so concurrent pipelines run **serially** (pipeline B waits for pipeline A — they don't both bump the overlay at once).

The bump touches **only** `manifests/dev/online-shop/overlays/dev/kustomization.yaml` (specifically the `newTag:` line). The Kustomize base is never modified by CI — it's authored by humans only.

## Image registry

`europe-west2-docker.pkg.dev/eco-gcp-dev-prj-sbx-01/sbx-images/online-shop:<SHA>`

Tags pushed per build:
- `:<CI_COMMIT_SHORT_SHA>` — immutable per commit, what ArgoCD reconciles to
- `:latest` — moving pointer for convenience (NOT used by the cluster)

## CI/CD variables consumed (inherited from group `test-gcp-dev-projects`)

| Variable | Used for |
|---|---|
| `GOOGLE_WORKLOAD_IDENTITY_PROVIDER` | OIDC audience for WIF token-exchange |
| `GOOGLE_SERVICE_ACCOUNT` | SA to impersonate (`agentic-ai-user-vertex`) |
| `GOOGLE_PROJECT` | `eco-gcp-dev-prj-sbx-01` (where AR + cluster live) |
| `SBX_GITOPS_TOKEN` | Group access token, `write_repository`, used to push the manifest bump back to sbx-02-manifests |

No project-level variables here — everything cascades from the group.

## Local run

```sh
go run .
# in another terminal
curl http://localhost:8080/shop/healthz
curl http://localhost:8080/shop/api/products | jq
open http://localhost:8080/shop/
```

## Reaching the deployed app

Through the cluster's internal Gateway IP `10.3.11.202` at path `/shop/`. From outside the VPC, tunnel through the bastion — see the Section 2.3 Path A pattern in `IMPORTANT-FILES/post-implementation-docs/gcp-resource-inventory-and-access.md` for the SSH-tunnel-via-Cloud-Shell flow.

```sh
# from a bastion-tunnelled session:
curl http://10.3.11.202/shop/healthz       # → ok
curl http://10.3.11.202/shop/api/products  # → [{...}, {...}, {...}]
```

## Reference

- `IMPORTANT-FILES/phase-2-gitlab-setup.md` — full Phase 2 design
- `IMPORTANT-FILES/post-implementation-docs/gcp-resource-inventory-and-access.md` — every GCP resource + how to reach it
- `../../sbx-gitops/sbx-02-manifests/manifests/dev/online-shop/` — the manifests this CI bumps
