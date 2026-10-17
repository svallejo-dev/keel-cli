package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/svallejo-dev/keel-cli/internal/control"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/ops"
)

// actionsFor returns every runnable action for an instance: lifecycle/management
// plus the component operations for its kind. These mirror the CLI commands.
func actionsFor(inst *domain.Instance) []action {
	acts := lifecycleActions()
	acts = append(acts, opsActions(inst.Kind)...)
	return acts
}

func lifecycleActions() []action {
	return []action{
		{id: "start", label: "Start", run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			return result{toast: "started " + i.Name, refresh: true}, m.Up(ctx, i.Name)
		}},
		{id: "stop", label: "Stop", run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			return result{toast: "stopped " + i.Name, refresh: true}, m.Down(ctx, i.Name)
		}},
		{id: "restart", label: "Restart", run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			return result{toast: "restarted " + i.Name, refresh: true}, m.Restart(ctx, i.Name)
		}},
		{id: "edit", label: "Edit (params / note)", fields: []field{
			{key: "note", label: "Note", placeholder: "free text"},
			{key: "param", label: "Param key=value", placeholder: "k=v (optional)"},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			params := map[string]string{}
			if p := strings.TrimSpace(in["param"]); p != "" {
				k, v, ok := strings.Cut(p, "=")
				if !ok {
					return result{}, fmt.Errorf("param must be key=value")
				}
				params[k] = v
			}
			_, err := m.Update(ctx, i.Name, params, in["note"], false)
			return result{toast: "updated " + i.Name, refresh: true}, err
		}},
		{id: "delete", label: "Delete instance", danger: true, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			return result{toast: "deleted " + i.Name, refresh: true}, m.Destroy(ctx, i.Name, true)
		}},
	}
}

func opsActions(kind domain.Kind) []action {
	switch kind {
	case domain.KindKafka:
		return kafkaActions()
	case domain.KindPostgres:
		return postgresActions()
	case domain.KindRedis:
		return redisActions()
	case domain.KindS3:
		return s3Actions()
	case domain.KindSNS:
		return snsActions()
	case domain.KindSQS:
		return sqsActions()
	case domain.KindSecrets:
		return secretActions()
	case domain.KindVault:
		return vaultActions()
	case domain.KindGCPIdentity:
		return identityActions()
	default:
		return nil
	}
}

// reg returns the ops registry or a clear error.
func reg(m *control.Manager) (*ops.Registry, error) {
	if m.Ops() == nil {
		return nil, fmt.Errorf("component operations unavailable")
	}
	return m.Ops(), nil
}

func kafkaActions() []action {
	return []action{
		{id: "topics", label: "List topics", run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			ts, err := r.Kafka.ListTopics(ctx, i)
			if err != nil {
				return result{}, err
			}
			rows := make([][]string, 0, len(ts))
			for _, t := range ts {
				rows = append(rows, []string{t.Name, strconv.Itoa(t.Partitions), strconv.Itoa(t.Replicas)})
			}
			return result{table: &tableData{cols: []string{"TOPIC", "PARTITIONS", "REPLICAS"}, rows: rows}}, nil
		}},
		{id: "create-topic", label: "Create topic", fields: []field{
			{key: "topic", label: "Topic", required: true},
			{key: "partitions", label: "Partitions", placeholder: "1"},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			p, _ := strconv.Atoi(in["partitions"])
			return result{toast: "topic " + in["topic"] + " created"}, r.Kafka.CreateTopic(ctx, i, in["topic"], p, 1, nil)
		}},
		{id: "delete-topic", label: "Delete topic", danger: true, fields: []field{{key: "topic", label: "Topic", required: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				return result{toast: "topic " + in["topic"] + " deleted"}, r.Kafka.DeleteTopic(ctx, i, in["topic"])
			}},
		{id: "produce", label: "Produce message", fields: []field{
			{key: "topic", label: "Topic", required: true},
			{key: "key", label: "Key", placeholder: "optional"},
			{key: "value", label: "Value", required: true, multiline: true},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			return result{toast: "produced to " + in["topic"]}, r.Kafka.Produce(ctx, i, in["topic"], in["key"], in["value"])
		}},
		{id: "consume", label: "Consume messages", fields: []field{
			{key: "topic", label: "Topic", required: true},
			{key: "n", label: "Count", placeholder: "10"},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			n, _ := strconv.Atoi(in["n"])
			recs, err := r.Kafka.Consume(ctx, i, in["topic"], n, true)
			if err != nil {
				return result{}, err
			}
			return result{text: strings.Join(recs, "\n")}, nil
		}},
	}
}

func postgresActions() []action {
	return []action{
		{id: "databases", label: "List databases", run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			dbs, err := r.Postgres.ListDatabases(ctx, i)
			if err != nil {
				return result{}, err
			}
			return result{text: strings.Join(dbs, "\n")}, nil
		}},
		{id: "create-db", label: "Create database", fields: []field{{key: "name", label: "Database name", required: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				return result{toast: "database " + in["name"] + " created"}, r.Postgres.CreateDatabase(ctx, i, in["name"])
			}},
		{id: "drop-db", label: "Drop database", danger: true, fields: []field{{key: "name", label: "Database name", required: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				return result{toast: "database " + in["name"] + " dropped"}, r.Postgres.DropDatabase(ctx, i, in["name"])
			}},
		{id: "sql", label: "Run SQL", fields: []field{
			{key: "db", label: "Database", placeholder: "default"},
			{key: "sql", label: "SQL", required: true, multiline: true},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			res, err := r.Postgres.Exec(ctx, i, in["db"], in["sql"])
			if err != nil {
				return result{}, err
			}
			if len(res.Columns) > 0 && len(res.Rows) > 0 {
				return result{table: &tableData{cols: res.Columns, rows: res.Rows}}, nil
			}
			return result{toast: fmt.Sprintf("%s (%d rows affected)", res.Command, res.RowsAffected)}, nil
		}},
	}
}

func redisActions() []action {
	return []action{
		{id: "keys", label: "Scan keys", fields: []field{{key: "pattern", label: "Pattern", placeholder: "*"}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				keys, err := r.Redis.Scan(ctx, i, in["pattern"], 200)
				if err != nil {
					return result{}, err
				}
				return result{text: strings.Join(keys, "\n")}, nil
			}},
		{id: "get", label: "Get key", fields: []field{{key: "key", label: "Key", required: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				v, err := r.Redis.Get(ctx, i, in["key"])
				if err != nil {
					return result{}, err
				}
				return result{text: v}, nil
			}},
		{id: "set", label: "Set key", fields: []field{
			{key: "key", label: "Key", required: true},
			{key: "value", label: "Value", required: true, multiline: true},
			{key: "ttl", label: "TTL", placeholder: "e.g. 5m (optional)"},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			var d time.Duration
			if t := strings.TrimSpace(in["ttl"]); t != "" {
				d, err = time.ParseDuration(t)
				if err != nil {
					return result{}, err
				}
			}
			return result{toast: "set " + in["key"]}, r.Redis.Set(ctx, i, in["key"], in["value"], d)
		}},
		{id: "del", label: "Delete key", danger: true, fields: []field{{key: "key", label: "Key", required: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				n, err := r.Redis.Del(ctx, i, in["key"])
				return result{toast: fmt.Sprintf("deleted %d key(s)", n)}, err
			}},
		{id: "flush", label: "Flush DB", danger: true, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			return result{toast: "flushed"}, r.Redis.FlushDB(ctx, i)
		}},
	}
}

func s3Actions() []action {
	return []action{
		{id: "objects", label: "List objects", fields: []field{{key: "prefix", label: "Prefix", placeholder: "optional"}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				objs, err := r.S3.ListObjects(ctx, i, in["prefix"])
				if err != nil {
					return result{}, err
				}
				rows := make([][]string, 0, len(objs))
				for _, o := range objs {
					rows = append(rows, []string{o.Key, strconv.FormatInt(o.Size, 10), o.LastModified.Format("2006-01-02 15:04")})
				}
				return result{table: &tableData{cols: []string{"KEY", "SIZE", "MODIFIED"}, rows: rows}}, nil
			}},
		{id: "put", label: "Put object", fields: []field{
			{key: "key", label: "Key", required: true},
			{key: "body", label: "Content", required: true, multiline: true},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			return result{toast: "put " + in["key"]}, r.S3.PutObject(ctx, i, in["key"], []byte(in["body"]), "")
		}},
		{id: "get", label: "Get object", fields: []field{{key: "key", label: "Key", required: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				b, err := r.S3.GetObject(ctx, i, in["key"])
				if err != nil {
					return result{}, err
				}
				return result{text: string(b)}, nil
			}},
		{id: "rm", label: "Delete object", danger: true, fields: []field{{key: "key", label: "Key", required: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				return result{toast: "deleted " + in["key"]}, r.S3.DeleteObject(ctx, i, in["key"])
			}},
	}
}

func snsActions() []action {
	return []action{
		{id: "publish", label: "Publish message", fields: []field{
			{key: "subject", label: "Subject", placeholder: "optional"},
			{key: "message", label: "Message", required: true, multiline: true},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			id, err := r.SNS.Publish(ctx, i, in["subject"], in["message"])
			return result{toast: "published (" + id + ")"}, err
		}},
		{id: "subs", label: "List subscriptions", run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			subs, err := r.SNS.ListSubscriptions(ctx, i)
			if err != nil {
				return result{}, err
			}
			rows := make([][]string, 0, len(subs))
			for _, s := range subs {
				rows = append(rows, []string{s.Protocol, s.Endpoint, s.Arn})
			}
			return result{table: &tableData{cols: []string{"PROTOCOL", "ENDPOINT", "ARN"}, rows: rows}}, nil
		}},
	}
}

func sqsActions() []action {
	return []action{
		{id: "send", label: "Send message", fields: []field{{key: "body", label: "Message", required: true, multiline: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				id, err := r.SQS.Send(ctx, i, in["body"])
				return result{toast: "sent (" + id + ")"}, err
			}},
		{id: "receive", label: "Receive messages", fields: []field{{key: "n", label: "Max", placeholder: "1"}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				n, _ := strconv.Atoi(in["n"])
				msgs, err := r.SQS.Receive(ctx, i, n, 0)
				if err != nil {
					return result{}, err
				}
				var b strings.Builder
				for _, mm := range msgs {
					fmt.Fprintf(&b, "%s\n%s\n\n", dimStyle.Render(mm.ID), mm.Body)
				}
				return result{text: strings.TrimSpace(b.String())}, nil
			}},
		{id: "attrs", label: "Attributes", run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			attrs, err := r.SQS.Attributes(ctx, i)
			if err != nil {
				return result{}, err
			}
			rows := make([][]string, 0, len(attrs))
			for k, v := range attrs {
				rows = append(rows, []string{k, v})
			}
			return result{table: &tableData{cols: []string{"ATTRIBUTE", "VALUE"}, rows: rows}}, nil
		}},
		{id: "purge", label: "Purge queue", danger: true, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			return result{toast: "purged"}, r.SQS.Purge(ctx, i)
		}},
	}
}

func secretActions() []action {
	return []action{
		{id: "get", label: "Get value", run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			v, err := r.Secrets.Get(ctx, i)
			if err != nil {
				return result{}, err
			}
			return result{text: v}, nil
		}},
		{id: "set", label: "Set value", fields: []field{{key: "value", label: "Value", required: true, multiline: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				return result{toast: "secret updated"}, r.Secrets.Set(ctx, i, in["value"])
			}},
	}
}

func vaultActions() []action {
	return []action{
		{id: "kv-list", label: "List secret paths", fields: []field{{key: "prefix", label: "Prefix", placeholder: "optional folder, e.g. team/"}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				keys, err := r.Vault.List(ctx, i, in["prefix"])
				if err != nil {
					return result{}, err
				}
				rows := make([][]string, 0, len(keys))
				for _, k := range keys {
					rows = append(rows, []string{k})
				}
				return result{table: &tableData{cols: []string{"PATH"}, rows: rows}}, nil
			}},
		{id: "kv-get", label: "Read secret", fields: []field{{key: "path", label: "Path", required: true, placeholder: "e.g. orders"}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				data, err := r.Vault.Get(ctx, i, in["path"])
				if err != nil {
					return result{}, err
				}
				return result{table: kvTable(data)}, nil
			}},
		{id: "kv-put", label: "Set key in secret", fields: []field{
			{key: "path", label: "Path", required: true, placeholder: "e.g. orders"},
			{key: "key", label: "Key", required: true},
			{key: "value", label: "Value", multiline: true},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			// Merge: setting one key from the TUI must not wipe the others.
			err = r.Vault.Put(ctx, i, in["path"], map[string]string{in["key"]: in["value"]}, true)
			return result{toast: in["key"] + " written to " + in["path"]}, err
		}},
		{id: "kv-delete", label: "Delete secret", danger: true, fields: []field{{key: "path", label: "Path", required: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				return result{toast: "deleted " + in["path"]}, r.Vault.Delete(ctx, i, in["path"])
			}},
	}
}

func identityActions() []action {
	return []action{
		{id: "users", label: "List users", run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			users, err := r.Identity.ListUsers(ctx, i)
			if err != nil {
				return result{}, err
			}
			rows := make([][]string, 0, len(users))
			for _, u := range users {
				rows = append(rows, []string{u.UID, u.Email, u.DisplayName, strconv.FormatBool(u.EmailVerified), u.CustomClaims})
			}
			return result{table: &tableData{cols: []string{"UID", "EMAIL", "NAME", "VERIFIED", "CLAIMS"}, rows: rows}}, nil
		}},
		{id: "create-user", label: "Create user", fields: []field{
			{key: "email", label: "Email", required: true},
			{key: "password", label: "Password", required: true, placeholder: "min 6 chars"},
			{key: "name", label: "Display name", placeholder: "optional"},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			uid, err := r.Identity.CreateUser(ctx, i, in["email"], in["password"], in["name"])
			return result{toast: "created " + in["email"] + " (uid " + uid + ")"}, err
		}},
		{id: "claims", label: "Set custom claims", fields: []field{
			{key: "user", label: "UID or email", required: true},
			{key: "claims", label: "Claims (JSON)", required: true, placeholder: `{"role":"admin"}`},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			return result{toast: "claims set for " + in["user"]}, r.Identity.SetClaims(ctx, i, in["user"], in["claims"])
		}},
		{id: "token", label: "Sign in (get ID token)", fields: []field{
			{key: "email", label: "Email", required: true},
			{key: "password", label: "Password", required: true},
		}, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			res, err := r.Identity.SignIn(ctx, i, in["email"], in["password"])
			if err != nil {
				return result{}, err
			}
			return result{text: fmt.Sprintf("uid: %s\nexpires in: %ss\n\nID token (Authorization: Bearer …):\n\n%s\n", res.UID, res.ExpiresIn, res.IDToken)}, nil
		}},
		{id: "delete-user", label: "Delete user", danger: true, fields: []field{{key: "user", label: "UID or email", required: true}},
			run: func(ctx context.Context, m *control.Manager, i *domain.Instance, in map[string]string) (result, error) {
				r, err := reg(m)
				if err != nil {
					return result{}, err
				}
				return result{toast: "deleted " + in["user"]}, r.Identity.DeleteUser(ctx, i, in["user"])
			}},
		{id: "reset", label: "Delete ALL users", danger: true, run: func(ctx context.Context, m *control.Manager, i *domain.Instance, _ map[string]string) (result, error) {
			r, err := reg(m)
			if err != nil {
				return result{}, err
			}
			return result{toast: "all users deleted"}, r.Identity.Reset(ctx, i)
		}},
	}
}

// kvTable renders a key/value map as a two-column table, sorted by key.
func kvTable(data map[string]string) *tableData {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []string{k, data[k]})
	}
	return &tableData{cols: []string{"KEY", "VALUE"}, rows: rows}
}
