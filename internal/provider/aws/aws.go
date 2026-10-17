// Package aws is the Provider that materializes resources through the floci
// emulator, honoring AWS parity as the default. It covers both AWS-API services
// (S3, SNS, SQS+DLQ, Secrets Manager) and the stateful engines exposed as AWS
// managed services: RDS (postgres), ElastiCache (redis) and MSK (kafka). floci
// proxies the stateful engines on its own address at allocated ports, so multiple
// instances stay isolated (RDS 7001,7002…; ElastiCache 6379,6380…).
package aws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/kafka"
	kafkatypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"

	"github.com/svallejo-dev/keel-cli/internal/awsx"
	appcfg "github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/floci"
	"github.com/svallejo-dev/keel-cli/internal/provider"
)

// dockerGateway is the hostname other containers use to reach floci's proxied
// engine ports on the shared network.
const dockerGateway = "floci"

// Provider provisions resources in floci.
type Provider struct {
	// progress, when set, receives human-readable bootstrap messages (e.g. while
	// starting floci). Nil keeps provisioning silent (used by the TUI).
	progress    func(string)
	cfg         appcfg.Config
	s3          *s3.Client
	sns         *sns.Client
	sqs         *sqs.Client
	secrets     *secretsmanager.Client
	rds         *rds.Client
	elasticache *elasticache.Client
	kafka       *kafka.Client
	dynamodb    *dynamodb.Client
}

// New builds the AWS clients pointed at the floci endpoint with dummy creds.
func New(cfg appcfg.Config) (*Provider, error) {
	cl, err := awsx.New(cfg)
	if err != nil {
		return nil, err
	}
	return &Provider{
		cfg:         cfg,
		s3:          cl.S3,
		sns:         cl.SNS,
		sqs:         cl.SQS,
		secrets:     cl.Secrets,
		rds:         cl.RDS,
		elasticache: cl.ElastiCache,
		kafka:       cl.Kafka,
		dynamodb:    cl.DynamoDB,
	}, nil
}

func (p *Provider) Name() domain.ProviderName { return domain.ProviderAWS }

// SetProgress installs a callback for bootstrap messages (nil = silent).
func (p *Provider) SetProgress(fn func(string)) { p.progress = fn }

// Provision creates the requested resource in floci.
func (p *Provider) Provision(ctx context.Context, spec domain.Spec, bp domain.Blueprint, _ provider.PortAllocator) (*domain.Instance, error) {
	// Bring the emulator up on demand: from zero (no network/image/container) this
	// is what makes the very first `create` work instead of "connection refused".
	// It is a single fast probe when floci is already running.
	if err := floci.EnsureUp(ctx, p.cfg, p.progress); err != nil {
		return nil, fmt.Errorf("no se pudo preparar floci: %w", err)
	}

	now := time.Now()
	inst := &domain.Instance{
		Name:         spec.Name,
		Kind:         bp.Kind,
		Provider:     domain.ProviderAWS,
		CloudService: bp.CloudService,
		Status:       domain.StatusRunning,
		Endpoints:    map[domain.Role]domain.Endpoint{},
		Resources:    map[string]string{"endpoint_url": p.cfg.FlociEndpoint},
		Params:       spec.Params,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	switch bp.Kind {
	case domain.KindPostgres:
		return inst, p.provisionRDS(ctx, spec, inst)
	case domain.KindRedis:
		return inst, p.provisionElastiCache(ctx, spec, inst)
	case domain.KindKafka:
		return inst, p.provisionMSK(ctx, spec, inst)
	case domain.KindS3, domain.KindSNS, domain.KindSQS, domain.KindSecrets:
		inst.Endpoints[domain.RoleDefault] = domain.Endpoint{Host: "127.0.0.1", Port: 4566, DockerHost: dockerGateway, DockerPort: 4566}
		return inst, p.provisionAWSAPI(ctx, spec, bp, inst)
	case domain.KindDynamoDB:
		inst.Endpoints[domain.RoleDefault] = domain.Endpoint{Host: "127.0.0.1", Port: 4566, DockerHost: dockerGateway, DockerPort: 4566}
		return inst, p.provisionDynamoDB(ctx, spec, inst)
	default:
		return nil, fmt.Errorf("aws provider does not handle kind %q", bp.Kind)
	}
}

// --- stateful engines via floci ---

func (p *Provider) provisionRDS(ctx context.Context, spec domain.Spec, inst *domain.Instance) error {
	user, pass, db := "infra", "infra_"+randHex(5), dbName(spec)
	in := &rds.CreateDBInstanceInput{
		DBInstanceIdentifier: aws.String(spec.Name),
		Engine:               aws.String("postgres"),
		DBInstanceClass:      aws.String(strParam(spec, "instance_class", "db.t3.micro")),
		AllocatedStorage:     aws.Int32(int32(intParam(spec, "storage", 20))),
		MasterUsername:       aws.String(user),
		MasterUserPassword:   aws.String(pass),
		DBName:               aws.String(db),
	}
	if v := strings.TrimSpace(spec.Params["engine_version"]); v != "" {
		in.EngineVersion = aws.String(v)
	}
	_, err := p.rds.CreateDBInstance(ctx, in)
	if err != nil {
		return fmt.Errorf("create db instance: %w", err)
	}

	var port int32
	if err := poll(ctx, 90*time.Second, func(ctx context.Context) (bool, error) {
		out, err := p.rds.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{DBInstanceIdentifier: aws.String(spec.Name)})
		if err != nil || len(out.DBInstances) == 0 {
			return false, err
		}
		i := out.DBInstances[0]
		if aws.ToString(i.DBInstanceStatus) != "available" || i.Endpoint == nil {
			return false, nil
		}
		port = aws.ToInt32(i.Endpoint.Port)
		return true, nil
	}); err != nil {
		return fmt.Errorf("waiting for rds: %w", err)
	}

	ep := flociEndpoint(int(port))
	inst.Endpoints[domain.RoleWrite] = ep
	inst.Endpoints[domain.RoleRead] = ep // single local node serves both roles
	inst.Cred = domain.Credential{Username: user, Password: pass, Extra: map[string]string{"dbname": db}}
	inst.Resources["db_identifier"] = spec.Name
	return nil
}

func (p *Provider) provisionElastiCache(ctx context.Context, spec domain.Spec, inst *domain.Instance) error {
	in := &elasticache.CreateReplicationGroupInput{
		ReplicationGroupId:          aws.String(spec.Name),
		ReplicationGroupDescription: aws.String(spec.Name),
		Engine:                      aws.String("redis"),
		CacheNodeType:               aws.String(strParam(spec, "node_type", "cache.t3.micro")),
	}
	if v := strings.TrimSpace(spec.Params["engine_version"]); v != "" {
		in.EngineVersion = aws.String(v)
	}
	_, err := p.elasticache.CreateReplicationGroup(ctx, in)
	if err != nil {
		return fmt.Errorf("create replication group: %w", err)
	}

	var port int32
	if err := poll(ctx, 90*time.Second, func(ctx context.Context) (bool, error) {
		out, err := p.elasticache.DescribeReplicationGroups(ctx, &elasticache.DescribeReplicationGroupsInput{ReplicationGroupId: aws.String(spec.Name)})
		if err != nil || len(out.ReplicationGroups) == 0 {
			return false, err
		}
		g := out.ReplicationGroups[0]
		if aws.ToString(g.Status) != "available" || g.ConfigurationEndpoint == nil {
			return false, nil
		}
		port = aws.ToInt32(g.ConfigurationEndpoint.Port)
		return true, nil
	}); err != nil {
		return fmt.Errorf("waiting for elasticache: %w", err)
	}

	inst.Endpoints[domain.RoleDefault] = flociEndpoint(int(port))
	inst.Resources["replication_group"] = spec.Name
	return nil
}

func (p *Provider) provisionMSK(ctx context.Context, spec domain.Spec, inst *domain.Instance) error {
	out, err := p.kafka.CreateClusterV2(ctx, &kafka.CreateClusterV2Input{
		ClusterName: aws.String(spec.Name),
		Serverless: &kafkatypes.ServerlessRequest{
			VpcConfigs: []kafkatypes.VpcConfig{{
				SubnetIds:        []string{"subnet-1", "subnet-2"},
				SecurityGroupIds: []string{"sg-1"},
			}},
		},
	})
	if err != nil {
		return fmt.Errorf("create msk cluster: %w", err)
	}
	arn := aws.ToString(out.ClusterArn)

	if err := poll(ctx, 90*time.Second, func(ctx context.Context) (bool, error) {
		d, err := p.kafka.DescribeClusterV2(ctx, &kafka.DescribeClusterV2Input{ClusterArn: aws.String(arn)})
		if err != nil || d.ClusterInfo == nil {
			return false, err
		}
		return d.ClusterInfo.State == kafkatypes.ClusterStateActive, nil
	}); err != nil {
		return fmt.Errorf("waiting for msk: %w", err)
	}

	brokers, err := p.kafka.GetBootstrapBrokers(ctx, &kafka.GetBootstrapBrokersInput{ClusterArn: aws.String(arn)})
	if err != nil {
		return fmt.Errorf("get bootstrap brokers: %w", err)
	}
	bootstrap := aws.ToString(brokers.BootstrapBrokerString)
	host, port := splitHostPort(bootstrap)

	// docker mode reaches the broker directly on the network; host mode is limited
	// (broker advertises an internal address and is not host-published).
	inst.Endpoints[domain.RoleDefault] = domain.Endpoint{
		Host: "127.0.0.1", Port: port, DockerHost: host, DockerPort: port,
	}
	inst.Resources["cluster_arn"] = arn
	inst.Resources["bootstrap"] = bootstrap
	return nil
}

// --- AWS-API services ---

func (p *Provider) provisionAWSAPI(ctx context.Context, spec domain.Spec, bp domain.Blueprint, inst *domain.Instance) error {
	switch bp.Kind {
	case domain.KindS3:
		if _, err := p.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(spec.Name)}); err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}
		inst.Resources["bucket"] = spec.Name
		if boolParam(spec, "versioning") {
			// floci is young: if versioning is unsupported, keep the bucket and warn
			// instead of failing the whole create.
			if _, err := p.s3.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
				Bucket:                  aws.String(spec.Name),
				VersioningConfiguration: &s3types.VersioningConfiguration{Status: s3types.BucketVersioningStatusEnabled},
			}); err != nil {
				inst.Resources["versioning"] = "unsupported: " + err.Error()
			} else {
				inst.Resources["versioning"] = "enabled"
			}
		}

	case domain.KindSNS:
		name := spec.Name
		attrs := map[string]string{}
		if boolParam(spec, "fifo") {
			name = fifoName(name)
			attrs["FifoTopic"] = "true"
		}
		out, err := p.sns.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(name), Attributes: attrs})
		if err != nil {
			return fmt.Errorf("create topic: %w", err)
		}
		inst.Resources["arn"] = aws.ToString(out.TopicArn)
		inst.Resources["topic"] = name

	case domain.KindSQS:
		return p.provisionSQS(ctx, spec, inst)

	case domain.KindSecrets:
		val := spec.Params["value"]
		if val == "" {
			val = "{}"
		}
		in := &secretsmanager.CreateSecretInput{Name: aws.String(spec.Name), SecretString: aws.String(val)}
		if d := spec.Params["description"]; d != "" {
			in.Description = aws.String(d)
		}
		out, err := p.secrets.CreateSecret(ctx, in)
		if err != nil {
			return fmt.Errorf("create secret: %w", err)
		}
		inst.Resources["arn"] = aws.ToString(out.ARN)
	}
	return nil
}

// provisionSQS creates a queue, optionally with a dead-letter queue wired via a
// redrive policy. Visibility timeout, retention, redrive threshold, DLQ and FIFO
// are all configurable (see the catalog's declared params).
func (p *Provider) provisionSQS(ctx context.Context, spec domain.Spec, inst *domain.Instance) error {
	fifo := boolParam(spec, "fifo")
	attrs := map[string]string{}
	if v := intParam(spec, "visibility_timeout", 0); v > 0 {
		attrs["VisibilityTimeout"] = strconv.Itoa(v)
	}
	if v := intParam(spec, "retention", 0); v > 0 {
		attrs["MessageRetentionPeriod"] = strconv.Itoa(v)
	}
	if fifo {
		attrs["FifoQueue"] = "true"
	}

	name := spec.Name
	if fifo {
		name = fifoName(name)
	}

	// Dead-letter queue first, so the main queue can reference its ARN.
	if boolParamDefault(spec, "dlq", true) {
		dlqName := spec.Name + "-dlq"
		dlqAttrs := map[string]string{}
		if fifo {
			dlqName = fifoName(dlqName)
			dlqAttrs["FifoQueue"] = "true"
		}
		dlq, err := p.sqs.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(dlqName), Attributes: dlqAttrs})
		if err != nil {
			return fmt.Errorf("create dlq: %w", err)
		}
		got, err := p.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl:       dlq.QueueUrl,
			AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
		})
		if err != nil {
			return fmt.Errorf("get dlq arn: %w", err)
		}
		dlqArn := got.Attributes["QueueArn"]
		attrs["RedrivePolicy"] = fmt.Sprintf(`{"deadLetterTargetArn":"%s","maxReceiveCount":"%d"}`,
			dlqArn, intParam(spec, "max_receive_count", 5))
		inst.Resources["dlq_url"] = aws.ToString(dlq.QueueUrl)
		inst.Resources["dlq_arn"] = dlqArn
	}

	main, err := p.sqs.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name), Attributes: attrs})
	if err != nil {
		return fmt.Errorf("create queue: %w", err)
	}
	inst.Resources["queue_url"] = aws.ToString(main.QueueUrl)
	inst.Resources["queue"] = name
	return nil
}

// provisionDynamoDB creates an on-demand table. Params: pk (partition key, default
// "id"), pktype (S|N|B), and optional sk/sktype for a sort key.
func (p *Provider) provisionDynamoDB(ctx context.Context, spec domain.Spec, inst *domain.Instance) error {
	table := spec.Name
	pk := spec.Params["pk"]
	if pk == "" {
		pk = "id"
	}

	attrs := []ddbtypes.AttributeDefinition{{AttributeName: aws.String(pk), AttributeType: ddbAttrType(spec.Params["pktype"])}}
	keys := []ddbtypes.KeySchemaElement{{AttributeName: aws.String(pk), KeyType: ddbtypes.KeyTypeHash}}
	if sk := spec.Params["sk"]; sk != "" {
		attrs = append(attrs, ddbtypes.AttributeDefinition{AttributeName: aws.String(sk), AttributeType: ddbAttrType(spec.Params["sktype"])})
		keys = append(keys, ddbtypes.KeySchemaElement{AttributeName: aws.String(sk), KeyType: ddbtypes.KeyTypeRange})
	}

	throughput := &ddbtypes.ProvisionedThroughput{
		ReadCapacityUnits:  aws.Int64(int64(intParam(spec, "read_capacity", 5))),
		WriteCapacityUnits: aws.Int64(int64(intParam(spec, "write_capacity", 5))),
	}

	// Optional global secondary index (one, declared via flat gsi_* params).
	var gsis []ddbtypes.GlobalSecondaryIndex
	if idx := strings.TrimSpace(spec.Params["gsi_name"]); idx != "" {
		gpk := strings.TrimSpace(spec.Params["gsi_pk"])
		if gpk == "" {
			return fmt.Errorf("gsi_name %q requires gsi_pk", idx)
		}
		attrs = appendAttr(attrs, gpk, ddbAttrType(spec.Params["gsi_pktype"]))
		gkeys := []ddbtypes.KeySchemaElement{{AttributeName: aws.String(gpk), KeyType: ddbtypes.KeyTypeHash}}
		if gsk := strings.TrimSpace(spec.Params["gsi_sk"]); gsk != "" {
			attrs = appendAttr(attrs, gsk, ddbAttrType(spec.Params["gsi_sktype"]))
			gkeys = append(gkeys, ddbtypes.KeySchemaElement{AttributeName: aws.String(gsk), KeyType: ddbtypes.KeyTypeRange})
		}
		projection := ddbtypes.ProjectionTypeAll
		if strings.EqualFold(spec.Params["gsi_projection"], "KEYS_ONLY") {
			projection = ddbtypes.ProjectionTypeKeysOnly
		}
		gsis = append(gsis, ddbtypes.GlobalSecondaryIndex{
			IndexName:             aws.String(idx),
			KeySchema:             gkeys,
			Projection:            &ddbtypes.Projection{ProjectionType: projection},
			ProvisionedThroughput: throughput,
		})
	}

	// Provisioned throughput (classic form) is supported by every DynamoDB-compatible
	// emulator; on-demand (PAY_PER_REQUEST) is newer and floci may reject it.
	if _, err := p.dynamodb.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:              aws.String(table),
		AttributeDefinitions:   attrs,
		KeySchema:              keys,
		ProvisionedThroughput:  throughput,
		GlobalSecondaryIndexes: gsis,
	}); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	if err := poll(ctx, 60*time.Second, func(ctx context.Context) (bool, error) {
		out, err := p.dynamodb.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(table)})
		if err != nil || out.Table == nil {
			return false, err
		}
		return out.Table.TableStatus == ddbtypes.TableStatusActive, nil
	}); err != nil {
		return fmt.Errorf("waiting for table: %w", err)
	}

	inst.Resources["table"] = table
	inst.Resources["partition_key"] = pk
	if sk := spec.Params["sk"]; sk != "" {
		inst.Resources["sort_key"] = sk
	}
	if len(gsis) > 0 {
		inst.Resources["gsi"] = aws.ToString(gsis[0].IndexName)
	}

	// TTL is a separate call; degrade with a note if floci lacks support.
	if ttl := strings.TrimSpace(spec.Params["ttl"]); ttl != "" {
		if _, err := p.dynamodb.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
			TableName: aws.String(table),
			TimeToLiveSpecification: &ddbtypes.TimeToLiveSpecification{
				AttributeName: aws.String(ttl), Enabled: aws.Bool(true),
			},
		}); err != nil {
			inst.Resources["ttl"] = "unsupported: " + err.Error()
		} else {
			inst.Resources["ttl"] = ttl
		}
	}
	return nil
}

// appendAttr adds an attribute definition unless the name is already declared
// (a GSI often reuses the table's key attributes).
func appendAttr(attrs []ddbtypes.AttributeDefinition, name string, t ddbtypes.ScalarAttributeType) []ddbtypes.AttributeDefinition {
	for _, a := range attrs {
		if aws.ToString(a.AttributeName) == name {
			return attrs
		}
	}
	return append(attrs, ddbtypes.AttributeDefinition{AttributeName: aws.String(name), AttributeType: t})
}

// --- param helpers -----------------------------------------------------------

// strParam returns a trimmed param value or def when unset.
func strParam(spec domain.Spec, key, def string) string {
	if v := strings.TrimSpace(spec.Params[key]); v != "" {
		return v
	}
	return def
}

// intParam parses a numeric param, falling back to def when unset or invalid.
func intParam(spec domain.Spec, key string, def int) int {
	v := strings.TrimSpace(spec.Params[key])
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// boolParam reports a truthy param (default false).
func boolParam(spec domain.Spec, key string) bool { return boolParamDefault(spec, key, false) }

// boolParamDefault reports a truthy param, using def when unset/unparseable.
func boolParamDefault(spec domain.Spec, key string, def bool) bool {
	v := strings.TrimSpace(spec.Params[key])
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// fifoName appends the .fifo suffix AWS requires for FIFO queues/topics.
func fifoName(name string) string {
	if strings.HasSuffix(name, ".fifo") {
		return name
	}
	return name + ".fifo"
}

// ddbAttrType maps a param string to a DynamoDB scalar attribute type (default S).
func ddbAttrType(s string) ddbtypes.ScalarAttributeType {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "N":
		return ddbtypes.ScalarAttributeTypeN
	case "B":
		return ddbtypes.ScalarAttributeTypeB
	default:
		return ddbtypes.ScalarAttributeTypeS
	}
}

// Start/Stop: floci resources are managed by the emulator; we reflect status only.
func (p *Provider) Start(_ context.Context, inst *domain.Instance) error {
	inst.Status = domain.StatusRunning
	inst.UpdatedAt = time.Now()
	return nil
}

func (p *Provider) Stop(_ context.Context, inst *domain.Instance) error {
	inst.Status = domain.StatusStopped
	inst.UpdatedAt = time.Now()
	return nil
}

// Destroy deletes the resource from floci. A resource that is already gone is
// treated as a successful destroy so the registry entry is removed cleanly.
func (p *Provider) Destroy(ctx context.Context, inst *domain.Instance, _ bool) error {
	switch inst.Kind {
	case domain.KindPostgres:
		_, err := p.rds.DeleteDBInstance(ctx, &rds.DeleteDBInstanceInput{
			DBInstanceIdentifier: aws.String(inst.Name),
			SkipFinalSnapshot:    aws.Bool(true),
		})
		return ignoreNotFound(err)
	case domain.KindRedis:
		_, err := p.elasticache.DeleteReplicationGroup(ctx, &elasticache.DeleteReplicationGroupInput{
			ReplicationGroupId: aws.String(inst.Name),
		})
		return ignoreNotFound(err)
	case domain.KindKafka:
		if arn := inst.Resources["cluster_arn"]; arn != "" {
			_, err := p.kafka.DeleteCluster(ctx, &kafka.DeleteClusterInput{ClusterArn: aws.String(arn)})
			return ignoreNotFound(err)
		}
		return nil
	case domain.KindS3:
		_, err := p.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(inst.Name)})
		return ignoreNotFound(err)
	case domain.KindSNS:
		_, err := p.sns.DeleteTopic(ctx, &sns.DeleteTopicInput{TopicArn: aws.String(inst.Resources["arn"])})
		return ignoreNotFound(err)
	case domain.KindSQS:
		if u := inst.Resources["queue_url"]; u != "" {
			_, _ = p.sqs.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(u)})
		}
		if u := inst.Resources["dlq_url"]; u != "" {
			_, _ = p.sqs.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(u)})
		}
		return nil
	case domain.KindSecrets:
		_, err := p.secrets.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
			SecretId:                   aws.String(inst.Name),
			ForceDeleteWithoutRecovery: aws.Bool(true),
		})
		return ignoreNotFound(err)
	case domain.KindDynamoDB:
		_, err := p.dynamodb.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(inst.Name)})
		return ignoreNotFound(err)
	default:
		return nil
	}
}

// ignoreNotFound returns nil when err indicates the resource is already gone.
func ignoreNotFound(err error) error {
	if err == nil {
		return nil
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "NotFound", "NotFoundException", "ResourceNotFoundException",
			"NoSuchBucket", "NoSuchEntity", "DBInstanceNotFound",
			"ReplicationGroupNotFoundFault", "ClusterNotFoundException":
			return nil
		}
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "not exist") || strings.Contains(msg, "not found") || strings.Contains(msg, "notfound") {
		return nil
	}
	return err
}

func (p *Provider) Logs(_ context.Context, inst *domain.Instance, _ bool, w io.Writer) error {
	_, err := fmt.Fprintf(w, "[aws/floci] %s (%s) is managed by floci; see the floci container logs: docker logs floci\n", inst.Name, inst.Kind)
	return err
}

// --- helpers ---

// flociEndpoint builds an endpoint for a floci-proxied engine port: host mode via
// localhost (published range), docker mode via the floci hostname.
func flociEndpoint(port int) domain.Endpoint {
	return domain.Endpoint{Host: "127.0.0.1", Port: port, DockerHost: dockerGateway, DockerPort: port}
}

// poll invokes check every 1.5s until it returns true, ctx is done, or timeout.
func poll(ctx context.Context, timeout time.Duration, check func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		ok, err := check(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout")
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func splitHostPort(hostport string) (string, int) {
	h, p, ok := strings.Cut(hostport, ":")
	if !ok {
		return hostport, 9092
	}
	n, err := strconv.Atoi(strings.TrimSpace(p))
	if err != nil {
		n = 9092
	}
	return h, n
}

func dbName(spec domain.Spec) string {
	if v := spec.Params["dbname"]; v != "" {
		return v
	}
	// RDS db names must be alphanumeric; sanitize the instance name.
	var sb strings.Builder
	for _, r := range strings.ToLower(spec.Name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	if sb.Len() == 0 {
		return "app"
	}
	return sb.String()
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "000000"
	}
	return hex.EncodeToString(b)
}
