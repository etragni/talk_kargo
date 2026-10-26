# Kargo Demo Guide
## "GitOps is not enough: taming environment promotion in Kubernetes with Kargo"
### Repo: https://github.com/etragni/talk_kargo

Used at Voxxed Days Luxembourg and Dutch Cloud Native Day. Acts 1 to 3 are the core demo; Acts 4 (Flux) and 5 (RBAC) are add-ons you can run or skip depending on the slot length.

---

## Repository layout

```
talk_kargo/
├── app/
│   ├── main.go                  # Go HTTP server, has FORCE_ERROR hook for demo
│   └── Dockerfile
├── manifests/
│   ├── base/                    # deployment.yaml + kustomization.yaml
│   ├── dev/kustomization.yaml
│   ├── staging/kustomization.yaml
│   ├── prod/kustomization.yaml
│   └── flux/kustomization.yaml  # overlay rendered by Kargo for the Flux stage
├── kargo/
│   ├── warehouse.yaml
│   ├── stages.yaml              # dev (auto), staging (auto + gates), prod (manual)
│   ├── project-config.yaml      # auto-promotion policies
│   └── analysis-template.yaml
├── argocd/
│   ├── app-dev.yaml
│   ├── app-staging.yaml
│   └── app-prod.yaml
├── flux/                        # ACT 4: same pipeline, different GitOps agent
│   ├── source.yaml              # GitRepository + Kustomization on stage/qa-flux
│   ├── receiver.yaml            # webhook Kargo calls right after git-push
│   └── stage-qa-flux.yaml       # Kargo Stage that writes to Git, never calls Argo CD
├── rbac/                        # ACT 5: who may promote into which Stage
│   ├── developers.yaml          # promote: dev, staging, qa-flux
│   ├── release-managers.yaml    # built-in kargo-promoter: every Stage
│   └── auditors.yaml            # built-in kargo-viewer: read-only
├── multi-cluster-expansion.yaml # ACT 3
├── argo-add-cluster.sh
└── .github/workflows/
    └── build-push.yml           # builds only when app/ changes
```

---

## Prerequisites

| Tool | Version | Notes |
|------|---------|-------|
| `kind` | ≥ 0.23 | demo uses kind |
| `kubectl` | latest | |
| `helm` | ≥ 3.14 | |
| `argocd` CLI | ≥ 2.10 | |
| `kargo` CLI | latest 1.x | `brew install akuity/tap/kargo` |
| `flux` CLI | ≥ 2.3 | `brew install fluxcd/tap/flux` (Act 4 only) |
| GitHub account | | GHCR + Actions, plus a fine-grained PAT with **Contents: read & write** on `talk_kargo` (Act 4 only) |

---

## Step 1: Bootstrap the cluster

> ⚠️ Run this entirely **before** the talk. It takes about 5 minutes.

```bash
# 1a. Create a kind cluster
kind create cluster --name kargo-demo --config - <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
  - role: worker
EOF

# 1b. Install Argo CD
kubectl create namespace argocd
helm repo add argo https://argoproj.github.io/argo-helm
helm upgrade --install argocd argo/argo-cd \
  --namespace argocd \
  --set server.service.type=NodePort \
  --wait

# 1c. Install Argo Rollouts (needed for AnalysisRuns in prod verification)
kubectl create namespace argo-rollouts
helm upgrade --install argo-rollouts argo/argo-rollouts \
  --namespace argo-rollouts \
  --wait

# 1d. Install cert-manager (Kargo dependency)
helm repo add jetstack https://charts.jetstack.io --force-update
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true \
  --wait

# 1e. Install Kargo
pass=devoteam1234
echo "Password: $pass"
hashed_pass=$(htpasswd -bnBC 10 "" $pass | tr -d ':\n')
signing_key=$(openssl rand -base64 48 | tr -d "=+/" | head -c 32)

helm upgrade --install kargo \
  oci://ghcr.io/akuity/kargo-charts/kargo \
  --namespace kargo \
  --create-namespace \
  --set api.adminAccount.passwordHash=$hashed_pass \
  --set api.adminAccount.tokenSigningKey=$signing_key \
  --set api.service.type=NodePort \
  --wait

echo "✅ Cluster ready"
kubectl get pods -A | grep -E 'argocd|kargo|argo-rollouts' | grep Running
```

Expand the fleet (needed for Act 3):

```bash
# 1f. Create the extra target clusters
kind create cluster --name kargo-eu
kind create cluster --name kargo-us

# Switch context back to the management cluster first
kubectl config use-context kind-kargo-demo

# Port-forward Argo CD in the background
kubectl port-forward svc/argocd-server -n argocd 8080:443 &

ARGOCD_PASS=$(kubectl -n argocd get secret argocd-initial-admin-secret \
  -o jsonpath="{.data.password}" | base64 -d)
argocd login localhost:8080 --username admin --password $ARGOCD_PASS --insecure

# Register the regional clusters in Argo CD
chmod +x argo-add-cluster.sh
./argo-add-cluster.sh
```

---

## Step 2: Create namespaces and Argo CD Applications

```bash
kubectl create namespace demo-dev
kubectl create namespace demo-staging
kubectl create namespace demo-prod
kubectl create namespace demo-flux      # used by Act 4, deployed by Flux

kubectl apply -f argocd/
kubectl get applications -A
```

---

## Step 3: Create the Kargo Project and resources

```bash
# The Project also creates the kargo-demo namespace
kubectl apply -f - <<EOF
apiVersion: kargo.akuity.io/v1alpha1
kind: Project
metadata:
  name: kargo-demo
EOF

kubectl wait --for=jsonpath='{.status.phase}'=Ready \
  project/kargo-demo --timeout=30s

# Warehouse, Stages, AnalysisTemplate and ProjectConfig
kubectl apply -f kargo/
kubectl get warehouses,stages -n kargo-demo
```

---

## Step 4: Auto-promotion policies

Applied in Step 3 via `kargo/project-config.yaml`. Dev, staging and qa-flux auto-promote; prod waits for a human. Policies for Stages that don't exist yet (`qa-flux`, `prod-eu`, `prod-us`) are harmless and kick in when those Stages are created.

```bash
kubectl get projectconfig kargo-demo -n kargo-demo -o yaml
```

---

## Step 5: Authorize Kargo to update Argo CD Applications

```bash
kubectl annotate application demo-dev -n argocd \
  kargo.akuity.io/authorized-stage="kargo-demo:dev"

kubectl annotate application demo-staging -n argocd \
  kargo.akuity.io/authorized-stage="kargo-demo:staging"

kubectl annotate application demo-prod -n argocd \
  kargo.akuity.io/authorized-stage="kargo-demo:prod"
```

---

## Step 6: Log in to the Kargo CLI

```bash
kubectl port-forward svc/kargo-api -n kargo 8443:443 &

kargo login https://localhost:8443 \
  --admin \
  --password devoteam1234 \
  --insecure-skip-tls-verify
```

Open the UI at **https://localhost:8443** and keep it visible during the demo.

---

## Step 7: Install Flux (Act 4 only)

Flux runs in the same management cluster, next to Argo CD. They don't interfere: Argo CD watches `main`, Flux only watches the `stage/qa-flux` branch.

```bash
flux check --pre
flux install            # source, kustomize, helm and notification controllers
kubectl get pods -n flux-system
```

---

## Step 8: Give Kargo write access to Git (Act 4 only)

The Argo CD stages never write to Git (`argocd-update` overrides the image on the Application). The Flux stage does: Kargo renders the manifests and pushes them to `stage/qa-flux`. That needs credentials.

```bash
export GITHUB_PAT=<fine-grained PAT, Contents: read & write on talk_kargo>

kubectl create secret generic github-creds -n kargo-demo \
  --from-literal=repoURL=https://github.com/etragni/talk_kargo \
  --from-literal=username=etragni \
  --from-literal=password=$GITHUB_PAT

kubectl label secret github-creds -n kargo-demo \
  kargo.akuity.io/cred-type=git
```

---

## Step 9: Wire Flux to Kargo (Act 4 only)

```bash
# 9a. Token for the webhook receiver
kubectl -n flux-system create secret generic kargo-receiver-token \
  --from-literal=token=$(openssl rand -hex 16)

# 9b. What Flux deploys, and the webhook Kargo will call
kubectl apply -f flux/source.yaml -f flux/receiver.yaml

# 9c. The receiver publishes its (secret) path in its status
kubectl -n flux-system wait receiver/kargo --for=condition=Ready --timeout=60s
HOOK=$(kubectl -n flux-system get receiver kargo \
  -o jsonpath='{.status.webhookPath}' | sed 's#^/hook/##')
echo "webhook path: $HOOK"
```

Don't apply the Stage yet: creating it live is the point of Act 4. The `GitRepository` shows "not ready" until the first promotion creates the branch. That's expected.

---

## Step 10: Prepare RBAC (Act 5 only)

Nothing to install. `rbac/` is applied live in Act 5. It maps three classes of user, via their OIDC `groups` claim, to ServiceAccounts:

| Who | Group claim | dev | staging | qa-flux | prod | Change the pipeline |
|-----|-------------|-----|---------|---------|------|---------------------|
| Developers | `developers` | ✅ | ✅ | ✅ | ❌ | ❌ |
| Release managers | `release-managers` | ✅ | ✅ | ✅ | ✅ | ❌ |
| Auditors | `auditors` | ❌ | ❌ | ❌ | ❌ | ❌ (read-only) |

The demo uses the admin account, so there is no OIDC. We prove the rules by **impersonating** the ServiceAccounts with `kubectl --as`, which hits exactly the same RBAC the Kargo API server evaluates for an SSO user mapped to them.

---

## Step 11: THE DEMO (run live on stage)

Five acts. Each one shows a different Kargo behaviour.

Keep two terminals open:
- **T1:** `watch -n 2 'kargo get stages -p kargo-demo'`
- **T2:** for commands

---

### 🟢 ACT 1: Happy path, dev auto-promotes

**Narration:** *"A developer pushes a change. CI builds the image and pushes it to GHCR. Kargo's Warehouse detects the new digest and creates a Freight object. Dev has auto-promotion enabled, so it gets the new version immediately, no human involved."*

```bash
# Bump the version and push. CI only runs on changes under app/
sed -i '' -E 's/version = "v[0-9.]+"/version = "v1.2.0"/' app/main.go
git add app/main.go
git commit -m "feat: bump to v1.2.0"
git push origin main
```

**What to show in the UI:**
- GitHub Actions: image being built and pushed
- Kargo UI: Warehouse detects new Freight
- dev Stage: auto-promotion runs `argocd-update` → `argocd-wait` → `smoke-test`
- dev turns green ✅, then staging follows with its two `http` gates

---

### 🔐 ACT 2: Prod requires human approval

**Narration:** *"Staging is green. The Freight is ready. But prod won't move on its own: it's waiting for a human decision. That's intentional, and no pipeline can override it."*

**Show the UI:** prod has eligible Freight but does not auto-promote.

```bash
kubectl get freights -n kargo-demo

# Replace FREIGHT_NAME with the Freight that passed staging
kargo approve --freight FREIGHT_NAME --stage prod -p kargo-demo
kargo promote --freight FREIGHT_NAME --stage prod -p kargo-demo

kargo get promotions -p kargo-demo --stage prod -w
```

**Or in the UI:** prod Stage → **Promote** → pick the Freight.

**Show the immutable digest in prod:**

```bash
for env in dev staging prod; do
  echo "=== $env ==="
  kubectl get deployment -n demo-$env \
    -o jsonpath='{.items[0].spec.template.spec.containers[0].image}'
  echo
done
```

**Narration:** *"Same digest in all three environments. What passed dev is exactly what runs in prod, byte for byte."*

---

### 🌍 ACT 3: Global expansion (multi-cluster promotion)

**Narration:** *"So far we've promoted across namespaces in one cluster. In reality, production is a fleet. Let's add an EU and a US cluster to the pipeline, live."*

```bash
kubectl --context kind-kargo-eu create namespace demo-prod
kubectl --context kind-kargo-us create namespace demo-prod
kubectl config use-context kind-kargo-demo

kubectl apply -f multi-cluster-expansion.yaml
```

**What to show in the UI:**
- The topology updates dynamically: prod now forks to prod-eu and prod-us.
- They subscribe to prod, so they immediately see prod's verified Freight and auto-promote (policy from `project-config.yaml`).

```bash
kubectl --context kind-kargo-eu get pods -n demo-prod
kubectl --context kind-kargo-us get pods -n demo-prod
kubectl --context kind-kargo-eu get deployment prod-demo-app -n demo-prod \
  -o jsonpath='{.spec.template.spec.containers[0].image}'
```

**Narration:** *"The exact digest that passed staging now runs on three separate clusters. One source of truth, the same artifact across the fleet."*

---

### 🔁 ACT 4: Same pipeline, different GitOps agent (Flux)

**Narration:** *"Everything so far was Argo CD. But Kargo doesn't deploy anything itself: it decides what each Stage should run, writes that to Git, and lets a GitOps agent reconcile it. So let's add a Stage that's deployed by Flux, with the same Warehouse, the same Freight and the same gates."*

```bash
# Create the Flux-backed Stage, injecting the receiver path from Step 9
sed "s#REPLACE_WITH_WEBHOOK_PATH#$HOOK#" flux/stage-qa-flux.yaml \
  | kubectl apply -f -
```

**What to show:**
1. **Kargo UI:** a new `qa-flux` Stage appears next to staging, fed by dev. Auto-promotion picks up the Freight that passed dev.
2. **The promotion steps:** `git-clone` → `kustomize-set-image` (digest) → `kustomize-build` → `git-commit` → `git-push` → `http` (Flux webhook) → `http` (wait healthy). No `argocd-*` step anywhere.
3. **GitHub:** open the `stage/qa-flux` branch, one commit per promotion, plain rendered YAML with `@sha256:` in the image.
4. **Flux:**
   ```bash
   flux get sources git -n flux-system
   flux get kustomizations -n flux-system
   kubectl get pods -n demo-flux
   ```
5. **Same digest, different agent:**
   ```bash
   for ns in demo-staging demo-flux; do
     echo "=== $ns ==="
     kubectl get deployment -n $ns \
       -o jsonpath='{.items[0].spec.template.spec.containers[0].image}'
     echo
   done
   ```

**Narration:** *"Argo CD on one side, Flux on the other, the same Freight in both. Kargo owns the promotion; who does the deploying is your choice. With Argo CD you get extras like the sync and health integration, but with Flux, or anything that reconciles from Git, the pipeline is identical."*

---

### 🛡️ ACT 5: Who is allowed to promote? (RBAC)

**Narration:** *"Prod waited for a human in Act 2. But which human? Today I'm admin, so I can do anything. In a real team, developers ship to dev and staging, release managers ship to prod, auditors only look. Kargo encodes that in plain Kubernetes RBAC, with one extra verb: `promote`."*

```bash
kubectl apply -f rbac/
kargo get roles --project kargo-demo      # built-in roles + the three new ones
```

**Ask Kubernetes, as each persona:**

```bash
SA=system:serviceaccount:kargo-demo

kubectl auth can-i promote stages/staging -n kargo-demo --as $SA:dev-promoter     # yes
kubectl auth can-i promote stages/prod    -n kargo-demo --as $SA:dev-promoter     # no
kubectl auth can-i promote stages/prod    -n kargo-demo --as $SA:release-manager  # yes
kubectl auth can-i update  stages/prod    -n kargo-demo --as $SA:release-manager  # no, can ship, can't rewire
kubectl auth can-i promote stages/prod    -n kargo-demo --as $SA:auditor          # no
```

**Now actually try it.** The developer *is* allowed to create Promotions, but Kargo's admission webhook checks `promote` on the **target Stage**. `--dry-run=server` runs the full admission chain without promoting anything:

```bash
FREIGHT=FREIGHT_NAME   # a Freight eligible for prod, from: kubectl get freights -n kargo-demo

cat <<EOF > /tmp/promo-prod.yaml
apiVersion: kargo.akuity.io/v1alpha1
kind: Promotion
metadata:
  generateName: prod-
  namespace: kargo-demo
spec:
  stage: prod
  freight: $FREIGHT
EOF

# Developer → rejected
kubectl create --dry-run=server -f /tmp/promo-prod.yaml --as $SA:dev-promoter

# Release manager → accepted
kubectl create --dry-run=server -f /tmp/promo-prod.yaml --as $SA:release-manager
```

**Narration:** *"Same request, two people, two answers. And this isn't a UI feature: UI, CLI and kubectl all go through the same rules. No side doors. The policy lives in Git next to the Stages, reviewed like any other change."*

**For the Q&A, how it works with real users:** the operator configures OIDC on the Kargo API server. On every request, Kargo reads the user's claims (`sub`, `email`, `groups`) and finds every ServiceAccount whose `rbac.kargo.akuity.io/claims` annotation matches. The effective permissions are the union of those ServiceAccounts' roles. Docs: https://docs.kargo.io/user-guide/security/access-controls

---

## Step 12: Timing guide (25 min, deck of 17 slides)

| Segment | Duration | Slides |
|---------|----------|--------|
| Intro + the promotion gap | 3 min | 1–5 |
| Promotion vs deployment, patterns, Kargo concepts | 4 min | 6–9 |
| Beyond Argo CD + access control | 4 min | 10–12 |
| **Act 1** happy path | 3 min | 13–14 |
| **Act 2** prod approval | 2 min | 14 |
| **Act 3** multi-cluster *(or)* **Act 4** Flux | 3 min | 14 |
| **Act 5** RBAC | 2 min | 14 |
| Comparison + takeaways + Q&A | 4 min | 15–17 |

> **In 25 minutes you can't run all five acts.** Pick Act 3 *or* Act 4 based on the audience. Act 5 is short and lands well right after Act 2.
> In a 40-minute slot, run everything.
> Steps 1–10 must be done before the talk. During the talk you only run Step 11.
> Do a full dry run the night before so images are cached and the cluster is warm. For Act 4, run one promotion during the dry run so the `stage/qa-flux` branch already exists (then delete the Stage with `kubectl delete stage qa-flux -n kargo-demo` and re-create it live).

---

## Step 13: Useful show-and-tell commands

```bash
# The Freight object, note the immutable digest
kubectl get freight -n kargo-demo -o yaml | grep -A 5 'images:'

# All Promotions and their status
kubectl get promotions -n kargo-demo

# Argo CD syncing to the digest
kubectl get application demo-prod -n argocd -o jsonpath='{.status.sync.revision}'

# What runs everywhere, Argo CD and Flux
for ns in demo-dev demo-staging demo-prod demo-flux; do
  echo "=== $ns ==="
  kubectl get deployment -n $ns \
    -o jsonpath='{.items[0].spec.template.spec.containers[0].image}'
  echo
done

# The commits Kargo made for the Flux stage
git fetch origin stage/qa-flux && git log --oneline origin/stage/qa-flux | head

# The RoleBindings that define who may promote
kubectl get rolebindings -n kargo-demo -o wide
```

---

## Troubleshooting cheat sheet

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Warehouse shows no Freight | GHCR credentials missing | `kubectl create secret docker-registry ghcr-creds -n kargo-demo --docker-server=ghcr.io --docker-username=<user> --docker-password=<PAT>` |
| New Freight on every manifest commit | CI building on every push | `build-push.yml` now only triggers on `app/**`, pull the latest workflow |
| Argo CD app stuck OutOfSync | Image override not written | `kargo get promotions -p kargo-demo` for step errors |
| Stage stuck `Verifying` | AnalysisRun failed | `kubectl get analysisruns -n kargo-demo`, check metric URL reachability |
| prod not moving | Expected! | `kargo promote --freight <n> --stage prod -p kargo-demo` |
| Promotion failed on Argo CD step | Missing authorization annotation | Re-run Step 5 |
| `http` step fails on staging | `FORCE_ERROR=true` still in kustomization | `git revert` the patch commit |
| qa-flux fails at `git-push` | No or wrong Git credentials | Check `github-creds` has the `kargo.akuity.io/cred-type=git` label and the PAT has Contents: write |
| qa-flux fails at `flux-reconcile` | Wrong webhook path | Re-read `$HOOK` (Step 9c) and re-apply the Stage with `sed` |
| qa-flux stuck at `wait-healthy` | Flux has not deployed yet | `flux get kustomizations -A`, `flux logs --kind=Kustomization` |
| `GitRepository` not ready | Branch `stage/qa-flux` not created yet | Expected until the first qa-flux promotion |
| `kubectl --as` says forbidden for everything | Your kubeconfig user can't impersonate | Use the kind admin context (`kind-kargo-demo`) |

---

## References

- **Kargo docs:** https://docs.kargo.io
- **Kargo access controls:** https://docs.kargo.io/user-guide/security/access-controls
- **Kargo promotion steps:** https://docs.kargo.io/user-guide/reference-docs/promotion-steps/kustomize-set-image
- **Kargo GitHub:** https://github.com/akuity/kargo
- **Argo CD:** https://argo-cd.readthedocs.io
- **Argo Rollouts Analysis:** https://argoproj.github.io/argo-rollouts/features/analysis
- **Flux webhook receivers:** https://fluxcd.io/flux/components/notification/receivers/
