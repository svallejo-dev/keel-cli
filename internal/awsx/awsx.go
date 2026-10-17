// Package awsx centralizes construction of AWS SDK clients pointed at the floci
// emulator. Both the aws provider (which creates resources) and the ops layer
// (which operates on them) build their clients here, so endpoint/credential
// wiring lives in one place.
package awsx

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfgload "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/kafka"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	appcfg "github.com/svallejo-dev/keel-cli/internal/config"
)

// Clients bundles the AWS service clients used across the platform.
type Clients struct {
	Config      aws.Config
	S3          *s3.Client
	SNS         *sns.Client
	SQS         *sqs.Client
	Secrets     *secretsmanager.Client
	RDS         *rds.Client
	ElastiCache *elasticache.Client
	Kafka       *kafka.Client
	DynamoDB    *dynamodb.Client
}

// New builds the AWS clients targeting the floci endpoint with dummy credentials.
func New(cfg appcfg.Config) (*Clients, error) {
	awsConf, err := awscfgload.LoadDefaultConfig(context.Background(),
		awscfgload.WithRegion(cfg.Region),
		awscfgload.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AWSAccessKey, cfg.AWSSecretKey, "")),
	)
	if err != nil {
		return nil, err
	}
	ep := cfg.FlociEndpoint
	return &Clients{
		Config: awsConf,
		S3: s3.NewFromConfig(awsConf, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(ep)
			o.UsePathStyle = true // required for custom S3 endpoints
		}),
		SNS:         sns.NewFromConfig(awsConf, func(o *sns.Options) { o.BaseEndpoint = aws.String(ep) }),
		SQS:         sqs.NewFromConfig(awsConf, func(o *sqs.Options) { o.BaseEndpoint = aws.String(ep) }),
		Secrets:     secretsmanager.NewFromConfig(awsConf, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(ep) }),
		RDS:         rds.NewFromConfig(awsConf, func(o *rds.Options) { o.BaseEndpoint = aws.String(ep) }),
		ElastiCache: elasticache.NewFromConfig(awsConf, func(o *elasticache.Options) { o.BaseEndpoint = aws.String(ep) }),
		Kafka:       kafka.NewFromConfig(awsConf, func(o *kafka.Options) { o.BaseEndpoint = aws.String(ep) }),
		DynamoDB:    dynamodb.NewFromConfig(awsConf, func(o *dynamodb.Options) { o.BaseEndpoint = aws.String(ep) }),
	}, nil
}
