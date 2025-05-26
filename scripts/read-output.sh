#!/bin/bash

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}📖 Reading simulation output...${NC}"

# Get the simulation server pod name
POD_NAME=$(kubectl get pods -l app=simulation-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$POD_NAME" ]; then
    echo -e "${RED}❌ No simulation server pod found${NC}"
    echo "Make sure the simulation server is running:"
    echo "  kubectl get pods -l app=simulation-server"
    exit 1
fi

echo -e "${YELLOW}📊 Reading output from pod: $POD_NAME${NC}"
echo ""

# Read the current content of the file
kubectl exec "$POD_NAME" -- cat /app/grid_output.txt 2>/dev/null

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to read output file${NC}"
    echo "The file might not exist yet or the pod might not be ready."
    exit 1
fi 