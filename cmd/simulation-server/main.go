package main

import (
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"net"
	"net/http"
	"time"

	pb "simulation/proto"

	"google.golang.org/grpc"
)

func main() {
	// Parse command line flags
	devModePtr := flag.Bool("dev", true, "Run in development mode")
	portPtr := flag.String("port", "8080", "Port to run the server on")
	grpcModePtr := flag.Bool("grpc", true, "Use gRPC instead of WebSocket")
	grpcPortPtr := flag.String("grpc-port", "9090", "Port to run the gRPC server on")
	flag.Parse()

	if *devModePtr {
		log.Println("Running in development mode")
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Create shared simulation core
	core := NewSimulationCore(5, 5, time.Second, 5*time.Second, rng, *devModePtr)
	defer core.Stop()

	if *grpcModePtr {
		// Start gRPC server
		log.Println("Starting gRPC simulation server...")
		grpcServer := NewGRPCSimulationServer(core)
		defer grpcServer.Stop()

		// Create gRPC server
		s := grpc.NewServer()
		pb.RegisterSimulationServiceServer(s, grpcServer)

		// Listen on the specified port
		lis, err := net.Listen("tcp", ":"+*grpcPortPtr)
		if err != nil {
			log.Fatalf("Failed to listen on port %s: %v", *grpcPortPtr, err)
		}

		log.Printf("gRPC server listening on port %s", *grpcPortPtr)

		// Start HTTP health endpoint for Kubernetes probes
		go func() {
			http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("healthy"))
			})
			http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
				gridState, _ := grpcServer.GetGridState(r.Context(), &pb.Empty{})
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"width":    gridState.Width,
					"height":   gridState.Height,
					"entities": len(gridState.Entities),
				})
			})
			log.Printf("HTTP health server listening on port %s", *portPtr)
			log.Fatal(http.ListenAndServe(":"+*portPtr, nil))
		}()

		// Start the simulation loop in a separate goroutine
		go func() {
			ticker := time.NewTicker(core.TickRate)
			defer ticker.Stop()

			// Initial state output
			core.PrintState()

			for {
				select {
				case <-ticker.C:
					grpcServer.Tick()
					core.PrintState()
				}
			}
		}()

		// Start the gRPC server
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve gRPC: %v", err)
		}
	} else {
		log.Println("Starting WebSocket simulation server...")
		wsServer := NewWebSocketServer(core)
		defer wsServer.Stop()

		// Set up HTTP routes
		http.HandleFunc("/register", wsServer.RegisterEntity)
		http.HandleFunc("/health", wsServer.HealthCheck)
		http.HandleFunc("/status", wsServer.Status)

		// Start the simulation
		wsServer.Start()

		// Start HTTP server
		log.Printf("Starting WebSocket simulation server on port %s", *portPtr)
		log.Fatal(http.ListenAndServe(":"+*portPtr, nil))
	}
}
