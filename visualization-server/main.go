package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"time"

	pb "simulation/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GridDTO is a lightweight JSON view sent to the browser
type GridDTO struct {
	Width  int32     `json:"width"`
	Height int32     `json:"height"`
	Cells  []CellDTO `json:"cells"`
	Tick   int64     `json:"tick"`
	At     time.Time `json:"at"`
}

type CellDTO struct {
	X        int32  `json:"x"`
	Y        int32  `json:"y"`
	Occupant *int32 `json:"occupant"`
	Action   string `json:"action"`
}

func main() {
	addr := flag.String("http", ":8080", "HTTP listen address for visualization server")
	simGRPC := flag.String("grpc", "simulation-server-service:9090", "Simulation server gRPC address")
	staticDir := flag.String("static", "../visualization-client", "Directory with static web assets")
	pollMs := flag.Int("poll_ms", 250, "Polling interval in milliseconds for grid updates")
	flag.Parse()

	// Connect to simulation gRPC server
	log.Printf("Connecting to simulation gRPC at %s", *simGRPC)
	conn, err := grpc.NewClient(*simGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to simulation server: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Error closing gRPC connection: %v", err)
		}
	}()

	client := pb.NewSimulationServiceClient(conn)

	// SSE endpoint
	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ticker := time.NewTicker(time.Duration(*pollMs) * time.Millisecond)
		defer ticker.Stop()

		// send one immediately
		if err := sendGridState(r.Context(), client, w); err != nil {
			log.Printf("/events initial send error: %v", err)
			return
		}
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				if err := sendGridState(r.Context(), client, w); err != nil {
					log.Printf("/events send error: %v", err)
					return
				}
				flusher.Flush()
			}
		}
	})

	// Static client assets
	absStaticDir, _ := filepath.Abs(*staticDir)
	log.Printf("Serving static files from %s", absStaticDir)
	http.Handle("/", http.FileServer(http.Dir(absStaticDir)))

	// Support automatic free port selection with -http :0
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to bind %s: %v", *addr, err)
	}
	actualAddr := ln.Addr().String()
	log.Printf("Visualization server listening on %s", actualAddr)
	if err := http.Serve(ln, nil); err != nil {
		log.Fatalf("HTTP server stopped: %v", err)
	}
}

func sendGridState(ctx context.Context, client pb.SimulationServiceClient, w http.ResponseWriter) error {
	// Fetch grid state
	gs, err := client.GetGridState(ctx, &pb.Empty{})
	if err != nil {
		return err
	}

	// Map to DTO
	cells := make([]CellDTO, 0, len(gs.Cells))
	for _, c := range gs.Cells {
		var occ *int32
		var action string
		if c.Occupant != nil {
			id := c.Occupant.Id
			occ = &id
			action = c.Occupant.DecidedActionDisplay
		}
		cells = append(cells, CellDTO{
			X:        c.Position.X,
			Y:        c.Position.Y,
			Occupant: occ,
			Action:   action,
		})
	}

	payload := GridDTO{
		Width:  gs.Width,
		Height: gs.Height,
		Cells:  cells,
		Tick:   time.Now().UnixNano(),
		At:     time.Now(),
	}

	// SSE: write as data: <json>\n\n
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return err
	}
	return nil
}
