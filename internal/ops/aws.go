package ops

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// ObjectInfo summarizes an S3 object.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// SubInfo summarizes an SNS subscription.
type SubInfo struct {
	Protocol string
	Endpoint string
	Arn      string
}

// Message is an SQS message.
type Message struct {
	ID            string
	Body          string
	ReceiptHandle string
}

// --- S3 ---

type S3Ops struct{ c *s3.Client }

func (o *S3Ops) ListObjects(ctx context.Context, inst *domain.Instance, prefix string) ([]ObjectInfo, error) {
	if err := requireReal(inst); err != nil {
		return nil, err
	}
	in := &s3.ListObjectsV2Input{Bucket: aws.String(inst.Name)}
	if prefix != "" {
		in.Prefix = aws.String(prefix)
	}
	out, err := o.c.ListObjectsV2(ctx, in)
	if err != nil {
		return nil, err
	}
	objs := make([]ObjectInfo, 0, len(out.Contents))
	for _, obj := range out.Contents {
		objs = append(objs, ObjectInfo{
			Key:          aws.ToString(obj.Key),
			Size:         aws.ToInt64(obj.Size),
			LastModified: aws.ToTime(obj.LastModified),
		})
	}
	return objs, nil
}

func (o *S3Ops) PutObject(ctx context.Context, inst *domain.Instance, key string, body []byte, contentType string) error {
	if err := requireReal(inst); err != nil {
		return err
	}
	in := &s3.PutObjectInput{Bucket: aws.String(inst.Name), Key: aws.String(key), Body: bytes.NewReader(body)}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	_, err := o.c.PutObject(ctx, in)
	return err
}

func (o *S3Ops) GetObject(ctx context.Context, inst *domain.Instance, key string) ([]byte, error) {
	if err := requireReal(inst); err != nil {
		return nil, err
	}
	out, err := o.c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(inst.Name), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (o *S3Ops) DeleteObject(ctx context.Context, inst *domain.Instance, key string) error {
	if err := requireReal(inst); err != nil {
		return err
	}
	_, err := o.c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(inst.Name), Key: aws.String(key)})
	return err
}

// --- SNS ---

type SNSOps struct{ c *sns.Client }

func (o *SNSOps) Publish(ctx context.Context, inst *domain.Instance, subject, message string) (string, error) {
	if err := requireReal(inst); err != nil {
		return "", err
	}
	in := &sns.PublishInput{TopicArn: aws.String(inst.Resources["arn"]), Message: aws.String(message)}
	if subject != "" {
		in.Subject = aws.String(subject)
	}
	out, err := o.c.Publish(ctx, in)
	if err != nil {
		return "", err
	}
	return aws.ToString(out.MessageId), nil
}

func (o *SNSOps) ListSubscriptions(ctx context.Context, inst *domain.Instance) ([]SubInfo, error) {
	if err := requireReal(inst); err != nil {
		return nil, err
	}
	out, err := o.c.ListSubscriptionsByTopic(ctx, &sns.ListSubscriptionsByTopicInput{TopicArn: aws.String(inst.Resources["arn"])})
	if err != nil {
		return nil, err
	}
	subs := make([]SubInfo, 0, len(out.Subscriptions))
	for _, s := range out.Subscriptions {
		subs = append(subs, SubInfo{
			Protocol: aws.ToString(s.Protocol),
			Endpoint: aws.ToString(s.Endpoint),
			Arn:      aws.ToString(s.SubscriptionArn),
		})
	}
	return subs, nil
}

// --- SQS ---

type SQSOps struct{ c *sqs.Client }

func (o *SQSOps) Send(ctx context.Context, inst *domain.Instance, body string) (string, error) {
	if err := requireReal(inst); err != nil {
		return "", err
	}
	out, err := o.c.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(inst.Resources["queue_url"]),
		MessageBody: aws.String(body),
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.MessageId), nil
}

func (o *SQSOps) Receive(ctx context.Context, inst *domain.Instance, max, waitSecs int) ([]Message, error) {
	if err := requireReal(inst); err != nil {
		return nil, err
	}
	if max <= 0 {
		max = 1
	}
	out, err := o.c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(inst.Resources["queue_url"]),
		MaxNumberOfMessages: int32(max),
		WaitTimeSeconds:     int32(waitSecs),
	})
	if err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(out.Messages))
	for _, m := range out.Messages {
		msgs = append(msgs, Message{
			ID:            aws.ToString(m.MessageId),
			Body:          aws.ToString(m.Body),
			ReceiptHandle: aws.ToString(m.ReceiptHandle),
		})
	}
	return msgs, nil
}

func (o *SQSOps) Purge(ctx context.Context, inst *domain.Instance) error {
	if err := requireReal(inst); err != nil {
		return err
	}
	_, err := o.c.PurgeQueue(ctx, &sqs.PurgeQueueInput{QueueUrl: aws.String(inst.Resources["queue_url"])})
	return err
}

func (o *SQSOps) Attributes(ctx context.Context, inst *domain.Instance) (map[string]string, error) {
	if err := requireReal(inst); err != nil {
		return nil, err
	}
	out, err := o.c.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(inst.Resources["queue_url"]),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
	})
	if err != nil {
		return nil, err
	}
	return out.Attributes, nil
}

// --- Secrets Manager ---

type SecretsOps struct{ c *secretsmanager.Client }

func (o *SecretsOps) Get(ctx context.Context, inst *domain.Instance) (string, error) {
	if err := requireReal(inst); err != nil {
		return "", err
	}
	out, err := o.c.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(inst.Name)})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.SecretString), nil
}

func (o *SecretsOps) Set(ctx context.Context, inst *domain.Instance, value string) error {
	if err := requireReal(inst); err != nil {
		return err
	}
	_, err := o.c.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(inst.Name),
		SecretString: aws.String(value),
	})
	return err
}

func (o *SecretsOps) Describe(ctx context.Context, inst *domain.Instance) (map[string]string, error) {
	if err := requireReal(inst); err != nil {
		return nil, err
	}
	out, err := o.c.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: aws.String(inst.Name)})
	if err != nil {
		return nil, err
	}
	m := map[string]string{
		"Name": aws.ToString(out.Name),
		"ARN":  aws.ToString(out.ARN),
	}
	if out.LastChangedDate != nil {
		m["LastChangedDate"] = out.LastChangedDate.Format(time.RFC3339)
	}
	if out.Description != nil {
		m["Description"] = aws.ToString(out.Description)
	}
	return m, nil
}
