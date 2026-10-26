# Kargo Demo

Demo repository for the talk **"GitOps is not enough: taming environment promotion in Kubernetes with Kargo"**

Presented at [Voxxed Days Luxembourg](https://voxxeddays.com/luxembourg/) and Dutch Cloud Native Day (Utrecht, 2026).
The full step-by-step demo script is in [`kargo-demo-guide.md`](kargo-demo-guide.md).

---

## What this is

A minimal but realistic end-to-end promotion pipeline:

```
GitHub Actions (CI)
       │  builds image, pushes to GHCR with SHA digest
       ▼
  Kargo Warehouse
       │  detects new Freight (image digest)
       ▼
  Stage: dev  ──── auto-promote ──── smoke test
       │
       ▼
  Stage: staging ── auto-promote ── integration test
       │
       ▼
  Stage: prod ───── 🔐 manual approval required
       │
       ├──▶ prod-eu / prod-us   (multi-cluster, Argo CD)

  Stage: dev ──▶ Stage: qa-flux  (same Freight, deployed by Flux)
```

Who may promote where is enforced with plain Kubernetes RBAC (`rbac/`).

Everything is Kubernetes-native. No shell scripts. No Slack messages. No manual PRs.

---

## Repository structure

```
.
├── app/                        # Sample Go HTTP application
│   ├── main.go
│   └── Dockerfile
├── manifests/                  # Kustomize manifests per environment
│   ├── base/
│   ├── dev/
│   ├── staging/
│   ├── prod/
│   └── flux/
├── kargo/                      # Kargo CRDs
│   ├── warehouse.yaml
│   ├── stages.yaml
│   ├── project-config.yaml     # auto-promotion policies
│   └── analysis-template.yaml
├── flux/                       # Flux-driven Stage (no Argo CD involved)
│   ├── source.yaml             # GitRepository + Kustomization on stage/qa-flux
│   ├── receiver.yaml           # webhook Kargo calls after git-push
│   └── stage-qa-flux.yaml      # Kargo Stage writing to Git
├── rbac/                       # who may promote into which Stage
│   ├── developers.yaml
│   ├── release-managers.yaml
│   └── auditors.yaml
├── argocd/                     # Argo CD Application CRDs
│   ├── app-dev.yaml
│   ├── app-staging.yaml
│   └── app-prod.yaml
└── .github/workflows/
    └── build-push.yaml         # CI: build + push when app/ changes on main
```

---

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| `kind` | ≥ 1.28 | `brew install kind` |
| `kubectl` | latest | `brew install kubectl` |
| `helm` | ≥ 3.13 | `brew install helm` |
| `argocd` CLI | ≥ 2.10 | `brew install argocd` |
| `kargo` CLI | latest 1.x | `brew install akuity/tap/kargo` |
| `flux` CLI | ≥ 2.3 | `brew install fluxcd/tap/flux` |


## Key concepts

| Concept | What it is |
|---------|-----------|
| **Warehouse** | Watches artifact sources (GHCR). Emits Freight when a new image digest is detected. |
| **Freight** | Immutable snapshot: image SHA-256 digest + git commit. The unit of promotion. |
| **Stage** | A target environment (dev / staging / prod). Holds a subscription and the current Freight. |
| **Promotion** | The act of advancing Freight from one Stage to the next, with verification steps. |
| **AnalysisTemplate** | Argo Rollouts resource used by Kargo to run automated verification gates. |

---

## Why image digests, not tags?

Image tags like `:latest` or `:v1.2.3` are **mutable** — the same tag can point to a different image after a push. Kargo always resolves tags to their immutable SHA-256 digest at Freight creation time, so you have a guarantee that what passed dev is exactly what gets promoted to prod.


---

## References

- [Kargo documentation](https://docs.kargo.io)
- [Kargo access controls](https://docs.kargo.io/user-guide/security/access-controls)
- [Flux webhook receivers](https://fluxcd.io/flux/components/notification/receivers/)
- [Kargo GitHub](https://github.com/akuity/kargo)
- [Argo CD](https://argo-cd.readthedocs.io)
- [Argo Rollouts Analysis](https://argoproj.github.io/argo-rollouts/features/analysis)
- [Flux image automation](https://fluxcd.io/flux/guides/image-update)