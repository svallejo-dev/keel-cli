// Package health probes provisioned instances at the connectivity level (SELECT 1,
// PING, TCP, HTTP, AWS endpoint), independent of the provider's lifecycle status.
package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/provider/gcp"
	"github.com/svallejo-dev/keel-cli/internal/vaultx"
)

// Result is the outcome of a health probe.
type Result struct {
	OK      bool
	Detail  string
	Latency time.Duration
}

// Status renders a short status word for display.
func (r Result) Status() string {
	if r.OK {
		return "healthy"
	}
	return "unhealthy"
}

const probeTimeout = 3 * time.Second

// Check probes a single instance according to its kind. Mock instances always
// report healthy (nothing real is running).
func Check(ctx context.Context, cfg config.Config, inst *domain.Instance) Result {
	start := time.Now()
	if inst.Provider == domain.ProviderMock {
		return Result{OK: true, Detail: "simulated (mock engine)", Latency: time.Since(start)}
	}
	if inst.Status == domain.StatusStopped {
		return Result{OK: false, Detail: "stopped"}
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var r Result
	switch inst.Kind {
	case domain.KindPostgres:
		r = checkPostgres(ctx, inst)
	case domain.KindRedis:
		r = checkRedis(ctx, inst)
	case domain.KindKafka:
		// floci MSK brokers advertise an internal address (not host-reachable), so
		// fall back to verifying floci is up; a standalone kafka is TCP-probed.
		if inst.Provider == domain.ProviderAWS {
			r = checkAWSEndpoint(ctx, cfg)
		} else {
			r = checkTCP(inst)
		}
	case domain.KindSchemaRegistry:
		r = checkHTTP(inst, "/health")
	case domain.KindVault:
		r = checkVault(ctx, inst)
	case domain.KindGCPIdentity:
		r = checkIdentity(ctx, inst)
	case domain.KindS3, domain.KindSNS, domain.KindSQS, domain.KindSecrets, domain.KindDynamoDB:
		r = checkAWSEndpoint(ctx, cfg)
	default:
		r = checkTCP(inst)
	}
	r.Latency = time.Since(start)
	return r
}

func checkPostgres(ctx context.Context, inst *domain.Instance) Result {
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return Result{Detail: "no endpoint"}
	}
	db := inst.Cred.Extra["dbname"]
	if db == "" {
		db = inst.Name
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable&connect_timeout=3",
		inst.Cred.Username, inst.Cred.Password, ep.Host, ep.Port, db)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return Result{Detail: "connect: " + err.Error()}
	}
	defer conn.Close(ctx)
	if err := conn.Ping(ctx); err != nil {
		return Result{Detail: "ping: " + err.Error()}
	}
	return Result{OK: true, Detail: "SELECT 1 ok"}
}

func checkRedis(ctx context.Context, inst *domain.Instance) Result {
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return Result{Detail: "no endpoint"}
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", ep.Host, ep.Port),
		Password: inst.Cred.Password,
	})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return Result{Detail: "PING: " + err.Error()}
	}
	return Result{OK: true, Detail: "PING pong"}
}

// checkVault distinguishes "down" from "up but sealed": a sealed Vault answers
// but serves nothing, which is the usual state right after a restart.
func checkVault(ctx context.Context, inst *domain.Instance) Result {
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return Result{Detail: "no endpoint"}
	}
	st, err := vaultx.New(fmt.Sprintf("http://%s:%d", ep.Host, ep.Port), "").Health(ctx)
	switch {
	case err != nil:
		return Result{Detail: "sys/health: " + err.Error()}
	case !st.Initialized:
		return Result{Detail: "not initialized"}
	case st.Sealed:
		return Result{Detail: "sealed — run `keel up " + inst.Name + "` to unseal"}
	}
	return Result{OK: true, Detail: "unsealed (vault " + st.Version + ")"}
}

func checkIdentity(ctx context.Context, inst *domain.Instance) Result {
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return Result{Detail: "no endpoint"}
	}
	url := fmt.Sprintf("http://%s:%d/", ep.Host, ep.Port)
	if !gcp.IdentityReady(ctx, &http.Client{Timeout: probeTimeout}, url) {
		return Result{Detail: "auth emulator not ready at " + url}
	}
	return Result{OK: true, Detail: "auth emulator ready"}
}

func checkTCP(inst *domain.Instance) Result {
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return Result{Detail: "no endpoint"}
	}
	addr := net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port))
	conn, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		return Result{Detail: "dial: " + err.Error()}
	}
	_ = conn.Close()
	return Result{OK: true, Detail: "tcp connect ok"}
}

func checkHTTP(inst *domain.Instance, path string) Result {
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return Result{Detail: "no endpoint"}
	}
	url := fmt.Sprintf("http://%s:%d%s", ep.Host, ep.Port, path)
	client := &http.Client{Timeout: probeTimeout}
	resp, err := client.Get(url)
	if err != nil {
		// Fall back to a raw TCP check: the engine may be up without that path.
		return checkTCP(inst)
	}
	_ = resp.Body.Close()
	return Result{OK: true, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
}

// checkAWSEndpoint verifies the floci gateway is reachable. Any HTTP response
// (even 400/404) means the emulator is up and serving the AWS API.
func checkAWSEndpoint(ctx context.Context, cfg config.Config) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.FlociEndpoint, nil)
	if err != nil {
		return Result{Detail: err.Error()}
	}
	client := &http.Client{Timeout: probeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return Result{Detail: "floci endpoint unreachable: " + err.Error()}
	}
	_ = resp.Body.Close()
	return Result{OK: true, Detail: fmt.Sprintf("floci reachable (HTTP %d)", resp.StatusCode)}
}
