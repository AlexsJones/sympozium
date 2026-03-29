# AWS Bedrock Guide

This guide covers setting up and using Amazon Bedrock as an LLM provider in Sympozium.

## Overview

Amazon Bedrock provides access to foundation models from Amazon (Nova), Anthropic (Claude), AI21, Cohere, and Meta through a managed API. Sympozium supports Bedrock's Converse API for conversational interactions with tool calling.

## Prerequisites

1. **AWS Account** with Bedrock access enabled
2. **Bedrock Model Access**: Some models require explicit approval in the AWS Console
3. **Kubernetes cluster** with Sympozium installed

## Step 1: Enable Bedrock Models

### Console Method
1. Navigate to [AWS Bedrock Console](https://console.aws.amazon.com/bedrock)
2. Go to **Model access** in the left sidebar
3. Click **Manage model access**
4. Select the models you want to use:
   - **Anthropic Claude**: `anthropic.claude-3-sonnet-20240229-v1:0`, `anthropic.claude-3-haiku-20240307-v1:0`, etc.
   - **Amazon Nova**: `amazon.nova-pro-v1:0`, `amazon.nova-lite-v1:0`
5. Check the boxes and click **Confirm changes**

### AWS CLI Method
```bash
# List available models (requires bedrock:ListFoundationModels permission)
aws bedrock list-foundation-models --region us-east-1 \
  --by-provider Anthropic \
  --query 'modelSummaries[*].{id:modelId,ar:arn}' \
  --output table
```

## Step 2: Configure Authentication

You have three options for authentication:

### Option A: IAM User Access Keys (Simplest)

1. Create an IAM user with Bedrock permissions
2. Generate access keys
3. Use them directly in Kubernetes secrets

**IAM Policy:**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "bedrock:ListFoundationModels",
        "bedrock:GetFoundationModel",
        "bedrock:Converse",
        "bedrock:InvokeModel"
      ],
      "Resource": "*"
    }
  ]
}
```

### Option B: IRSA (EKS IAM Roles for Service Accounts)

For EKS clusters, use IRSA to avoid managing access keys:

1. **Create an IAM OIDC provider** (if not exists):
```bash
eksctl utils associate-iam-oidc-provider \
  --cluster <cluster-name> \
  --approve
```

2. **Create IAM role with trust policy:**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::<account-id>:oidc-provider/oidc.eks.<region>.amazonaws.com/id/<oidc-id>"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "oidc.eks.<region>.amazonaws.com/id/<oidc-id>:sub": "system:serviceaccount:sympozium:sympozium-agent"
        }
      }
    }
  ]
}
```

3. **Attach Bedrock permissions policy** to the role (same as Option A)

4. **Annotate your service account:**
```bash
kubectl annotate serviceaccount sympozium-agent \
  eks.amazonaws.com/role-arn=arn:aws:iam::<account-id>:role/<bedrock-role-name> \
  -n sympozium
```

### Option C: EC2 Instance Profile / EKS Node Role

If running on EC2/EKS nodes with an instance profile:
- The node's IAM role will be used automatically
- Ensure the role has Bedrock permissions (see Option A policy)

## Step 3: Create Kubernetes Secret

### For IAM User Credentials
```bash
kubectl create secret generic my-instance-bedrock-key -n sympozium \
  --from-literal=AWS_REGION=us-east-1 \
  --from-literal=AWS_ACCESS_KEY_ID=<your-access-key-id> \
  --from-literal=AWS_SECRET_ACCESS_KEY=<your-secret-access-key>
```

### For IRSA (No Access Keys)
```bash
kubectl create secret generic my-instance-bedrock-key -n sympozium \
  --from-literal=AWS_REGION=us-east-1
```

### For Temporary Credentials (STS)
If using STS temporary credentials:
```bash
kubectl create secret generic my-instance-bedrock-key -n sympozium \
  --from-literal=AWS_REGION=us-east-1 \
  --from-literal=AWS_ACCESS_KEY_ID=<access-key> \
  --from-literal=AWS_SECRET_ACCESS_KEY=<secret-key> \
  --from-literal=AWS_SESSION_TOKEN=<session-token>
```

## Step 4: Create SympoziumInstance

### Using the CLI Wizard
```bash
sympozium create instance my-bedrock-agent
# Select option 6: AWS Bedrock (Claude, Nova, etc.)
# Enter your region and model ID when prompted
```

### Manual YAML Configuration
```yaml
apiVersion: sympozium.ai/v1alpha1
kind: SympoziumInstance
metadata:
  name: my-bedrock-agent
  namespace: sympozium
spec:
  provider: bedrock
  model: anthropic.claude-3-sonnet-20240229-v1:0
  region: us-east-1
  secretRef:
    name: my-instance-bedrock-key
  # Optional: Enable specific skills
  skills:
    - name: k8s-ops
  # Optional: Channel bindings (Telegram, Slack, etc.)
  channels:
    - name: my-telegram-channel
```

## Available Bedrock Models

Sympozium supports all text-capable foundation models. Common options:

| Provider | Model ID | Description |
|----------|----------|-------------|
| Anthropic | `anthropic.claude-3-sonnet-20240229-v1:0` | Balanced performance/cost |
| Anthropic | `anthropic.claude-3-haiku-20240307-v1:0` | Fast, lightweight |
| Anthropic | `anthropic.claude-3-opus-20240229-v1:0` | Most powerful |
| Anthropic | `anthropic.claude-3.5-sonnet-20241022-v2:0` | Latest Claude 3.5 |
| Amazon | `amazon.nova-pro-v1:0` | Amazon's Nova Pro |
| Amazon | `amazon.nova-lite-v1:0` | Amazon's Nova Lite |

### Listing Available Models
```bash
# Via API (requires credentials in environment)
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=<your-key>
export AWS_SECRET_ACCESS_KEY=<your-secret>

curl -X POST http://<apiserver>/api/v1/providers/bedrock/models \
  -H "Content-Type: application/json" \
  -d '{"region": "us-east-1", "accessKeyId": "<key>", "secretAccessKey": "<secret>"}'
```

## Tool Calling with Bedrock

Bedrock's Converse API supports native tool calling. Sympozium automatically:
1. Converts tool definitions to Bedrock's `ToolSpecification` format
2. Handles the multi-turn conversation flow for tool execution
3. Formats tool results back to the model

Example workflow:
```
User task → Model generates tool_use block → Sympozium executes tool → 
Model receives tool_result → Final response
```

## Troubleshooting

### AccessDeniedException
- Verify the model is approved in Bedrock Console
- Check IAM permissions include `bedrock:Converse` and `bedrock:ListFoundationModels`
- Ensure region matches where models are provisioned

### ModelNotAccessibleException
- Some models require explicit opt-in in specific regions
- Try a different region or model ID

### Credentials not discovered
- Verify environment variables: `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`
- For IRSA, check service account annotations and IAM role trust policy
- Ensure AWS SDK can access the credentials (check pod logs)

### Region mismatch
- Bedrock models are region-specific
- Ensure `region` in SympoziumInstance matches where you approved model access

## Best Practices

1. **Use IRSA for production**: Avoid long-lived access keys
2. **Least privilege IAM**: Restrict to specific model ARNs when possible
3. **Model selection**: Start with `anthropic.claude-3-haiku` for cost efficiency
4. **Region consistency**: Keep region consistent across AWS config and model approval
5. **Monitor costs**: Bedrock charges per input/output token

## Example: Full Deployment

```bash
# 1. Create IAM role and policy (IRSA)
eksctl create iamserviceaccount \
  --name sympozium-agent \
  --namespace sympozium \
  --cluster <cluster> \
  --role-name sympozium-bedrock-role \
  --attach-policy-arn arn:aws:iam::<account>:policy/BedrockAccess \
  --approve

# 2. Create minimal secret (IRSA handles auth)
kubectl create secret generic bedrock-agent-key -n sympozium \
  --from-literal=AWS_REGION=us-east-1

# 3. Deploy SympoziumInstance
kubectl apply -f - <<EOF
apiVersion: sympozium.ai/v1alpha1
kind: SympoziumInstance
metadata:
  name: bedrock-assistant
  namespace: sympozium
spec:
  provider: bedrock
  model: anthropic.claude-3-haiku-20240307-v1:0
  region: us-east-1
  secretRef:
    name: bedrock-agent-key
EOF

# 4. Test with a simple AgentRun
kubectl apply -f - <<EOF
apiVersion: sympozium.ai/v1alpha1
kind: AgentRun
metadata:
  name: test-bedrock
  namespace: sympozium
spec:
  instanceRef: bedrock-assistant
  task: "Say hello in one sentence"
EOF

# 5. Check results
kubectl get agentrun test-bedrock -n sympozium -o yaml
```

## Related Documentation

- [Sympozium Design](/docs/design.md) - Architecture overview
- [Writing Skills](/guides/writing-skills.md) - Creating custom skills
- [Channels](/concepts/channels.md) - Connecting Telegram, Slack, etc.
