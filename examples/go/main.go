// Command main demonstrates the Atlas Go SDK: register a game server,
// start auto-heartbeat, then look up characters and leave cleanly.
//
// Run against a local Atlas (default ports):
//
//	go run ./examples/go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	atlas "github.com/cuihairu/atlas/sdk/go/atlas"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// REST by default; switch with atlas.TransportGRPC and the gRPC addr.
	// RegistryAddr points at the split registry port when not behind a
	// merged proxy (local dev: ATLAS_REGISTRY_ADDR=localhost:8081).
	cli, err := atlas.New(atlas.Options{
		Addr:          envOr("ATLAS_ADDR", "localhost:8080"),
		RegistryAddr:  os.Getenv("ATLAS_REGISTRY_ADDR"),
		Transport:     atlas.Transport(envOr("ATLAS_TRANSPORT", string(atlas.TransportREST))),
		RegistryToken: os.Getenv("ATLAS_REGISTRY_TOKEN"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cli.Close()

	// 1. Register this server.
	serverID := envOr("SERVER_ID", "demo-game-1")
	reg, err := cli.Register(ctx, atlas.RegisterRequest{
		ServerID: serverID,
		Name:     "Demo Game Server",
		Type:     "game",
		Region:   "cn-east",
		Version:  "1.0.0",
		Platform: "any",
		Endpoint: atlas.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 2000,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("registered: %s (status=%s)\n", reg.ServerID, reg.Status)

	// 2. Heartbeat: one synchronous beat first — discovery/routing only
	// see this server once it is online — then the auto loop every 10s.
	// Update the payload as load changes; Atlas suspects at 3x the interval.
	if _, err := cli.Heartbeat(ctx, serverID, atlas.HeartbeatRequest{}); err != nil {
		log.Printf("first heartbeat: %v", err)
	}
	loop := cli.StartHeartbeat(serverID, 10*time.Second, atlas.HeartbeatRequest{})
	defer loop.Stop()
	loop.OnError(func(err error) { log.Printf("heartbeat failed: %v", err) })

	// 3. Discovery: where should account 42 play?
	rec, err := cli.Recommend(ctx, 42, "cn-east", "", "")
	if err != nil {
		log.Printf("recommend: %v", err)
	} else {
		fmt.Printf("account 42 → %s (%s)\n", rec.Server.ID, rec.Reason)
	}

	// 4. Directory: characters on that server.
	page, err := cli.ListCharactersByServer(ctx, rec.Server.ID, 10, "")
	if err != nil {
		log.Printf("list characters: %v", err)
	} else {
		for _, ch := range page.Characters {
			fmt.Printf("  character %d %s lv%d\n", ch.CharacterID, ch.Name, ch.Level)
		}
	}

	// 5. Stay up until signalled, then drain and leave cleanly.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("shutting down…")
	loop.Stop()
	if _, err := cli.Unregister(ctx, serverID); err != nil {
		log.Printf("unregister: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
