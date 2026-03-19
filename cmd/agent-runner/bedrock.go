package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// bedrockClientAPI is the subset of the Bedrock Runtime client used by callBedrock.
// It exists so tests can inject a mock without hitting AWS.
type bedrockClientAPI interface {
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// newBedrockClient creates a real Bedrock Runtime client from the default AWS config.
// AWS SDK v2 auto-discovers credentials from AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
// AWS_SESSION_TOKEN, and AWS_REGION environment variables.
func newBedrockClient(ctx context.Context) (bedrockClientAPI, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return bedrockruntime.NewFromConfig(cfg), nil
}

// callBedrock uses the AWS Bedrock Converse API with optional tool calling.
// AWS credentials are resolved from the environment via the SDK's default chain.
func callBedrock(ctx context.Context, model, systemPrompt, task string, tools []ToolDef) (string, int, int, int, error) {
	client, err := newBedrockClient(ctx)
	if err != nil {
		return "", 0, 0, 0, err
	}
	return callBedrockWithClient(ctx, client, model, systemPrompt, task, tools)
}

// callBedrockWithClient is the core implementation, accepting a client interface for testability.
func callBedrockWithClient(ctx context.Context, client bedrockClientAPI, model, systemPrompt, task string, tools []ToolDef) (string, int, int, int, error) {
	// Build Bedrock tool definitions.
	var bedrockTools []types.Tool
	for _, t := range tools {
		schemaBytes, err := json.Marshal(t.Parameters)
		if err != nil {
			return "", 0, 0, 0, fmt.Errorf("marshaling tool schema for %s: %w", t.Name, err)
		}
		bedrockTools = append(bedrockTools, &types.ToolMemberToolSpec{
			Value: types.ToolSpecification{
				Name:        aws.String(t.Name),
				Description: aws.String(t.Description),
				InputSchema: &types.ToolInputSchemaMemberJson{
					Value: document.NewLazyDocument(json.RawMessage(schemaBytes)),
				},
			},
		})
	}

	messages := []types.Message{
		{
			Role: types.ConversationRoleUser,
			Content: []types.ContentBlock{
				&types.ContentBlockMemberText{Value: task},
			},
		},
	}

	totalInputTokens := 0
	totalOutputTokens := 0
	totalToolCalls := 0

	for i := 0; i < maxToolIterations; i++ {
		input := &bedrockruntime.ConverseInput{
			ModelId:  aws.String(model),
			Messages: messages,
			System: []types.SystemContentBlock{
				&types.SystemContentBlockMemberText{Value: systemPrompt},
			},
		}
		if len(bedrockTools) > 0 {
			input.ToolConfig = &types.ToolConfiguration{
				Tools: bedrockTools,
			}
		}

		chatCtx, chatSpan := obs.startChatSpan(ctx,
			attribute.String("gen_ai.system", "bedrock"),
			attribute.String("gen_ai.request.model", model),
		)
		output, err := client.Converse(chatCtx, input)
		if err != nil {
			markSpanError(chatSpan, err)
			chatSpan.End()
			var apiErr smithy.APIError
			if ok := errors.As(err, &apiErr); ok {
				return "", totalInputTokens, totalOutputTokens, totalToolCalls,
					fmt.Errorf("Bedrock API error (%s): %s", apiErr.ErrorCode(), apiErr.ErrorMessage())
			}
			return "", totalInputTokens, totalOutputTokens, totalToolCalls,
				fmt.Errorf("Bedrock API error: %w", err)
		}

		if output.Usage != nil {
			totalInputTokens += int(aws.ToInt32(output.Usage.InputTokens))
			totalOutputTokens += int(aws.ToInt32(output.Usage.OutputTokens))
			chatSpan.SetAttributes(
				attribute.Int("gen_ai.usage.input_tokens", int(aws.ToInt32(output.Usage.InputTokens))),
				attribute.Int("gen_ai.usage.output_tokens", int(aws.ToInt32(output.Usage.OutputTokens))),
			)
		}
		chatSpan.SetAttributes(attribute.String("gen_ai.response.finish_reasons", string(output.StopReason)))
		chatSpan.SetStatus(codes.Ok, "")
		chatSpan.End()

		// Separate text blocks and tool-use blocks.
		var textContent strings.Builder
		var toolUseBlocks []bedrockToolUse
		for _, block := range output.Output.(*types.ConverseOutputMemberMessage).Value.Content {
			switch v := block.(type) {
			case *types.ContentBlockMemberText:
				textContent.WriteString(v.Value)
			case *types.ContentBlockMemberToolUse:
				inputBytes, _ := v.Value.Input.MarshalSmithyDocument()
				toolUseBlocks = append(toolUseBlocks, bedrockToolUse{
					ToolUseID: aws.ToString(v.Value.ToolUseId),
					Name:      aws.ToString(v.Value.Name),
					Input:     string(inputBytes),
				})
			}
		}

		// If no tool calls, return the text.
		if output.StopReason != types.StopReasonToolUse || len(toolUseBlocks) == 0 {
			return textContent.String(), totalInputTokens, totalOutputTokens, totalToolCalls, nil
		}

		// Build the assistant message with all content blocks.
		var assistantContent []types.ContentBlock
		for _, block := range output.Output.(*types.ConverseOutputMemberMessage).Value.Content {
			assistantContent = append(assistantContent, block)
		}
		messages = append(messages, types.Message{
			Role:    types.ConversationRoleAssistant,
			Content: assistantContent,
		})

		// Execute each tool call and build tool_result blocks.
		var resultContent []types.ContentBlock
		for _, tu := range toolUseBlocks {
			totalToolCalls++
			log.Printf("tool_use [%d]: %s id=%s", totalToolCalls, tu.Name, tu.ToolUseID)

			result := executeToolCallWithTelemetry(ctx, tu.Name, tu.Input, tu.ToolUseID)
			isErr := strings.HasPrefix(result, "Error:")

			toolResult := &types.ContentBlockMemberToolResult{
				Value: types.ToolResultBlock{
					ToolUseId: aws.String(tu.ToolUseID),
					Content: []types.ToolResultContentBlock{
						&types.ToolResultContentBlockMemberText{Value: result},
					},
				},
			}
			if isErr {
				toolResult.Value.Status = types.ToolResultStatusError
			}
			resultContent = append(resultContent, toolResult)
		}
		messages = append(messages, types.Message{
			Role:    types.ConversationRoleUser,
			Content: resultContent,
		})
	}

	return "", totalInputTokens, totalOutputTokens, totalToolCalls,
		fmt.Errorf("exceeded maximum tool-call iterations (%d)", maxToolIterations)
}

// bedrockToolUse holds the parsed fields from a Bedrock tool_use content block.
type bedrockToolUse struct {
	ToolUseID string
	Name      string
	Input     string
}
