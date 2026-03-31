package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagent"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime/types"
)

// defaultModelARN returns the Amazon Nova Lite model ARN for the given region.
// Nova is supported for Knowledge Base RetrieveAndGenerate.
// If you get "The model arn provided is not supported": enable the model in the
// Bedrock console (AWS Console → Bedrock → Model access → Enable "Amazon Nova Lite").
func defaultModelARN(region string) string {
	return "arn:aws:bedrock:" + region + "::foundation-model/amazon.nova-lite-v1:0"
}

// BedrockClient wraps the Bedrock Agent Runtime client for knowledge base queries.
type BedrockClient struct {
	client *bedrockagentruntime.Client
}

// NewBedrockClient creates a Bedrock client using the given config.
func NewBedrockClient(cfg ObsyncianConfig) (*BedrockClient, error) {
	awsCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.Credentials.Key, cfg.Credentials.Secret, "",
		)),
		config.WithRegion(cfg.AWSRegion()),
	)
	if err != nil {
		return nil, fmt.Errorf("bedrock aws config: %w", err)
	}
	client := bedrockagentruntime.NewFromConfig(awsCfg)
	return &BedrockClient{client: client}, nil
}

// RetrieveAndGenerate queries the knowledge base and returns the generated response text.
func (b *BedrockClient) RetrieveAndGenerate(ctx context.Context, knowledgeBaseID, query, region string) (string, error) {
	modelARN := defaultModelARN(region)
	input := &bedrockagentruntime.RetrieveAndGenerateInput{
		Input: &types.RetrieveAndGenerateInput{
			Text: aws.String(query),
		},
		RetrieveAndGenerateConfiguration: &types.RetrieveAndGenerateConfiguration{
			Type: types.RetrieveAndGenerateTypeKnowledgeBase,
			KnowledgeBaseConfiguration: &types.KnowledgeBaseRetrieveAndGenerateConfiguration{
				KnowledgeBaseId: aws.String(knowledgeBaseID),
				ModelArn:        aws.String(modelARN),
			},
		},
	}
	out, err := b.client.RetrieveAndGenerate(ctx, input)
	if err != nil {
		return "", err
	}
	if out.Output == nil || out.Output.Text == nil {
		return "", nil
	}
	text := strings.TrimSpace(*out.Output.Text)
	// If the API returned an internal action/tool call instead of user-facing text, show a friendly message
	if text == "" || isInternalActionOutput(text) {
		if text == "" {
			return "No response was generated. Try rephrasing your question or run Sync KB (press I) to refresh the knowledge base.", nil
		}
		return "The agent returned an internal response instead of an answer. Try rephrasing your question, or run Sync KB (press I) and try again.", nil
	}
	return text, nil
}

// isInternalActionOutput returns true if s looks like a Bedrock internal action/tool call (e.g. "Action: GlobalDataSource.search(...)").
func isInternalActionOutput(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "Action:") || strings.Contains(s, "GlobalDataSource.")
}

// StartKnowledgeBaseIngestion starts an ingestion job for the KB data source so the knowledge base index is updated from S3.
// Requires config.KnowledgeBaseID and config.DataSourceID to be set.
func StartKnowledgeBaseIngestion(ctx context.Context, cfg ObsyncianConfig) (jobID string, err error) {
	if cfg.KnowledgeBaseID == "" || cfg.DataSourceID == "" {
		return "", fmt.Errorf("knowledgeBaseId and dataSourceId required for KB sync")
	}
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.Credentials.Key, cfg.Credentials.Secret, "",
		)),
		config.WithRegion(cfg.AWSRegion()),
	)
	if err != nil {
		return "", fmt.Errorf("bedrock agent aws config: %w", err)
	}
	client := bedrockagent.NewFromConfig(awsCfg)
	out, err := client.StartIngestionJob(ctx, &bedrockagent.StartIngestionJobInput{
		KnowledgeBaseId: aws.String(cfg.KnowledgeBaseID),
		DataSourceId:    aws.String(cfg.DataSourceID),
	})
	if err != nil {
		return "", err
	}
	if out.IngestionJob != nil && out.IngestionJob.IngestionJobId != nil {
		return *out.IngestionJob.IngestionJobId, nil
	}
	return "", nil
}
