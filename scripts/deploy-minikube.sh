#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}🚀 Starting minikube deployment${NC}"

# Check if minikube is running
if ! minikube status >/dev/null 2>&1; then
    echo -e "${RED}❌ Minikube is not running. Please start it with: minikube start${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Minikube is running${NC}"

# Configure Docker to use minikube's daemon
echo -e "${YELLOW}🔧 Configuring Docker to use minikube's daemon...${NC}"
eval $(minikube docker-env)

# Build images in minikube's Docker daemon
echo -e "${YELLOW}🔨 Building Docker images in minikube...${NC}"

echo "Building simulation server..."
docker build -f docker/simulation-server.Dockerfile -t simulation-server:latest .

echo "Building entity client..."
docker build -f docker/entity-client.Dockerfile -t entity-client:latest .

echo -e "${GREEN}✅ Docker images built successfully in minikube${NC}"

# Clean up any existing deployments
echo -e "${YELLOW}🧹 Cleaning up existing deployments...${NC}"
kubectl delete deployment simulation-server --ignore-not-found=true
kubectl delete deployment entity-client --ignore-not-found=true
kubectl delete service simulation-server-service --ignore-not-found=true
kubectl delete secret gemini-api-secret --ignore-not-found=true

# Deploy to Kubernetes
echo -e "${YELLOW}🚢 Deploying to Kubernetes...${NC}"

# Deploy simulation server
echo "Deploying simulation server..."
kubectl apply -f k8s/simulation-server-deployment.yaml

# Wait for simulation server to be ready
echo "Waiting for simulation server to be ready..."
kubectl wait --for=condition=available --timeout=300s deployment/simulation-server

# Deploy entity client
echo "Deploying entity client..."
kubectl apply -f k8s/entity-client-deployment.yaml

# Wait for entity client to be ready
echo "Waiting for entity client to be ready..."
kubectl wait --for=condition=available --timeout=300s deployment/entity-client

echo -e "${GREEN}✅ Deployment completed successfully!${NC}"

# Show deployment status
echo -e "${YELLOW}📊 Deployment Status:${NC}"
kubectl get pods -l app=simulation-server
kubectl get pods -l app=entity-client

echo -e "${YELLOW}🔍 To monitor the simulation:${NC}"
echo "  kubectl logs -f deployment/simulation-server"
echo "  kubectl logs -f deployment/entity-client"

echo -e "${YELLOW}🌐 To access the simulation server:${NC}"
echo "  kubectl port-forward svc/simulation-server-service 8080:8080"
echo "  Then visit: http://localhost:8080/status"

echo -e "${GREEN}🎉 Distributed simulation is now running in minikube!${NC}" 