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

const (
	defaultGridWidth       = 5
	defaultGridHeight      = 5
	defaultTickRate        = time.Second
	defaultDecisionTimeout = 5 * time.Second
	httpReadTimeout        = 10 * time.Second
	httpWriteTimeout       = 10 * time.Second
	httpIdleTimeout        = 60 * time.Second
)

func main() {
	// Parse command line flags
	devModePtr := flag.Bool("dev", true, "Run in development mode")
	portPtr := flag.String("port", "8080", "Port to run the HTTP health server on")
	grpcPortPtr := flag.String("grpc-port", "9090", "Port to run the gRPC server on")
	flag.Parse()

	if *devModePtr {
		log.Println("Running in development mode")
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Create shared simulation core
	core := NewSimulationCore(defaultGridWidth, defaultGridHeight, defaultTickRate, defaultDecisionTimeout, rng, *devModePtr)
	defer core.Stop()

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
		grpcServer.Stop()
		core.Stop()
		log.Fatalf("Failed to listen on port %s: %v", *grpcPortPtr, err) //nolint:gocritic
	}

	log.Printf("gRPC server listening on port %s", *grpcPortPtr)

	// Start HTTP health endpoint for Kubernetes probes
	go func() {
		mux := http.NewServeMux()

		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("healthy")); err != nil {
				log.Printf("Failed to write health response: %v", err)
				// Note: Cannot change status code after WriteHeader, but log the error
			}
		})

		mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
			gridState, err := grpcServer.GetGridState(r.Context(), &pb.Empty{})
			if err != nil {
				log.Printf("Failed to get grid state: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"width":  gridState.Width,
				"height": gridState.Height,
				"cells":  len(gridState.Cells),
			}); err != nil {
				log.Printf("Failed to encode JSON response: %v", err)
				// Note: Cannot send error response as headers are already written
			}
		})

		server := &http.Server{
			Addr:         ":" + *portPtr,
			Handler:      mux,
			ReadTimeout:  httpReadTimeout,
			WriteTimeout: httpWriteTimeout,
			IdleTimeout:  httpIdleTimeout,
		}

		log.Printf("HTTP health server listening on port %s", *portPtr)
		log.Fatal(server.ListenAndServe())
	}()

	// Start the simulation loop in a separate goroutine
	go func() {
		ticker := time.NewTicker(core.TickRate)
		defer ticker.Stop()

		// Initial state output
		core.PrintState()

		for range ticker.C {
			grpcServer.Tick()
			core.PrintState()
		}
	}()

	// Start the gRPC server
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC: %v", err)
	}
}
