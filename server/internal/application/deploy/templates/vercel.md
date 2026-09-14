---
id: vercel
provider: vercel
name: Vercel
summary: Build with the Vercel CLI and promote the prebuilt output, using an instant alias rollback to the previous deployment on failure.
kinds: [frontend]
envs: [stage, preprod, prod]
workflow_file: deploy-vercel.yml
rollback_hint: vercel rollback DEPLOYMENT_URL --token $VERCEL_TOKEN
required_vars:
  - key: vercel_scope
    label: Vercel team/scope slug
    example: tasktrooper
  - key: vercel_project
    label: Vercel project name
    example: tasktrooper-web
  - key: health_url
    label: Health check URL
    example: https://app.example.com
---
# Vercel deploy

## Repository secrets required

- `VERCEL_TOKEN`
- `VERCEL_ORG_ID`
- `VERCEL_PROJECT_ID`

## Workflow — `.github/workflows/{{workflow_file}}`

```yaml
name: deploy-vercel-{{env}}
on:
  workflow_dispatch:

concurrency:
  group: deploy-{{env}}-{{vercel_project}}
  cancel-in-progress: false

env:
  VERCEL_ORG_ID: ${{ secrets.VERCEL_ORG_ID }}
  VERCEL_PROJECT_ID: ${{ secrets.VERCEL_PROJECT_ID }}

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22

      - name: Install Vercel CLI
        run: npm i -g vercel@latest

      # stage/preprod ship a PREVIEW deployment; only env=prod touches the
      # production alias. One recipe, but the deploy target's env decides.
      - name: Resolve deploy mode
        run: |
          if [ "{{env}}" = "prod" ]; then
            echo "VERCEL_ENVIRONMENT=production" >> "$GITHUB_ENV"
            echo "VERCEL_PROD_FLAG=--prod" >> "$GITHUB_ENV"
          else
            echo "VERCEL_ENVIRONMENT=preview" >> "$GITHUB_ENV"
            echo "VERCEL_PROD_FLAG=" >> "$GITHUB_ENV"
          fi

      - name: Remember the current production deployment
        run: |
          if [ "{{env}}" != "prod" ]; then
            echo "PREVIOUS_DEPLOYMENT=" >> "$GITHUB_ENV"
            exit 0
          fi
          PREV=$(vercel ls {{vercel_project}} --scope {{vercel_scope}} \
            --token ${{ secrets.VERCEL_TOKEN }} --prod --yes | head -n 1 | awk '{print $1}')
          echo "PREVIOUS_DEPLOYMENT=$PREV" >> "$GITHUB_ENV"

      - name: Pull environment
        run: vercel pull --yes --environment=$VERCEL_ENVIRONMENT --scope {{vercel_scope}} --token ${{ secrets.VERCEL_TOKEN }}

      - name: Build
        run: vercel build $VERCEL_PROD_FLAG --token ${{ secrets.VERCEL_TOKEN }}

      - name: Deploy prebuilt output
        run: |
          URL=$(vercel deploy --prebuilt $VERCEL_PROD_FLAG --scope {{vercel_scope}} --token ${{ secrets.VERCEL_TOKEN }})
          echo "DEPLOY_URL=$URL" >> "$GITHUB_ENV"

      - name: Smoke check
        run: |
          if [ "{{env}}" = "prod" ]; then
            curl -fsS --retry 6 --retry-delay 5 --retry-all-errors {{health_url}}
          else
            curl -fsS --retry 6 --retry-delay 5 --retry-all-errors "$DEPLOY_URL"
          fi

      - name: Roll back on failure
        if: failure()
        run: |
          if [ "{{env}}" = "prod" ] && test -n "$PREVIOUS_DEPLOYMENT"; then
            vercel rollback "$PREVIOUS_DEPLOYMENT" \
              --scope {{vercel_scope}} --token ${{ secrets.VERCEL_TOKEN }} --yes
          fi
```

## After pushing the workflow

1. Map category `{{env}}_deploy` → `{{workflow_file}}` under Repository Settings → Pipeline.
2. Disable Vercel's own Git integration for this project, or every push deploys twice and the rollback step fights the platform.
