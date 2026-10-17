package ops

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// rpkImage is the throwaway container image used to run rpk against the broker.
const rpkImage = "redpandadata/redpanda:latest"

// KafkaOps operates on a Kafka instance by running rpk in an ephemeral container
// on the shared network, reaching the broker via its docker-mode address (works
// for both floci MSK and standalone redpanda).
type KafkaOps struct {
	network string
}

// TopicInfo summarizes a topic.
type TopicInfo struct {
	Name       string
	Partitions int
	Replicas   int
}

func brokers(inst *domain.Instance) string {
	ep, _ := inst.PrimaryEndpoint()
	host, port := ep.DockerHost, ep.DockerPort
	if host == "" {
		host, port = ep.Host, ep.Port
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func (o *KafkaOps) run(ctx context.Context, inst *domain.Instance, stdin string, args ...string) (string, error) {
	if err := requireReal(inst); err != nil {
		return "", err
	}
	full := []string{"run", "--rm"}
	if stdin != "" {
		full = append(full, "-i")
	}
	full = append(full, "--network", o.network, "--entrypoint", "rpk", rpkImage)
	full = append(full, args...)
	full = append(full, "--brokers", brokers(inst))

	cmd := exec.CommandContext(ctx, "docker", full...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String()) // rpk reports some errors on stdout
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return out.String(), nil
}

// ListTopics returns the topics on the broker.
func (o *KafkaOps) ListTopics(ctx context.Context, inst *domain.Instance) ([]TopicInfo, error) {
	out, err := o.run(ctx, inst, "", "topic", "list")
	if err != nil {
		return nil, err
	}
	var topics []TopicInfo
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.EqualFold(fields[0], "NAME") {
			continue
		}
		t := TopicInfo{Name: fields[0]}
		if len(fields) > 1 {
			t.Partitions, _ = strconv.Atoi(fields[1])
		}
		if len(fields) > 2 {
			t.Replicas, _ = strconv.Atoi(fields[2])
		}
		topics = append(topics, t)
	}
	return topics, nil
}

// CreateTopic creates a topic.
func (o *KafkaOps) CreateTopic(ctx context.Context, inst *domain.Instance, name string, partitions, replication int, configs map[string]string) error {
	if partitions <= 0 {
		partitions = 1
	}
	if replication <= 0 {
		replication = 1
	}
	args := []string{"topic", "create", name, "-p", strconv.Itoa(partitions), "-r", strconv.Itoa(replication)}
	for k, v := range configs {
		args = append(args, "-c", k+"="+v)
	}
	_, err := o.run(ctx, inst, "", args...)
	return err
}

// DeleteTopic deletes a topic.
func (o *KafkaOps) DeleteTopic(ctx context.Context, inst *domain.Instance, name string) error {
	_, err := o.run(ctx, inst, "", "topic", "delete", name)
	return err
}

// DescribeTopic returns rpk's description of a topic.
func (o *KafkaOps) DescribeTopic(ctx context.Context, inst *domain.Instance, name string) (string, error) {
	return o.run(ctx, inst, "", "topic", "describe", name)
}

// Produce publishes a single record (optional key) to a topic.
func (o *KafkaOps) Produce(ctx context.Context, inst *domain.Instance, topic, key, value string) error {
	args := []string{"topic", "produce", topic}
	if key != "" {
		args = append(args, "--key", key)
	}
	_, err := o.run(ctx, inst, value+"\n", args...)
	return err
}

// Consume reads up to n records from a topic.
func (o *KafkaOps) Consume(ctx context.Context, inst *domain.Instance, topic string, n int, fromStart bool) ([]string, error) {
	if n <= 0 {
		n = 10
	}
	args := []string{"topic", "consume", topic, "-n", strconv.Itoa(n), "-f", "%v\n"}
	if fromStart {
		args = append(args, "-o", "start")
	}
	out, err := o.run(ctx, inst, "", args...)
	if err != nil {
		return nil, err
	}
	var recs []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			recs = append(recs, line)
		}
	}
	return recs, nil
}
