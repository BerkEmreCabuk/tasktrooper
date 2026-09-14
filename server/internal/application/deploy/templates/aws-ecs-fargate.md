---
id: aws-ecs-fargate
provider: aws_ecs
name: AWS ECS Fargate
summary: Build to ECR, register a new task definition revision, update the ECS service and wait for stability, rolling back to the previous revision on failure.
kinds: [backend, worker, monorepo]
envs: [stage, preprod, prod]
workflow_file: deploy-ecs.yml
rollback_hint: aws ecs update-service --cluster CLUSTER --service SERVICE --task-definition PREVIOUS_REVISION
required_vars:
  - key: aws_region
    label: AWS region
    example: eu-central-1
  - key: ecr_repository
    label: ECR repository name
    example: tasktrooper-api
  - key: ecs_cluster
    label: ECS cluster name
    example: tasktrooper-prod
  - key: ecs_service
    label: ECS service name
    example: api
  - key: task_family
    label: Task definition family
    example: tasktrooper-api
  - key: container_name
    label: Container name in the task definition
    example: api
  - key: health_url
    label: Health check URL
    example: https://api.example.com/health
---
# ECS Fargate deploy

Authentication uses GitHub OIDC → an IAM role; no static access keys.

## Repository secrets required

- `AWS_DEPLOY_ROLE_ARN` — role trusted by `token.actions.githubusercontent.com`, with ECR push, `ecs:RegisterTaskDefinition`, `ecs:UpdateService`, `ecs:DescribeServices` and `iam:PassRole` for the task roles.

## Workflow — `.github/workflows/{{workflow_file}}`

```yaml
name: deploy-ecs-{{env}}
on:
  workflow_dispatch:

permissions:
  contents: read
  id-token: write

concurrency:
  group: deploy-{{env}}-{{ecs_service}}
  cancel-in-progress: false

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ secrets.AWS_DEPLOY_ROLE_ARN }}
          aws-region: {{aws_region}}

      - id: ecr
        uses: aws-actions/amazon-ecr-login@v2

      - name: Build and push
        run: |
          IMAGE=${{ steps.ecr.outputs.registry }}/{{ecr_repository}}:${{ github.sha }}
          docker build -t "$IMAGE" .
          docker push "$IMAGE"
          echo "IMAGE=$IMAGE" >> "$GITHUB_ENV"

      - name: Remember the current task definition
        run: |
          CURRENT=$(aws ecs describe-services --cluster {{ecs_cluster}} --services {{ecs_service}} \
            --query 'services[0].taskDefinition' --output text)
          echo "PREVIOUS_TASKDEF=$CURRENT" >> "$GITHUB_ENV"

      - id: taskdef
        name: Render new task definition
        run: |
          aws ecs describe-task-definition --task-definition {{task_family}} \
            --query 'taskDefinition' --output json > taskdef.json
          python3 - <<'PY'
          import json
          d = json.load(open('taskdef.json'))
          for k in ('taskDefinitionArn','revision','status','requiresAttributes',
                    'compatibilities','registeredAt','registeredBy','deregisteredAt'):
              d.pop(k, None)
          import os
          for c in d['containerDefinitions']:
              if c['name'] == '{{container_name}}':
                  c['image'] = os.environ['IMAGE']
          json.dump(d, open('taskdef.new.json','w'))
          PY
          ARN=$(aws ecs register-task-definition --cli-input-json file://taskdef.new.json \
            --query 'taskDefinition.taskDefinitionArn' --output text)
          echo "arn=$ARN" >> "$GITHUB_OUTPUT"

      - name: Update service
        run: |
          aws ecs update-service --cluster {{ecs_cluster}} --service {{ecs_service}} \
            --task-definition ${{ steps.taskdef.outputs.arn }}
          aws ecs wait services-stable --cluster {{ecs_cluster}} --services {{ecs_service}}

      - name: Smoke check
        run: curl -fsS --retry 6 --retry-delay 5 --retry-all-errors {{health_url}}

      - name: Roll back on failure
        if: failure()
        run: |
          test -n "$PREVIOUS_TASKDEF" && aws ecs update-service --cluster {{ecs_cluster}} \
            --service {{ecs_service}} --task-definition "$PREVIOUS_TASKDEF"
```

## After pushing the workflow

1. Map category `{{env}}_deploy` → `{{workflow_file}}` under Repository Settings → Pipeline.
2. Set the target's health URL to `{{health_url}}`.
3. `aws ecs wait services-stable` is what makes the deploy honest — do not drop it for speed.
