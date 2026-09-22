# Central OpenTelemetry (OTel) Collector Setup

This directory contains the Docker Compose and configuration files to run a **central OpenTelemetry Collector** on your server.

It receives logs, traces, and metrics from all services (such as `credential-service`) via OTLP (gRPC/HTTP) and ships them to your existing **Grafana Loki** instance.

---

## Architecture Overview

```
[ credential-service ] --\
[ other-service-A    ] ---> (4317/4318 OTLP) ---> [ otel-collector ] ---> [ Loki :3100 ]
[ other-service-B    ] --/                                       \---> [ Tempo / Prometheus ]
```

---

## Files

- [docker-compose.yml](file:///Users/deepanshu/CRED/cred-poc/deployments/otel-collector/docker-compose.yml): Runs `otel/opentelemetry-collector-contrib` with port bindings and shared network access.
- [otel-collector-config.yaml](file:///Users/deepanshu/CRED/cred-poc/deployments/otel-collector/otel-collector-config.yaml): Defines the OTLP receivers, processors, and Loki exporters.
- [.env.sample](file:///Users/deepanshu/CRED/cred-poc/deployments/otel-collector/.env.sample): Environment file template.

---

## Ports Exposed

| Port | Protocol | Purpose |
| :--- | :--- | :--- |
| **`4317`** | gRPC | Standard OTLP receiver (recommended for high throughput) |
| **`4318`** | HTTP | Standard OTLP receiver (HTTP POST) |
| **`8888`** | HTTP | Internal collector Prometheus metrics |
| **`13133`**| HTTP | Collector health check endpoint |

---

## Deployment Instructions

### 1. Ensure the shared Docker network exists
Ensure the network specified in `DOCKER_NETWORK_NAME` (default: `cred_network`) is created:
```bash
docker network create cred_network || true
```

### 2. Configure Environment
Copy `.env.sample` to `.env`:
```bash
cp .env.sample .env
```

Set `LOKI_ENDPOINT` according to where Loki is running:
- **If Loki runs as a host process / systemd:**
  ```env
  LOKI_ENDPOINT=http://host.docker.internal:3100
  ```
- **If Loki runs as a Docker container:**
  Connect Loki to the same network (`cred_network`), then set:
  ```env
  LOKI_ENDPOINT=http://<loki-container-name>:3100
  ```

### 3. Start the OTel Collector
```bash
docker compose up -d
```

### 4. Verify Collector Status
- **Check healthcheck:**
  ```bash
  curl -I http://localhost:13133/
  # Expected: HTTP/1.1 200 OK
  ```
- **Check logs:**
  ```bash
  docker logs -f otel-collector
  ```

---

## How Applications Connect to OTel Collector

### Services running inside the Docker network (`cred_network`):
Set the following environment variables in the service's `.env` or `docker-compose.yml`:
```env
OTEL_SERVICE_NAME=credential-service
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
OTEL_EXPORTER_OTLP_INSECURE=true
```

### Services or scripts running directly on the host (outside Docker):
```env
OTEL_SERVICE_NAME=credential-service
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
OTEL_EXPORTER_OTLP_INSECURE=true
```

---

## Loki Version Notes

- **Loki v3.0+ (Default)**: Uses native OTLP ingestion over HTTP (`http://${LOKI_ENDPOINT}/otlp`). Ensure `allow_structured_metadata: true` is set in your Loki config.
- **Loki v2.x (Legacy)**: If using Loki version 2, switch the exporter in [otel-collector-config.yaml](file:///Users/deepanshu/CRED/cred-poc/deployments/otel-collector/otel-collector-config.yaml) to use the `loki` exporter pointing to `${LOKI_ENDPOINT}/loki/api/v1/push`.
