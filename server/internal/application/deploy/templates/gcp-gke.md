---
id: gcp-gke
provider: gcp_gke
name: GCP GKE (Kubernetes)
summary: Build a container, push it to Artifact Registry, set the image on a Deployment and wait for the rollout, with kubectl rollout undo as rollback.
kinds: [backend, worker, monorepo]
envs: [stage, preprod, prod]
workflow_file: deploy-gke.yml
rollback_hint: kubectl rollout undo deployment/DEPLOYMENT -n NAMESPACE
required_vars:
  - key: gcp_project_id
    label: GCP project id
    example: tasktrooper-prod
  - key: gcp_region
    label: Cluster region/location
    example: europe-west1
  - key: cluster_name
    label: GKE cluster name
    example: tasktrooper-autopilot
  - key: namespace
    label: Kubernetes namespace
    example: production
  - key: deployment_name
    label: Deployment name
    example: control-plane
  - key: container_name
    label: Container name inside the pod spec
    example: control-plane
  - key: artifact_repo
    label: Artifact Registry repository
    example: containers
  - key: health_url
    label: Health check URL
    example: https://api.example.com/health
---
# GKE rolling deploy

Authentication uses Workload Identity Federation.

## Repository secrets required

- `GCP_WIF_PROVIDER`
- `GCP_DEPLOY_SA` — needs `roles/container.developer` and `roles/artifactregistry.writer`

## Workflow — `.github/workflows/{{workflow_file}}`

```yaml
name: deploy-gke-{{env}}
on:
  workflow_dispatch:

permissions:
  contents: read
  id-token: write

concurrency:
  group: deploy-{{env}}-{{deployment_name}}
  cancel-in-progress: false

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: google-github-actions/auth@v2
        with:
          workload_identity_provider: ${{ secrets.GCP_WIF_PROVIDER }}
          service_account: ${{ secrets.GCP_DEPLOY_SA }}

      - uses: google-github-actions/setup-gcloud@v2
        with:
          install_components: gke-gcloud-auth-plugin

      - name: Build and push
        run: |
          gcloud auth configure-docker {{gcp_region}}-docker.pkg.dev --quiet
          IMAGE={{gcp_region}}-docker.pkg.dev/{{gcp_project_id}}/{{artifact_repo}}/{{deployment_name}}:${{ github.sha }}
          docker build -t "$IMAGE" .
          docker push "$IMAGE"
          echo "IMAGE=$IMAGE" >> "$GITHUB_ENV"

      - name: Get cluster credentials
        run: |
          gcloud container clusters get-credentials {{cluster_name}} \
            --region {{gcp_region}} --project {{gcp_project_id}}

      - name: Roll out
        id: rollout
        run: |
          kubectl -n {{namespace}} set image deployment/{{deployment_name}} \
            {{container_name}}="$IMAGE"
          kubectl -n {{namespace}} rollout status deployment/{{deployment_name}} --timeout=5m

      - name: Smoke check
        run: curl -fsS --retry 6 --retry-delay 5 --retry-all-errors {{health_url}}

      # Undo only when the rollout actually ran: a failed build leaves the
      # Deployment untouched, and undoing it would revert a healthy service.
      - name: Roll back on failure
        if: failure() && steps.rollout.outcome != 'skipped'
        run: kubectl -n {{namespace}} rollout undo deployment/{{deployment_name}}
```

## After pushing the workflow

1. Map category `{{env}}_deploy` → `{{workflow_file}}` under Repository Settings → Pipeline.
2. Set the target's health URL to `{{health_url}}`.
3. Keep `rollout status` in the workflow: without it a crash-looping pod reports a green deploy.
