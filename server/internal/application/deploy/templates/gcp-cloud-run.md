---
id: gcp-cloud-run
provider: gcp_cloud_run
name: GCP Cloud Run
summary: Build a container, push it to Artifact Registry and roll it out to a Cloud Run service, with a smoke check and revision-traffic rollback.
kinds: [backend, worker, monorepo]
envs: [stage, preprod, prod]
workflow_file: deploy-cloud-run.yml
rollback_hint: gcloud run services update-traffic SERVICE --region REGION --to-revisions PREVIOUS_REVISION=100
required_vars:
  - key: gcp_project_id
    label: GCP project id
    example: tasktrooper-prod
  - key: gcp_region
    label: Region
    example: europe-west1
  - key: artifact_repo
    label: Artifact Registry repository
    example: containers
  - key: service_name
    label: Cloud Run service name
    example: tasktrooper-api
  - key: health_url
    label: Health check URL
    example: https://api.example.com/health
---
# Cloud Run deploy

Authentication uses Workload Identity Federation — no long-lived service account keys.

## Repository secrets required

- `GCP_WIF_PROVIDER` — full provider resource name (`projects/…/locations/global/workloadIdentityPools/…/providers/…`)
- `GCP_DEPLOY_SA` — deployer service account email

The deployer SA needs `roles/run.admin`, `roles/artifactregistry.writer` and `roles/iam.serviceAccountUser`.

## Workflow — `.github/workflows/{{workflow_file}}`

```yaml
name: deploy-cloud-run-{{env}}
on:
  workflow_dispatch:

permissions:
  contents: read
  id-token: write

concurrency:
  group: deploy-{{env}}-{{service_name}}
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

      - name: Configure docker auth
        run: gcloud auth configure-docker {{gcp_region}}-docker.pkg.dev --quiet

      - name: Build and push
        run: |
          IMAGE={{gcp_region}}-docker.pkg.dev/{{gcp_project_id}}/{{artifact_repo}}/{{service_name}}:${{ github.sha }}
          docker build -t "$IMAGE" .
          docker push "$IMAGE"
          echo "IMAGE=$IMAGE" >> "$GITHUB_ENV"

      - name: Remember the serving revision
        run: |
          PREV=$(gcloud run services describe {{service_name}} \
            --region {{gcp_region}} --project {{gcp_project_id}} \
            --format='value(status.latestReadyRevisionName)' 2>/dev/null || echo "")
          echo "PREVIOUS_REVISION=$PREV" >> "$GITHUB_ENV"

      - name: Deploy revision
        id: deploy
        run: |
          gcloud run deploy {{service_name}} \
            --image "$IMAGE" \
            --region {{gcp_region}} \
            --project {{gcp_project_id}} \
            --quiet

      - name: Smoke check
        run: curl -fsS --retry 6 --retry-delay 5 --retry-all-errors {{health_url}}

      # Roll back only when the deploy itself ran: a failed build must not
      # repin traffic — the snapshot taken BEFORE the deploy is the truth, not
      # whatever "second newest revision" means after a partial rollout.
      - name: Roll back on failed smoke check
        if: failure() && steps.deploy.outcome != 'skipped'
        run: |
          test -n "$PREVIOUS_REVISION" && gcloud run services update-traffic {{service_name}} \
            --region {{gcp_region}} --project {{gcp_project_id}} --to-revisions "$PREVIOUS_REVISION=100"
```

## After pushing the workflow

1. Save the mapping under Repository Settings → Pipeline: category `{{env}}_deploy` → workflow file `{{workflow_file}}`.
2. Set the target's health URL to `{{health_url}}` so the production monitor probes this environment.
3. Verify a manual dispatch succeeds once before relying on the automatic release step.
