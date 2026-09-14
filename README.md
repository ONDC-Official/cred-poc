# Credential Service (`cred-poc`)

Backend service for identity verification and credential issuance.

## Architecture & Docker Setup

The service runs using Docker Compose with PostgreSQL and connects to a shared, manual Docker network so other services on the host or in other compose stacks can communicate with it.

### Manual Docker Network

The compose file uses an external Docker network named `cred_network` (customizable via `DOCKER_NETWORK_NAME`):

```yaml
networks:
  cred_network:
    name: ${DOCKER_NETWORK_NAME:-cred_network}
    external: true
```

#### How other services communicate with this service:
Any other service (e.g. API gateway, client app, or mock server) running in Docker can join the same network by adding to their `docker-compose.yml`:

```yaml
services:
  other-service:
    image: my-other-service:latest
    networks:
      - cred_network

networks:
  cred_network:
    name: cred_network
    external: true
```

Once attached, the service can reach `credential-service` directly using DNS:
- **URL**: `http://credential-service:8080` (or `http://credential-service:8080/verify-identity`, etc.)
- **Postgres** (if needed): `credential-service-postgres:5432` or `postgres:5432`

---

## Deployment Strategy (GitHub Actions Workflow)

A fully automated CI/CD pipeline is provided in [`.github/workflows/deploy.yml`](.github/workflows/deploy.yml).

### Deployment Lifecycle (Manual Trigger)
Deployment is triggered manually via GitHub Actions (`Actions` tab -> `Deploy to Server` -> `Run workflow`):
1. **Connects** to the deployment server over SSH.
2. **Backs up** existing server configuration (`.env`) if present.
3. **Stops existing containers** (`docker compose down --remove-orphans`) to free ports and container names (named volumes such as `postgres_data` are safely retained).
4. **Cleans up** the old deployment directory (`rm -rf "$DEPLOY_DIR"`).
5. **Clones fresh code** from the repository branch into the deployment directory.
6. **Restores or sets up `.env`** (from GitHub Secret `ENV_FILE`, backed-up server `.env`, or `.env.sample`).
7. **Verifies or creates** the manual Docker network (`cred_network`).
8. **Builds & starts** the stack (`docker compose up --build -d`).
9. **Verifies status** and service health.

---

## Required GitHub Repository Secrets

Configure the following secrets in your GitHub repository (**Settings** -> **Secrets and variables** -> **Actions** -> **New repository secret**):

| Secret Name | Required | Description | Example |
|---|---|---|---|
| `SERVER_HOST` | **Yes** | Server IP address or hostname | `192.0.2.1` or `api.example.com` |
| `SERVER_USER` | **Yes** | SSH user | `ubuntu` or `root` |
| `SERVER_SSH_KEY` | **Yes** | SSH private key | `-----BEGIN OPENSSH PRIVATE KEY----- ...` |
| `SERVER_PORT` | No | SSH port (defaults to `22`) | `22` |
| `DEPLOY_DIR` | No | Deployment path on server (defaults to `~/cred-poc`) | `/home/ubuntu/cred-poc` |
| `DOCKER_NETWORK_NAME` | No | Docker network name (defaults to `cred_network`) | `cred_network` |
| `ENV_FILE` | No | Production `.env` contents to write | *(Copy of production .env)* |
| `SERVER_PASSWORD` | No | Server password (if not using SSH key) | `secretpassword` |
| `SERVER_SSH_PASSPHRASE`| No | Passphrase for private SSH key (if encrypted) | `keypassphrase` |

---

## Local Development Commands

The [`Makefile`](Makefile) provides simple shortcuts for local development:

```bash
# Ensure manual network exists and start containers in background
make docker-up

# View logs for credential-service
make docker-logs

# Stop containers
make docker-down

# Run unit tests
make test

# Format code
make fmt

# Build standalone binary locally
make build

# Run standalone binary directly
make run
```
