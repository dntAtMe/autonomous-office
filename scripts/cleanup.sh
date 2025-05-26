#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE=${NAMESPACE:-"default"}
DOCKER_REGISTRY=${DOCKER_REGISTRY:-"localhost:5000"}
IMAGE_TAG=${IMAGE_TAG:-"latest"}

echo -e "${YELLOW}🧹 Starting cleanup of distributed simulation${NC}"

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check if kubectl exists
if ! command_exists kubectl; then
    echo -e "${RED}❌ kubectl is not installed${NC}"
    exit 1
fi

# Remove Kubernetes deployments
echo -e "${YELLOW}🗑️  Removing Kubernetes deployments...${NC}"

# Delete entity client deployment
if kubectl get deployment entity-client -n ${NAMESPACE} >/dev/null 2>&1; then
    echo "Removing entity client deployment..."
    kubectl delete deployment entity-client -n ${NAMESPACE}
else
    echo "Entity client deployment not found"
fi

# Delete simulation server deployment and service
if kubectl get deployment simulation-server -n ${NAMESPACE} >/dev/null 2>&1; then
    echo "Removing simulation server deployment..."
    kubectl delete deployment simulation-server -n ${NAMESPACE}
else
    echo "Simulation server deployment not found"
fi

if kubectl get service simulation-server-service -n ${NAMESPACE} >/dev/null 2>&1; then
    echo "Removing simulation server service..."
    kubectl delete service simulation-server-service -n ${NAMESPACE}
else
    echo "Simulation server service not found"
fi

# Delete secret if it exists
if kubectl get secret gemini-api-secret -n ${NAMESPACE} >/dev/null 2>&1; then
    echo "Removing Gemini API secret..."
    kubectl delete secret gemini-api-secret -n ${NAMESPACE}
else
    echo "Gemini API secret not found"
fi

echo -e "${GREEN}✅ Kubernetes resources removed${NC}"

# Remove Docker images if requested
if [[ "${1}" == "--images" ]]; then
    echo -e "${YELLOW}🐳 Removing Docker images...${NC}"
    
    if command_exists docker; then
        # Remove local images
        docker rmi simulation-server:${IMAGE_TAG} 2>/dev/null || echo "Local simulation-server image not found"
        docker rmi entity-client:${IMAGE_TAG} 2>/dev/null || echo "Local entity-client image not found"
        
        # Remove registry-tagged images
        docker rmi ${DOCKER_REGISTRY}/simulation-server:${IMAGE_TAG} 2>/dev/null || echo "Registry simulation-server image not found"
        docker rmi ${DOCKER_REGISTRY}/entity-client:${IMAGE_TAG} 2>/dev/null || echo "Registry entity-client image not found"
        
        echo -e "${GREEN}✅ Docker images removed${NC}"
    else
        echo -e "${YELLOW}⚠️  Docker not found, skipping image cleanup${NC}"
    fi
fi

# Clean up any temporary files
if [[ -d "tmp" ]]; then
    echo -e "${YELLOW}📁 Removing temporary files...${NC}"
    rm -rf tmp
    echo -e "${GREEN}✅ Temporary files removed${NC}"
fi

echo -e "${GREEN}🎉 Cleanup completed successfully!${NC}"

# Show remaining resources (if any)
echo -e "${YELLOW}📊 Remaining resources in namespace '${NAMESPACE}':${NC}"
kubectl get all -n ${NAMESPACE} | grep -E "(simulation|entity)" || echo "No simulation resources found"

echo -e "${YELLOW}💡 Usage tips:${NC}"
echo "  - Run with --images flag to also remove Docker images"
echo "  - Set NAMESPACE environment variable to clean up different namespace"
echo "  - Example: NAMESPACE=my-sim ./scripts/cleanup.sh --images" 