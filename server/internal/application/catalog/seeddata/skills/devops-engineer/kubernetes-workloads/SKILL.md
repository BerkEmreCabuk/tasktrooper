---
name: kubernetes-workloads
category: infrastructure
description: Use when writing or debugging Kubernetes manifests, Helm charts or Kustomize overlays - probes, resources, rollout strategy, config and secrets, ingress, and how to read a failing rollout
tech_stack: Kubernetes
---

# Kubernetes Workloads

## Overview

**Core principle:** Every workload declares what it needs (resources), how to tell it is healthy (probes), and how it is replaced (rollout strategy). A Deployment missing those three is the cause of most "it works until it doesn't" clusters.

## The shape of a service

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: shop-api, labels: { app.kubernetes.io/name: shop-api } }
spec:
  replicas: 2                       # 1 replica means downtime on every deploy
  strategy:
    rollingUpdate: { maxSurge: 1, maxUnavailable: 0 }
  selector: { matchLabels: { app.kubernetes.io/name: shop-api } }
  template:
    metadata: { labels: { app.kubernetes.io/name: shop-api } }
    spec:
      securityContext: { runAsNonRoot: true, seccompProfile: { type: RuntimeDefault } }
      containers:
        - name: api
          image: registry.example.com/shop-api:1.4.2   # a real tag or digest, never :latest
          ports: [{ containerPort: 8080 }]
          envFrom:
            - configMapRef: { name: shop-api-config }
            - secretRef: { name: shop-api-secrets }     # created out of band, never committed
          resources:
            requests: { cpu: 100m, memory: 256Mi }      # what the scheduler reserves
            limits:   { memory: 512Mi }                 # CPU limits throttle; usually omit
          startupProbe:   { httpGet: { path: /health/live,  port: 8080 }, failureThreshold: 30, periodSeconds: 2 }
          readinessProbe: { httpGet: { path: /health/ready, port: 8080 }, periodSeconds: 10 }
          livenessProbe:  { httpGet: { path: /health/live,  port: 8080 }, periodSeconds: 10 }
```

- **Readiness ≠ liveness.** Readiness means "send me traffic"; it may depend on the database. Liveness means "I am not wedged"; it must NOT depend on a dependency, or one slow database restarts every pod in the cluster.
- **Startup probe** covers a slow boot so liveness can stay tight afterwards.
- `maxUnavailable: 0` plus a `PodDisruptionBudget` is what makes a rollout and a node drain non-disruptive.
- Config in a ConfigMap, credentials in a Secret the cluster already holds (see secrets-and-key-vaults). Roll pods on config change with a checksum annotation, or the new values never load.

## Helm / Kustomize

Follow whichever the repository already uses. Kustomize: one `base/` plus one overlay per environment, differences expressed as patches — never a forked copy of the base. Helm: values per environment, `helm lint` and `helm template` before install, and `--atomic --timeout 5m` so a failed release rolls itself back (`--atomic` already implies `--wait`, and `--timeout` needs a duration).

## Debugging a failing rollout

```bash
kubectl rollout status deploy/shop-api --timeout=120s
kubectl describe pod -l app.kubernetes.io/name=shop-api   # Events explain Pending/CrashLoop
kubectl logs -l app.kubernetes.io/name=shop-api --previous --tail=200
kubectl events --for deploy/shop-api                      # or: kubectl get events --sort-by=.lastTimestamp
```

| Symptom | Usual cause |
|---|---|
| `Pending` | No node fits the requests, or no volume can be bound |
| `CrashLoopBackOff` | The app exits on start — read `--previous` logs, usually config or a missing secret |
| `ImagePullBackOff` | Wrong tag, or the pull secret is missing/expired |
| `OOMKilled` | Memory limit below real usage — measure, then raise |
| Rollout stuck at N/M | Readiness never passes; the probe path or port is wrong |

## Verify

`kubectl apply --dry-run=server -f` (server-side validation catches schema and admission errors), `kubectl rollout status` after applying, and the service answering through its ingress. Roll back with `kubectl rollout undo deploy/<name>` — know it before you deploy.

## Red flags

- `imagePullPolicy: Always` with `:latest` — nobody can say what is running.
- Secrets base64'd into a committed manifest (base64 is not encryption).
- No resource requests: the scheduler packs the node and everything degrades together.
- `kubectl edit` on a live object — the change dies at the next apply and is documented nowhere.
