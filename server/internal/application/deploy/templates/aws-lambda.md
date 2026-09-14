---
id: aws-lambda
provider: aws_lambda
name: AWS Lambda (alias shift)
summary: Publish a new function version and point the environment alias at it, so rollback is a one-line alias move back to the previous version.
kinds: [backend, worker]
envs: [stage, preprod, prod]
workflow_file: deploy-lambda.yml
rollback_hint: aws lambda update-alias --function-name FN --name ALIAS --function-version PREVIOUS
required_vars:
  - key: aws_region
    label: AWS region
    example: eu-central-1
  - key: function_name
    label: Lambda function name
    example: tasktrooper-worker
  - key: alias_name
    label: Alias for this environment
    example: prod
  - key: artifact_path
    label: Build artifact (zip) path
    example: dist/function.zip
  - key: health_url
    label: Health check URL (API Gateway route or function URL)
    example: https://api.example.com/health
    required: false
---
# Lambda deploy via alias shift

Authentication uses GitHub OIDC → an IAM role.

## Repository secrets required

- `AWS_DEPLOY_ROLE_ARN` — allows `lambda:UpdateFunctionCode`, `lambda:PublishVersion`, `lambda:UpdateAlias`, `lambda:GetAlias`.

## Workflow — `.github/workflows/{{workflow_file}}`

```yaml
name: deploy-lambda-{{env}}
on:
  workflow_dispatch:

permissions:
  contents: read
  id-token: write

concurrency:
  group: deploy-{{env}}-{{function_name}}
  cancel-in-progress: false

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Build artifact
        run: make build-lambda   # must produce {{artifact_path}}

      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ secrets.AWS_DEPLOY_ROLE_ARN }}
          aws-region: {{aws_region}}

      - name: Remember the current alias version
        run: |
          PREV=$(aws lambda get-alias --function-name {{function_name}} --name {{alias_name}} \
            --query 'FunctionVersion' --output text || echo "")
          echo "PREVIOUS_VERSION=$PREV" >> "$GITHUB_ENV"

      - name: Publish new version
        run: |
          aws lambda update-function-code --function-name {{function_name}} \
            --zip-file fileb://{{artifact_path}} --publish > publish.json
          VERSION=$(python3 -c "import json;print(json.load(open('publish.json'))['Version'])")
          aws lambda wait function-updated --function-name {{function_name}}
          echo "NEW_VERSION=$VERSION" >> "$GITHUB_ENV"

      - name: Shift alias
        run: |
          aws lambda update-alias --function-name {{function_name}} \
            --name {{alias_name}} --function-version "$NEW_VERSION"

      - name: Smoke check
        run: |
          if [ -n "{{health_url}}" ]; then
            curl -fsS --retry 6 --retry-delay 5 --retry-all-errors {{health_url}}
          else
            echo "no health_url configured for this target; skipping smoke check"
          fi

      - name: Roll back alias on failure
        if: failure()
        run: |
          test -n "$PREVIOUS_VERSION" && aws lambda update-alias --function-name {{function_name}} \
            --name {{alias_name}} --function-version "$PREVIOUS_VERSION"
```

## After pushing the workflow

1. Map category `{{env}}_deploy` → `{{workflow_file}}` under Repository Settings → Pipeline.
2. Point every caller (API Gateway, EventBridge) at the **alias**, not at `$LATEST` — otherwise the rollback step moves an alias nobody uses.
