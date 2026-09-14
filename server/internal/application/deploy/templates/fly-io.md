---
id: fly-io
provider: fly
name: Fly.io
summary: Deploy with flyctl using a rolling strategy and a release health gate, rolling back to the previous image on failure.
kinds: [backend, worker]
envs: [stage, preprod, prod]
workflow_file: deploy-fly.yml
rollback_hint: flyctl deploy --app APP --image PREVIOUS_IMAGE
required_vars:
  - key: fly_app
    label: Fly app name
    example: tasktrooper-api
  - key: health_url
    label: Health check URL
    example: https://tasktrooper-api.fly.dev/health
---
# Fly.io deploy

## Repository secrets required

- `FLY_API_TOKEN`

## Workflow — `.github/workflows/{{workflow_file}}`

```yaml
name: deploy-fly-{{env}}
on:
  workflow_dispatch:

concurrency:
  group: deploy-{{env}}-{{fly_app}}
  cancel-in-progress: false

env:
  FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: superfly/flyctl-actions/setup-flyctl@master

      - name: Remember the running image
        run: |
          PREV=$(flyctl status --app {{fly_app}} --json | python3 -c "import json,sys; d=json.load(sys.stdin)['ImageDetails']; print(d['Repository']+':'+d['Tag'])" || echo "")
          if [ -z "$PREV" ]; then
            echo "::warning::could not record the running image; automatic rollback is disabled for this run"
          fi
          echo "PREVIOUS_IMAGE=$PREV" >> "$GITHUB_ENV"

      - name: Deploy
        run: flyctl deploy --app {{fly_app}} --strategy rolling --wait-timeout 300

      - name: Smoke check
        run: curl -fsS --retry 6 --retry-delay 5 --retry-all-errors {{health_url}}

      - name: Roll back on failure
        if: failure()
        run: |
          test -n "$PREVIOUS_IMAGE" && flyctl deploy --app {{fly_app}} --image "$PREVIOUS_IMAGE" --strategy immediate
```

## After pushing the workflow

1. Map category `{{env}}_deploy` → `{{workflow_file}}` under Repository Settings → Pipeline.
2. Keep `[[http_service.checks]]` in `fly.toml` aligned with `{{health_url}}`: the platform gate and the production monitor should agree on what "healthy" means.
