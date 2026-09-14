#!/usr/bin/env bash
# ==============================================================================
# Deployment Script for credential-service
# Strategy:
# 1. Stop existing containers (preserving named docker volumes)
# 2. Backup existing .env (if present)
# 3. Completely clean up / delete existing folder
# 4. Clone fresh repository code from specified branch
# 5. Restore or configure .env
# 6. Ensure manual external Docker network exists for cross-service communication
# 7. docker compose up --build -d
# ==============================================================================

set -euo pipefail

# Configuration defaults (can be overridden via environment variables)
DEPLOY_DIR="${DEPLOY_DIR:-$HOME/cred-poc}"
# Expand ~/ if present
DEPLOY_DIR="${DEPLOY_DIR/#\~/$HOME}"

BRANCH="${BRANCH:-main}"
REPO_URL="${REPO_URL:-https://github.com/ONDC-Organization/cred-poc.git}"
DOCKER_NETWORK_NAME="${DOCKER_NETWORK_NAME:-cred_network}"
BACKUP_ENV_PATH="/tmp/cred_poc_env_backup_$$.env"

echo "=========================================================="
echo " Starting Deployment"
echo " Target Directory : ${DEPLOY_DIR}"
echo " Branch           : ${BRANCH}"
echo " Network Name     : ${DOCKER_NETWORK_NAME}"
echo " Timestamp        : $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo "=========================================================="

# Check requirements
command -v git >/dev/null 2>&1 || { echo "ERROR: 'git' is not installed." >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "ERROR: 'docker' is not installed." >&2; exit 1; }

# Determine Docker Compose command
if docker compose version >/dev/null 2>&1; then
    COMPOSE_CMD="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE_CMD="docker-compose"
elif sudo -n docker compose version >/dev/null 2>&1; then
    COMPOSE_CMD="sudo docker compose"
elif sudo -n command -v docker-compose >/dev/null 2>&1; then
    COMPOSE_CMD="sudo docker-compose"
else
    echo "ERROR: Neither 'docker compose' nor 'docker-compose' found." >&2
    exit 1
fi
echo "Using compose command: ${COMPOSE_CMD}"

# Step 1: Clean up existing deployment if present
if [ -d "${DEPLOY_DIR}" ]; then
    echo "--- [1/6] Existing deployment found at ${DEPLOY_DIR} ---"
    
    # Backup .env if exists
    if [ -f "${DEPLOY_DIR}/.env" ]; then
        echo "Backing up existing .env to ${BACKUP_ENV_PATH}..."
        cp "${DEPLOY_DIR}/.env" "${BACKUP_ENV_PATH}"
    fi

    # Gracefully stop existing containers
    echo "Stopping existing docker containers..."
    (cd "${DEPLOY_DIR}" && ${COMPOSE_CMD} down --remove-orphans) || true

    # Clean up directory
    echo "Removing existing deployment directory: ${DEPLOY_DIR}..."
    rm -rf "${DEPLOY_DIR}"
else
    echo "--- [1/6] No existing deployment found. Performing initial setup ---"
fi

# Step 2: Fresh Clone
echo "--- [2/6] Cloning fresh repository from branch '${BRANCH}' ---"
mkdir -p "$(dirname "${DEPLOY_DIR}")"
git clone --depth 1 --branch "${BRANCH}" "${REPO_URL}" "${DEPLOY_DIR}"
cd "${DEPLOY_DIR}"

# Step 3: Setup .env file
echo "--- [3/6] Configuring environment (.env) ---"
if [ -n "${SECRET_ENV_B64:-}" ]; then
    echo "Writing .env from provided base64 secret payload..."
    echo "${SECRET_ENV_B64}" | base64 -d > .env
elif [ -f "${BACKUP_ENV_PATH}" ]; then
    echo "Restoring backed-up .env file..."
    mv "${BACKUP_ENV_PATH}" .env
elif [ -f .env.sample ]; then
    echo "No .env provided or backed up; initializing from .env.sample..."
    cp .env.sample .env
fi
# Clean up temporary backup if it still exists
rm -f "${BACKUP_ENV_PATH}" 2>/dev/null || true

# Step 4: Ensure manual Docker network exists
echo "--- [4/6] Verifying manual Docker network '${DOCKER_NETWORK_NAME}' ---"
if ! docker network inspect "${DOCKER_NETWORK_NAME}" >/dev/null 2>&1; then
    echo "Network '${DOCKER_NETWORK_NAME}' not found. Creating external network..."
    docker network create "${DOCKER_NETWORK_NAME}"
    echo "Network '${DOCKER_NETWORK_NAME}' created successfully."
else
    echo "Network '${DOCKER_NETWORK_NAME}' already exists."
fi

# Step 5: Build and start containers
echo "--- [5/6] Building images and starting containers ---"
${COMPOSE_CMD} up --build -d --remove-orphans

# Step 6: Verify deployment status
echo "--- [6/6] Verifying running containers ---"
sleep 5
${COMPOSE_CMD} ps

echo "=========================================================="
echo " Deployment completed successfully!"
echo " Services are connected to network '${DOCKER_NETWORK_NAME}'"
echo "=========================================================="
