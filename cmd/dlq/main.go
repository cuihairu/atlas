// Command dlq re-publishes Atlas dead letters onto their source topic.
//
// A dead letter is a copy Atlas wrote to "<topic>.dl" after a message
// exhausted its delivery cap (docs/sync.md §4). This tool reads that
// channel oldest-first, re-publishes each copy as a fresh canonical event,
// and only then removes it — so a crash between publish and remove at worst
// makes a re-run re-apply the same event, which Directory.ApplyEvent is
// idempotent for (an upsert).
//
// Usage:
//
//	dlq                      # both topics, adapter from ATLAS_EVENT_ADAPTER
//	dlq -dry-run             # count and decode only; change nothing
//	dlq -topic atlas.config -limit 50
//
// It reads the same ATLAS_* environment the atlas server does, so run it
// with the deployment's env; the exit code is non-zero only when a pass
// left copies behind (decode failures, failed re-publishes or removals).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/config"
	"github.com/cuihairu/atlas/internal/event"
	kafkaEvent "github.com/cuihairu/atlas/internal/event/kafka"
	natsEvent "github.com/cuihairu/atlas/internal/event/nats"
	rabbitEvent "github.com/cuihairu/atlas/internal/event/rabbitmq"
	redisEvent "github.com/cuihairu/atlas/internal/event/redis"
)

// allTopics is the default replay scope: every topic Atlas routes events
// onto.
var allTopics = []string{event.TopicCharacters, event.TopicConfig}

// topicsFlag collects repeatable, comma-separated -topic values.
type topicsFlag []string

func (t *topicsFlag) String() string { return strings.Join(*t, ",") }

func (t *topicsFlag) Set(v string) error {
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			*t = append(*t, s)
		}
	}
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable body of main: it returns the process exit code.
func run(args []string, out, errw io.Writer) int {
	var topics topicsFlag
	fs := flag.NewFlagSet("dlq", flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.Var(&topics, "topic", "topic to replay (repeatable, comma-separated); default: both Atlas topics")
	adapterFlag := fs.String("adapter", "", "adapter to replay through: redis, kafka, nats or rabbitmq (default $ATLAS_EVENT_ADAPTER)")
	limit := fs.Int("limit", 0, "max copies per pass (0 = all)")
	dryRun := fs.Bool("dry-run", false, "count and decode only; publish nothing, remove nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg := config.Load()
	name := strings.TrimSpace(*adapterFlag)
	if name == "" {
		name = cfg.EventAdapter
	}
	if err := validateAdapter(name); err != nil {
		fmt.Fprintln(errw, err)
		return 2
	}
	scope := resolveTopics(topics)

	logger := slog.New(slog.NewTextHandler(errw, &slog.HandlerOptions{Level: slog.LevelError}))
	replayer, closeFn, err := openReplayer(cfg, name, logger)
	if err != nil {
		fmt.Fprintln(errw, err)
		return 1
	}
	defer func() { _ = closeFn() }()

	ctx := context.Background()
	failed := false
	for _, topic := range scope {
		rep, err := replayer.ReplayDeadLetters(ctx, topic, event.ReplayOptions{Limit: *limit, DryRun: *dryRun})
		if err != nil {
			fmt.Fprintf(errw, "replay %s: %v\n", topic, err)
			return 1
		}
		printReport(out, rep)
		if rep.Failed() {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// resolveTopics returns the topic scope to replay; an empty -topic selects
// all known Atlas topics.
func resolveTopics(topics []string) []string {
	if len(topics) == 0 {
		return append([]string(nil), allTopics...)
	}
	return topics
}

// validateAdapter rejects adapters that cannot replay.
func validateAdapter(name string) error {
	switch name {
	case "redis", "kafka", "nats", "rabbitmq":
		return nil
	case "http", "":
		return fmt.Errorf("dlq: adapter %q has no dead-letter channel to replay", name)
	default:
		return fmt.Errorf("dlq: unknown event adapter %q (want redis, kafka, nats or rabbitmq)", name)
	}
}

// openReplayer builds the adapter for the store carrying the dead letters.
// The returned close releases the adapter's transport (for redis, the
// client, which the adapter itself does not own).
func openReplayer(cfg config.Config, name string, logger *slog.Logger) (event.Replayer, func() error, error) {
	switch name {
	case "redis":
		client, err := redisClient(cfg)
		if err != nil {
			return nil, nil, err
		}
		a := redisEvent.New(client, redisEvent.Options{Logger: logger, MaxDelivery: cfg.EventMaxDelivery})
		return a, func() error { return client.Close() }, nil
	case "kafka":
		a, err := kafkaEvent.Open(context.Background(), kafkaEvent.Options{
			Brokers:     splitAddrs(cfg.KafkaBrokers),
			Logger:      logger,
			MaxDelivery: cfg.EventMaxDelivery,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("dlq: kafka: %w", err)
		}
		return a, a.Close, nil
	case "nats":
		a, err := natsEvent.Open(natsEvent.Options{
			URL:         cfg.NATSURL,
			Logger:      logger,
			MaxDelivery: cfg.EventMaxDelivery,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("dlq: nats: %w", err)
		}
		return a, a.Close, nil
	case "rabbitmq":
		a, err := rabbitEvent.Open(rabbitEvent.Options{
			URL:         cfg.RabbitURL,
			Logger:      logger,
			MaxDelivery: cfg.EventMaxDelivery,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("dlq: rabbitmq: %w", err)
		}
		return a, a.Close, nil
	default:
		return nil, nil, fmt.Errorf("dlq: unknown event adapter %q", name)
	}
}

// redisClient builds a go-redis client for the configured topology (single
// node, Sentinel failover, or cluster), mirroring the atlas server's choice.
func redisClient(cfg config.Config) (redis.UniversalClient, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("dlq: parse ATLAS_REDIS_URL %q: %w", cfg.RedisURL, err)
	}
	if seeds := splitAddrs(cfg.RedisClusterAddrs); len(seeds) > 0 {
		return redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    seeds,
			Username: opts.Username,
			Password: opts.Password,
		}), nil
	}
	if sentinels := splitAddrs(cfg.RedisSentinelAddrs); len(sentinels) > 0 {
		return redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    cfg.RedisMasterName,
			SentinelAddrs: sentinels,
			Username:      opts.Username,
			Password:      opts.Password,
			DB:            opts.DB,
		}), nil
	}
	return redis.NewClient(opts), nil
}

// splitAddrs splits a comma-separated address list, dropping blanks.
func splitAddrs(raw string) []string {
	var out []string
	for _, a := range strings.Split(raw, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// printReport writes one pass's result as a single line.
func printReport(w io.Writer, rep event.Report) {
	mode := "replayed"
	if rep.Failed() {
		mode = "incomplete"
	}
	fmt.Fprintf(w, "%s: found %d, republished %d, removed %d, %s\n",
		rep.Topic, rep.Found, rep.Republished, rep.Removed, mode)
	for _, e := range rep.Errors {
		fmt.Fprintf(w, "  %s\n", e.Error())
	}
}
